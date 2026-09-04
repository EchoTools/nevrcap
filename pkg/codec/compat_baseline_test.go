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
// — "we can still read what v4.0.0 wrote" — and replacing an old entry with a
// newer one silently retires a promise nobody agreed to retire. Add a row when
// a release is tagged; remove one only as a deliberate, announced decision to
// stop supporting that version.
type compatBaseline struct {
	// Ref is a tag or commit resolvable by git in this repository. A tag is
	// preferred: it names a release rather than a moment.
	Ref string
	// Why records what this baseline represents, so a later reader can judge
	// whether it is still worth carrying.
	Why string
	// ByteIdentical says whether property 3 applies. It does NOT apply across
	// a change to the compression library: zstd's encoder output is a property
	// of klauspost/compress, not of this format, so comparing bytes across a
	// version bump measures the wrong thing and would fail for a reason that
	// has nothing to do with tape. Readability (1, 2, 4) is checked for every
	// baseline regardless — surviving a dependency bump is precisely what
	// readability is for.
	ByteIdentical bool
}

var compatBaselines = []compatBaseline{
	{
		Ref:           "v4.0.0",
		Why:           "the released format version, tagged 2026-08-26",
		ByteIdentical: false, // pins klauspost/compress v1.18.6; HEAD pins v1.19.2
	},
	{
		Ref:           "2ca18fa",
		Why:           "last commit before per-block compression; same dependency pins as HEAD",
		ByteIdentical: true,
	},
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

			// --- Property 3: byte-identical default output -------------------
			if base.ByteIdentical {
				headSum := sha256File(t, headDefault)
				baseSum := sha256File(t, baselineFile)
				if headSum != baseSum {
					t.Errorf("property 3 (default output is byte-identical): THE DEFAULT ON-DISK "+
						"LAYOUT CHANGED.\n  HEAD     sha256:%s (%d bytes)\n  %-8s sha256:%s (%d bytes)",
						headSum, fileSize(t, headDefault), base.Ref, baseSum, fileSize(t, baselineFile))
				} else {
					t.Logf("property 3: default output byte-identical, sha256:%s", headSum)
				}
			} else {
				// Not a skip of the property — a statement that it does not
				// apply, with the reason, so nobody reads silence as a pass.
				t.Logf("property 3 not applicable to %s: its go.mod pins a different "+
					"klauspost/compress, so zstd output bytes are a property of that library "+
					"rather than of this format. Readability is still checked below.", base.Ref)
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
