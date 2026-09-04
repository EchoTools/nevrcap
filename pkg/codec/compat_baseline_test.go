package codec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Backward compatibility, checked mechanically on every run.
//
// WHY THIS EXISTS. Compatibility was previously "checked" by
// TestEmitLayoutsForCrossVersionCheck, which t.Skip()s unless an environment
// variable is set. In a normal `go test ./...` it skipped, so nothing in CI
// caught a backward-compatibility break; the guarantee held only because a
// person hand-compiled an old reader and ran a matrix by hand. A compatibility
// promise that depends on someone remembering to check it is not a promise.
//
// WHAT IT PROVES, and these four are the bar:
//
//	1. A reader built from a BASELINE reads a file written by HEAD's defaults.
//	2. HEAD reads a file written by that baseline.
//	3. HEAD's default output is BYTE-IDENTICAL to the baseline's, by sha256 —
//	   not merely "readable" (but see the dependency caveat below).
//	4. The opt-in per-block layout is readable by the baseline reader.
//
// HOW IT GETS AN OLD READER: it builds one, from this repository's own git
// history, via `git archive <baseline> pkg/codec go.mod go.sum` into a temp
// tree with testdata/compat/harness dropped in and compiled there.
//
// CHOICE, AND ITS FAILURE MODE IN THE SAME BREATH: building from a pinned SHA
// is preferred over committing prebuilt fixture files because a fixture cannot
// be a READER — properties 1 and 4 are about running old code, and no file can
// stand in for a program — and because `git archive <sha>` either reproduces
// the exact historical tree or fails, where a committed copy can be silently
// "fixed" by a well-meaning editor; the failure mode I accept in exchange is
// that when a baseline stops COMPILING under a future Go toolchain this test
// goes red for a reason that is not a compatibility break, which is why a build
// failure is reported as its own distinct error naming the baseline rather than
// as a compatibility failure, and why it FAILS rather than skips — "we can no
// longer build the version we promise to support" is a fact a human must act
// on, by advancing the promise deliberately, not a fact to swallow.
//
// It deliberately does NOT skip when git history is unavailable. Skipping on a
// missing precondition is exactly the hole this test was written to close.

// compatBaseline is a version this repository promises to stay compatible with.
//
// BASELINES ACCUMULATE; THEY DO NOT ADVANCE. Compatibility is a set of promises
// -- "we can still read what v4.0.0 wrote" -- and replacing an old entry with a
// newer one silently retires a promise nobody agreed to retire. Add a row when
// a release is tagged; remove one only as a deliberate, announced decision to
// stop supporting that version.
//
// THE COST OF THAT POLICY, WHICH WHOEVER ADDS ROW SEVEN MUST KNOW: the
// compressed-bytes assertion (property 3b) can only run when a baseline pins the
// same output-affecting dependencies as HEAD, and every klauspost/compress bump
// retires it for every row written before the bump. So the row COUNT trends up
// while the number of rows able to catch a compressed-bytes change trends to
// zero, and a growing table reads as growing coverage when it is not. Row seven
// probably cannot catch a layout change by that route.
//
// Two things hold the line. First, property 3a compares the DECOMPRESSED
// envelope stream and the container's frame structure -- properties of this
// format rather than of the compressor -- so it runs on every row and survives
// dependency bumps. That is where the real protection lives; readability alone
// never was protection, because an old reader that still parses a rewritten
// layout reports OK. Second, TestCompatBaselinesCanStillCatchAByteChange fails
// when NO row can do 3b, so the day a bump disarms the last one is the day CI
// says so rather than the day it quietly stops mattering.
type compatBaseline struct {
	// Ref is a tag or commit resolvable by git in this repository. A tag is
	// preferred: it names a release rather than a moment.
	Ref string
	// Why records what this baseline represents, so a later reader can judge
	// whether it is still worth carrying.
	Why string
}

var compatBaselines = []compatBaseline{
	{
		Ref: "v4.0.0",
		Why: "the released format version, tagged 2026-08-26",
	},
	{
		Ref: "2ca18fa",
		Why: "last commit before per-block compression",
	},
}

