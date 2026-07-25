package conversion

import (
	"archive/zip"
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- unit tests for the key-set completeness guard ---------------------------
//
// The guard exists because the round-trip audit is blind by construction: it
// compares frames parsed from the ORIGINAL against frames parsed from the
// REWRITE, and the echoreplay reader is built with
// protojson.UnmarshalOptions{DiscardUnknown: true} (pkg/codec/echoreplay.go).
// A JSON key the proto has no field for is therefore dropped AT READ TIME, so
// it is absent from BOTH sides of the comparison and the audit calls the file
// LOSSLESS. Reproduced: a real recording rebuilt with "unknown_future_field"
// injected into all 197 session objects audited LOSSLESS with 0 mismatches.
//
// The audit is the floor for deciding whether irreplaceable recordings can be
// deleted, so it must be able to see content the codec silently discards. The
// guard reads the ORIGINAL FILE'S BYTES and checks every JSON key present in
// its session objects against the proto descriptor.

const testInjectedKey = `"unknown_future_field":12345,`

// TestSessionKeySetGuard_FlagsInjectedUnknownKey is the guard's red: a
// recording that carries a key the proto has no field for must be reported,
// with the key's name and occurrence count.
func TestSessionKeySetGuard_FlagsInjectedUnknownKey(t *testing.T) {
	src := "../../testdata/sample.echoreplay"
	dst := filepath.Join(t.TempDir(), "sample_unknownkey.echoreplay")
	injected := injectSessionKey(t, src, dst, testInjectedKey)
	if injected == 0 {
		t.Fatalf("fixture built with 0 injections")
	}

	res, err := auditSessionKeys(dst, 0)
	if err != nil {
		t.Fatalf("guard: %v", err)
	}
	if len(res.Findings) == 0 {
		t.Fatalf("guard reported no findings on a file carrying %d copies of %s; "+
			"the audit cannot see content the codec discards at read time",
			injected, testInjectedKey)
	}

	var total int
	var named bool
	for _, f := range res.Findings {
		total += f.Count
		if strings.HasSuffix(f.Path, ".unknown_future_field") {
			named = true
		}
	}
	if !named {
		t.Errorf("findings do not name unknown_future_field: %v", res.Findings)
	}
	if total != injected {
		t.Errorf("occurrence count = %d, want %d (one per injected session object)", total, injected)
	}
	t.Logf("RED: %d frames scanned, findings: %s", res.FramesScanned, res.Summary())
}

// TestSessionKeySetGuard_FlagsNestedUnknownKey pins the nesting requirement: a
// top-level-only check would be worthless, because the levels that carry the
// per-team and per-player data are exactly the ones worth not losing.
func TestSessionKeySetGuard_FlagsNestedUnknownKey(t *testing.T) {
	for _, tc := range []struct {
		name     string
		marker   string
		wantPath string
	}{
		{"team", `"teams":[`, "SessionResponse.teams[].unknown_future_field"},
		{"team_member", `"players":[`, "SessionResponse.teams[].players[].unknown_future_field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := "../../testdata/sample.echoreplay"
			dst := filepath.Join(t.TempDir(), "sample_nestedkey.echoreplay")
			injected := injectAfterArrayOpen(t, src, dst, tc.marker, testInjectedKey)
			if injected == 0 {
				t.Fatalf("fixture built with 0 nested injections at %s", tc.marker)
			}

			res, err := auditSessionKeys(dst, 0)
			if err != nil {
				t.Fatalf("guard: %v", err)
			}
			var named bool
			for _, f := range res.Findings {
				if f.Path == tc.wantPath {
					named = true
				}
			}
			if !named {
				t.Fatalf("nested key at %s not reported as %s; findings: %v", tc.marker, tc.wantPath, res.Findings)
			}
			t.Logf("RED (nested %s): %s", tc.name, res.Summary())
		})
	}
}

// TestSessionKeySetGuard_CleanOnRealRecording is the guard's green: the
// committed real recording must report zero findings, so the guard fails only
// on files that actually carry unrepresentable content.
func TestSessionKeySetGuard_CleanOnRealRecording(t *testing.T) {
	res, err := auditSessionKeys("../../testdata/sample.echoreplay", 0)
	if err != nil {
		t.Fatalf("guard: %v", err)
	}
	if len(res.Findings) != 0 {
		t.Fatalf("guard flagged an unmodified real recording: %s", res.Summary())
	}
	t.Logf("GREEN: %s", res.Summary())
}

// TestSessionKeySetGuard_SamplingIsReported pins that a sampled scan says so:
// a guard that quietly looks at 1%% of a 100k-frame file and prints OK is the
// same blindness this whole file exists to remove.
func TestSessionKeySetGuard_SamplingIsReported(t *testing.T) {
	res, err := auditSessionKeys("../../testdata/sample.echoreplay", 10)
	if err != nil {
		t.Fatalf("guard: %v", err)
	}
	// budget+1: the last frame is always included on top of the strided
	// sample, so the scan may exceed the budget by exactly one.
	if res.FramesScanned > 11 {
		t.Errorf("budget 10 honoured? scanned %d", res.FramesScanned)
	}
	if res.FramesScanned >= res.FramesTotal {
		t.Fatalf("expected a sampled scan, got %d/%d", res.FramesScanned, res.FramesTotal)
	}
	if !strings.Contains(strings.ToLower(res.Summary()), "sampled") {
		t.Errorf("sampled scan does not announce sampling: %q", res.Summary())
	}
	t.Logf("SAMPLED: %s", res.Summary())
}

// --- fixture builders --------------------------------------------------------

// injectSessionKey rebuilds src at dst with raw inserted directly after the
// opening brace of every session object, and returns the number of insertions.
func injectSessionKey(t *testing.T, src, dst, raw string) int {
	t.Helper()
	return rebuildEchoReplay(t, src, dst, func(session []byte) ([]byte, int) {
		if len(session) == 0 || session[0] != '{' {
			return session, 0
		}
		out := make([]byte, 0, len(session)+len(raw))
		out = append(out, '{')
		out = append(out, raw...)
		out = append(out, session[1:]...)
		return out, 1
	})
}

// injectAfterArrayOpen rebuilds src at dst with raw inserted into the first
// object of the array named by marker (e.g. `"teams":[`), exercising the
// nested levels of the guard.
func injectAfterArrayOpen(t *testing.T, src, dst, markerStr, raw string) int {
	t.Helper()
	marker := []byte(markerStr)
	return rebuildEchoReplay(t, src, dst, func(session []byte) ([]byte, int) {
		i := bytes.Index(session, marker)
		if i < 0 {
			return session, 0
		}
		j := i + len(marker)
		if j >= len(session) || session[j] != '{' {
			return session, 0
		}
		out := make([]byte, 0, len(session)+len(raw))
		out = append(out, session[:j+1]...)
		out = append(out, raw...)
		out = append(out, session[j+1:]...)
		return out, 1
	})
}

// rebuildEchoReplay copies src to dst line for line, applying edit to each
// line's session JSON (field 2 of the TSV). The zip member keeps its original
// name so the codec's member-selection logic resolves it identically.
func rebuildEchoReplay(t *testing.T, src, dst string, edit func([]byte) ([]byte, int)) int {
	t.Helper()

	zr, err := zip.OpenReader(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer func() { _ = zr.Close() }()

	member := pickReplayMember(t, zr, src)
	rc, err := member.Open()
	if err != nil {
		t.Fatalf("open member: %v", err)
	}
	defer func() { _ = rc.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	defer func() { _ = out.Close() }()

	zw := zip.NewWriter(out)
	w, err := zw.Create(member.Name)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 64*1024), 10*1024*1024)
	edits := 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		parts := bytes.Split(line, []byte("\t"))
		if len(parts) >= 2 {
			edited, n := edit(parts[1])
			parts[1] = edited
			edits += n
		}
		if _, err := w.Write(bytes.Join(parts, []byte("\t"))); err != nil {
			t.Fatalf("write line: %v", err)
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			t.Fatalf("write newline: %v", err)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return edits
}

func pickReplayMember(t *testing.T, zr *zip.ReadCloser, name string) *zip.File {
	t.Helper()
	f, err := replayMember(&zr.Reader, name)
	if err != nil {
		t.Fatalf("member: %v", err)
	}
	return f
}