// byteIdenticalPins are the module requirements whose versions can change the
// BYTES a writer emits with no change to this repository at all: compressed
// output comes from klauspost/compress, message encoding from the protobuf
// runtime and the generated types.
//
// Whether property 3b applies is DERIVED from comparing these across two go.mod
// files, never declared in the table above, because a hand-maintained flag
// becomes a lie the first time somebody bumps a dependency without revisiting
// it -- and a lie in this direction disables an assertion silently, which is
// the exact failure this file exists to prevent.
var byteIdenticalPins = []string{
	"github.com/klauspost/compress",
	"google.golang.org/protobuf",
	"buf.build/gen/go/echotools/nevr-api/protocolbuffers/go",
}

// pinsMatchHEAD reports whether a baseline's output-affecting dependency
// versions are identical to HEAD's, and returns the first difference so the
// reason can be stated rather than merely asserted.
func pinsMatchHEAD(t *testing.T, ref string) (bool, string) {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("locating the repository: %v", err)
	}
	headMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("reading HEAD go.mod: %v", err)
	}
	baseMod, err := runIn(root, "git", "show", ref+":go.mod")
	if err != nil {
		t.Fatalf("reading %s go.mod: %v", ref, err)
	}
	head, base := modulePins(string(headMod)), modulePins(baseMod)
	for _, mod := range byteIdenticalPins {
		if head[mod] != base[mod] {
			return false, fmt.Sprintf("%s: baseline pins %s, HEAD pins %s", mod, base[mod], head[mod])
		}
	}
	return true, ""
}

// modulePins reads module→version for the requirements named in
// byteIdenticalPins out of go.mod content.
func modulePins(gomod string) map[string]string {
	pins := map[string]string{}
	for line := range strings.Lines(gomod) {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		for _, mod := range byteIdenticalPins {
			if fields[0] == mod {
				pins[mod] = fields[1]
			}
		}
	}
	return pins
}

// The fixed corpus. It must stay byte-for-byte in step with fixedHeader /
// fixedFrames in testdata/compat/harness/main.go — the byte-identity property
// compares this test's output against that program's, so drift between them
// surfaces as a compatibility failure that is really a test bug. Kept small and
// literal for exactly that reason.
const (
	compatCaptureID  = "compat-baseline-corpus"
	compatSessionID  = "COMPAT-001"
	compatMapName    = "mpl_arena_a"
	compatEpochUnix  = 1756600000
	compatFrameCount = 400
	compatKeyframes  = 50
)

func compatHeader() *capturepb.CaptureHeader {
	return &capturepb.CaptureHeader{
		CaptureId:     compatCaptureID,
		CreatedAt:     timestamppb.New(time.Unix(compatEpochUnix, 0).UTC()),
		FormatVersion: 2,
		GameHeader: &capturepb.CaptureHeader_EchoArena{
			EchoArena: &capturepb.EchoArenaHeader{
				SessionId: compatSessionID,
				MapName:   compatMapName,
				MatchType: capturepb.MatchType_MATCH_TYPE_ARENA,
			},
		},
	}
}

func compatFrames() []*capturepb.Frame {
	frames := make([]*capturepb.Frame, compatFrameCount)
	for i := range frames {
		idx := uint32(i) //nolint:gosec // compatFrameCount is a small constant
		frames[i] = &capturepb.Frame{
			FrameIndex:        idx,
			TimestampOffsetMs: idx * 33,
			Payload: &capturepb.Frame_EchoArena{
				EchoArena: &capturepb.EchoArenaFrame{
					GameStatus:   capturepb.GameStatus_GAME_STATUS_PLAYING,
					GameClock:    300 - float32(i)*0.033,
					BluePoints:   int32(i / 100), //nolint:gosec // bounded by compatFrameCount
					OrangePoints: int32(i / 150), //nolint:gosec // bounded by compatFrameCount
				},
			},
		}
	}
	return frames
}

// writeCompatCapture writes the fixed corpus with HEAD's writer.
func writeCompatCapture(t *testing.T, path string, opts ...WriterOption) {
	t.Helper()
	opts = append([]WriterOption{WithKeyframeInterval(compatKeyframes)}, opts...)
	w, err := NewWriterWithOptions(path, opts...)
	if err != nil {
		t.Fatalf("NewWriterWithOptions: %v", err)
	}
	if err := w.WriteHeader(compatHeader()); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	for _, f := range compatFrames() {
		if err := w.WriteFrame(f); err != nil {
			t.Fatalf("WriteFrame %d: %v", f.GetFrameIndex(), err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// baselineBuild caches one built harness per baseline ref for the life of the
// test binary: `git archive` plus `go build` is a few seconds, and repeating it
// per sub-test would dominate the run.
type baselineBuild struct {
	binary string
	err    error
}

var (
	baselineMu     sync.Mutex
	baselineBuilds = map[string]*baselineBuild{}
)

// buildBaselineHarness produces a harness binary compiled against the pkg/codec
// of the given ref. The returned error distinguishes "could not build the
// baseline" from any compatibility result, because they mean different things
// to whoever reads the failure.
func buildBaselineHarness(t *testing.T, ref string) string {
	t.Helper()

	baselineMu.Lock()
	defer baselineMu.Unlock()

	if b, ok := baselineBuilds[ref]; ok {
		if b.err != nil {
			t.Fatalf("%v", b.err)
		}
		return b.binary
	}

	b := &baselineBuild{}
	b.binary, b.err = buildBaseline(ref)
	baselineBuilds[ref] = b
	if b.err != nil {
		t.Fatalf("%v", b.err)
	}
	return b.binary
}

func buildBaseline(ref string) (string, error) {
	root, err := repoRoot()
	if err != nil {
		return "", fmt.Errorf("compat baseline %s: cannot locate the repository, so the "+
			"backward-compatibility check cannot run: %w", ref, err)
	}

	sha, err := runIn(root, "git", "rev-parse", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("compat baseline %s: cannot resolve the ref. In CI this usually "+
			"means a shallow clone: the checkout step needs fetch-depth: 0. %w", ref, err)
	}
	sha = strings.TrimSpace(sha)

	// os.MkdirTemp rather than t.TempDir: the build is cached across sub-tests
	// and must outlive the test that first asked for it.
	dir, err := os.MkdirTemp("", "tape-compat-"+strings.NewReplacer("/", "-", ".", "-").Replace(ref)+"-")
	if err != nil {
		return "", fmt.Errorf("compat baseline %s: %w", ref, err)
	}

	// Extract only what the harness needs to compile: the codec package and the
	// module definition of that era, so the baseline builds against the
	// dependency versions it actually shipped with.
	archive := exec.Command("git", "-C", root, "archive", sha, "pkg/codec", "go.mod", "go.sum")
	untar := exec.Command("tar", "-x", "-C", dir)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("compat baseline %s (%s): %w", ref, sha[:8], err)
	}
	untar.Stdin = pipe
	var archiveErr, untarErr strings.Builder
	archive.Stderr = &archiveErr
	untar.Stderr = &untarErr
	if err := untar.Start(); err != nil {
		return "", fmt.Errorf("compat baseline %s (%s): tar: %w", ref, sha[:8], err)
	}
	if err := archive.Run(); err != nil {
		return "", fmt.Errorf("compat baseline %s (%s): git archive failed: %w: %s",
			ref, sha[:8], err, archiveErr.String())
	}
	if err := untar.Wait(); err != nil {
		return "", fmt.Errorf("compat baseline %s (%s): tar failed: %w: %s",
			ref, sha[:8], err, untarErr.String())
	}

	// The historical tree has tests that may reference helpers we did not
	// extract; the harness only needs the non-test sources.
	testFiles, _ := filepath.Glob(filepath.Join(dir, "pkg", "codec", "*_test.go"))
	for _, f := range testFiles {
		if err := os.Remove(f); err != nil {
			return "", fmt.Errorf("compat baseline %s: %w", ref, err)
		}
	}

	harnessDir := filepath.Join(dir, "cmd", "compatharness")
	if err := os.MkdirAll(harnessDir, 0o750); err != nil {
		return "", fmt.Errorf("compat baseline %s: %w", ref, err)
	}
	src, err := os.ReadFile(filepath.Join(root, "testdata", "compat", "harness", "main.go"))
	if err != nil {
		return "", fmt.Errorf("compat baseline %s: reading the harness source: %w", ref, err)
	}

	// The module path is not stable across history: v4.0.0 is tagged on a tree
	// whose go.mod declares `module github.com/echotools/tape`, with no /v4
	// suffix, while HEAD declares `github.com/echotools/tape/v4`. The harness
	// must import the codec by the path its baseline actually publishes, so the
	// import is rewritten per baseline rather than hardcoded.
	modPath, err := modulePath(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("compat baseline %s (%s): %w", ref, sha[:8], err)
	}
	src = bytes.ReplaceAll(src,
		[]byte(`"github.com/echotools/tape/v4/pkg/codec"`),
		[]byte(strconv.Quote(modPath+"/pkg/codec")))

	if err := os.WriteFile(filepath.Join(harnessDir, "main.go"), src, 0o600); err != nil {
		return "", fmt.Errorf("compat baseline %s: %w", ref, err)
	}

	binary := filepath.Join(dir, "harness")
	build := exec.Command("go", "build", "-o", binary, "./cmd/compatharness")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	if out, err := build.CombinedOutput(); err != nil {
		// This is NOT a compatibility failure and must not read as one.
		return "", fmt.Errorf("compat baseline %s (%s) NO LONGER BUILDS under this Go toolchain. "+
			"This is a BUILD failure, not a compatibility break: the format promise is untested, "+
			"not broken. Fix the build or retire the baseline deliberately in compatBaselines. "+
			"go build said:\n%s: %w", ref, sha[:8], out, err)
	}
	return binary, nil
}

// modulePath reads the `module` declaration from a go.mod. It is a two-line
// parser rather than a golang.org/x/mod dependency because the only thing
// needed here is the first `module` line, and adding a module-parsing library
// to read one line would be a heavier promise than the job.
func modulePath(gomod string) (string, error) {
	data, err := os.ReadFile(gomod) //nolint:gosec // path is inside a temp dir this test created
	if err != nil {
		return "", fmt.Errorf("reading go.mod: %w", err)
	}
	for line := range strings.Lines(string(data)) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("no module declaration in %s", gomod)
}

// repoRoot returns the working tree root.
func repoRoot() (string, error) {
	out, err := runIn(".", "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func runIn(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// harnessRead runs the baseline reader against a file and returns its one-line
// verdict.
func harnessRead(t *testing.T, binary, path string) string {
	t.Helper()
	out, err := exec.Command(binary, "read", path).CombinedOutput() //nolint:gosec // binary and path are test-controlled
	line := strings.TrimSpace(string(out))
	if err != nil {
		t.Fatalf("baseline reader rejected the file: %s", line)
	}
	return line
}

// TestBackwardCompatibility is the gate. It runs on every `go test ./...` with
// no environment variable, no build tag, and no skip.
func TestBackwardCompatibility(t *testing.T) {
	for _, base := range compatBaselines {
		t.Run(base.Ref, func(t *testing.T) {
			binary := buildBaselineHarness(t, base.Ref)
			dir := t.TempDir()

			// --- Property 1: baseline reader × HEAD's default output ---------
			headDefault := filepath.Join(dir, "head-default.tape")
			writeCompatCapture(t, headDefault)

			want := fmt.Sprintf("OK frames=%d first=0 last=%d footer_frames=%d footer_keyframes=%d",
				compatFrameCount, compatFrameCount-1, compatFrameCount, compatFrameCount/compatKeyframes)
			if got := harnessRead(t, binary, headDefault); got != want {
				t.Errorf("property 1 (baseline reads HEAD's default output):\n got %s\nwant %s", got, want)
			}

			// --- Property 2: HEAD × baseline's output ------------------------
			baselineFile := filepath.Join(dir, "baseline.tape")
			if out, err := exec.Command(binary, "write", baselineFile).CombinedOutput(); err != nil { //nolint:gosec // test-controlled
				t.Fatalf("baseline writer failed: %s", strings.TrimSpace(string(out)))
			}
			headFrames := readCompatCapture(t, baselineFile)
			if len(headFrames) != compatFrameCount {
				t.Fatalf("property 2 (HEAD reads baseline's output): read %d frames, want %d",
					len(headFrames), compatFrameCount)
			}
			for i, want := range compatFrames() {
				if !proto.Equal(want, headFrames[i]) {
					t.Fatalf("property 2: frame %d read back different from what the baseline wrote", i)
				}
			}

			// --- Property 3a: the default LAYOUT is unchanged ----------------
			//
			// The assertion that survives a dependency bump, and where the real
			// protection lives. Compressed bytes belong to klauspost/compress;
			// the DECOMPRESSED envelope stream and the container's frame
			// structure belong to tape. Comparing those catches a layout change
			// on EVERY baseline, including ones whose pins have drifted far
			// enough that comparing compressed bytes would measure the wrong
			// library.
			//
			// Without this, readability alone lets a total container change
			// pass: an old reader that still parses a rewritten layout reports
			// OK, and OK was the entire signal.
			//
			// Decoding is version-independent -- zstd's format is stable even
			// where its ENCODER output is not -- which is what makes this
			// comparison legitimate across a compression-library bump.
			headPlain := decompressAll(t, headDefault)
			basePlain := decompressAll(t, baselineFile)
			if !bytes.Equal(headPlain, basePlain) {
				t.Errorf("property 3a (default layout unchanged): THE DECOMPRESSED ENVELOPE "+
					"STREAM DIFFERS, so the container layout, framing or message encoding "+
					"changed.\n  HEAD     %d bytes sha256:%s\n  %-8s %d bytes sha256:%s",
					len(headPlain), sha256Bytes(headPlain), base.Ref, len(basePlain), sha256Bytes(basePlain))
			}

			// The container's shape, which the decompressed stream cannot show:
			// a default capture is exactly ONE zstd frame and carries no seek
			// table. Splitting the same envelopes across independent frames can
			// leave the decompressed bytes identical, so this is the assertion
			// that catches that.
			if n := countZstdFrames(t, headDefault); n != 1 {
				t.Errorf("property 3a (default layout unchanged): HEAD's default output is %d "+
					"zstd frames, want exactly 1; the default container layout changed", n)
			}
			if n := countZstdFrames(t, baselineFile); n != 1 {
				t.Errorf("property 3a: %s's default output is %d zstd frames, want 1", base.Ref, n)
			}
			if _, err := OpenBlockIndex(headDefault); !errors.Is(err, ErrNoSeekTable) {
				t.Errorf("property 3a (default layout unchanged): HEAD's default output carries "+
					"a seek table (%v); the default container layout changed", err)
			}
			// A receipt on success. A property that silently does not run looks
			// exactly like a property that ran and passed, and this suite has
			// been bitten by that difference more than once; the passing run
			// should say what it checked without anyone having to instrument it.
			t.Logf("property 3a: default layout unchanged — %d-byte decompressed envelope stream "+
				"identical (sha256:%s), 1 zstd frame, no seek table",
				len(headPlain), sha256Bytes(headPlain))

			// --- Property 3b: byte-identical compressed output ---------------
			//
			// The strongest available assertion, and it only means anything when
			// the baseline pins the same output-affecting dependencies as HEAD.
			matched, why := pinsMatchHEAD(t, base.Ref)
			if matched {
				headSum := sha256File(t, headDefault)
				baseSum := sha256File(t, baselineFile)
				if headSum != baseSum {
					t.Errorf("property 3b (compressed output byte-identical): THE DEFAULT ON-DISK "+
						"BYTES CHANGED.\n  HEAD     sha256:%s (%d bytes)\n  %-8s sha256:%s (%d bytes)",
						headSum, fileSize(t, headDefault), base.Ref, baseSum, fileSize(t, baselineFile))
				} else {
					t.Logf("property 3b: compressed output byte-identical, sha256:%s", headSum)
				}
			} else {
				// Not a skip of the property -- a statement that it cannot
				// apply, naming the dependency, so nobody reads silence as a
				// pass. Property 3a above checked the layout regardless.
				t.Logf("property 3b not applicable to %s (%s); zstd and protobuf output bytes "+
					"belong to those libraries, not to this format. Property 3a checked the "+
					"layout instead, and it applies to every baseline.", base.Ref, why)
			}

			// --- Property 4: baseline reader × opt-in per-block layout -------
			perBlock := filepath.Join(dir, "head-perblock.tape")
			writeCompatCapture(t, perBlock, WithPerBlockCompression())

			// Guard the guard: if per-block silently produced the default
			// layout, property 4 would pass by collapsing into property 1.
			index, err := OpenBlockIndex(perBlock)
			if err != nil {
				t.Fatalf("property 4: the per-block capture has no seek table (%v), so this "+
					"assertion would be testing the default layout twice", err)
			}
			if index.Blocks() < 3 {
				t.Fatalf("property 4: per-block capture has %d blocks; it is not actually blocked",
					index.Blocks())
			}
			wantPB := fmt.Sprintf("OK frames=%d first=0 last=%d footer_frames=%d footer_keyframes=%d",
				compatFrameCount, compatFrameCount-1, compatFrameCount, compatFrameCount/compatKeyframes)
			if got := harnessRead(t, binary, perBlock); got != wantPB {
				t.Errorf("property 4 (baseline reads the opt-in per-block layout):\n got %s\nwant %s",
					got, wantPB)
			}
		})
	}
}

// TestCompatBaselinesCanStillCatchAByteChange is the guard on the guard.
//
// Property 3b only runs for baselines whose output-affecting pins match HEAD's.
// That condition is derived, not declared, so it cannot be switched off by
// editing a flag -- but it CAN go away on its own. A klauspost/compress bump
// retires 3b for every existing row at once, and because each row still passes
// its remaining assertions, the suite would keep reporting green while the
// strongest thing it does quietly stopped running.
//
// So: at least one baseline must still be able to perform the check. When a
// dependency bump takes the last one out, this fails and says what to do, which
// makes the disarm an event somebody sees rather than a silence.
func TestCompatBaselinesCanStillCatchAByteChange(t *testing.T) {
	if len(compatBaselines) == 0 {
		t.Fatal("compatBaselines is empty: nothing is checked for backward compatibility at all")
	}

	var capable []string
	reasons := make([]string, 0, len(compatBaselines))
	for _, base := range compatBaselines {
		matched, why := pinsMatchHEAD(t, base.Ref)
		if matched {
			capable = append(capable, base.Ref)
		} else {
			reasons = append(reasons, fmt.Sprintf("    %-10s %s", base.Ref, why))
		}
	}

	if len(capable) == 0 {
		t.Fatalf("NO BASELINE CAN STILL CHECK COMPRESSED-BYTE IDENTITY.\n"+
			"Every row's output-affecting dependencies have drifted from HEAD's, so property 3b "+
			"no longer runs anywhere and a change to the default on-disk BYTES would go "+
			"unnoticed. Property 3a still checks the layout, which is the broader protection, "+
			"but the byte check is gone.\n"+
			"Fix: ADD a baseline at or after the dependency bump -- a release tag if there is "+
			"one, otherwise the commit that performed the bump. Do not remove existing rows; "+
			"baselines accumulate.\nWhy each row is out:\n%s", strings.Join(reasons, "\n"))
	}
	t.Logf("compressed-byte identity is still checkable against: %s", strings.Join(capable, ", "))
}

// TestMain removes the baseline build trees this file caches. They are created
// with os.MkdirTemp rather than t.TempDir because one build is reused across
// sub-tests and must outlive the test that first asked for it -- which means
// nothing else will clean them up. Left alone they accumulated at roughly 17 MB
// per run of this package (measured: 32 directories, 241 MB): disposable on an
// ephemeral CI runner, permanent on a developer's machine or a self-hosted one.
func TestMain(m *testing.M) {
	code := m.Run()
	baselineMu.Lock()
	for _, b := range baselineBuilds {
		if b.binary != "" {
			os.RemoveAll(filepath.Dir(b.binary)) //nolint:errcheck // best-effort cleanup at exit
		}
	}
	baselineMu.Unlock()
	os.Exit(code)
}

// decompressAll returns a capture's decompressed contents: the envelope stream
// exactly as the writer produced it, before compression.
func decompressAll(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatalf("zstd.NewReader: %v", err)
	}
	defer dec.Close()
	out, err := dec.DecodeAll(data, nil)
	if err != nil {
		t.Fatalf("decompressing %s: %v", path, err)
	}
	return out
}

func sha256Bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// readCompatCapture reads a capture with HEAD's reader.
func readCompatCapture(t *testing.T, path string) []*capturepb.Frame {
	t.Helper()
	r, err := NewReader(path)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close() //nolint:errcheck // read-only

	if _, err := r.ReadHeader(); err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	var frames []*capturepb.Frame
	for {
		f, err := r.ReadFrame()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		frames = append(frames, f)
	}
	return frames
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Size()
}
