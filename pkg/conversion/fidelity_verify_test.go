package conversion

import (
	"archive/zip"
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/echotools/tape/pkg/fidelity"
)

// fidelity_verify_test.go — the per-file verifier's own red and green, on
// constructed fixtures.
//
// These are the cases that must FAIL a verification even though the field-level
// comparison finds nothing wrong, because the lost content never reached the
// comparison at all. Each one is a way a file could be deleted on the strength
// of a verdict that proved nothing.

// TestVerifyFailsOnUnparseableFrame is the red for the drop-at-read lane. A
// frame whose session JSON does not parse is skipped by the codec
// (pkg/codec/echoreplay.go: skippedFrames++ and continue), so it is absent from
// the original side, absent from the reconstruction, and absent from both frame
// counts. Nothing in a field comparison can see it. The verdict has to.
func TestVerifyFailsOnUnparseableFrame(t *testing.T) {
	src := "../../testdata/sample.echoreplay"
	dst := filepath.Join(t.TempDir(), "one_broken_frame.echoreplay")
	broken := 0
	rebuildEchoReplay(t, src, dst, 1, func(session []byte) ([]byte, int) {
		if broken > 0 {
			return session, 0
		}
		broken++
		return []byte(`{"sessionid": "unterminated`), 1
	})
	if broken != 1 {
		t.Fatalf("fixture built with %d broken frames, want 1", broken)
	}

	v, err := VerifyEchoReplayRoundTrip(dst, VerifyOptions{Allowlist: KnownUnpreserved})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if v.Pass() {
		t.Fatalf("a file with a dropped frame passed verification:\n%s", v.String())
	}
	reasons := strings.Join(v.FailureReasons(), "\n")
	if !strings.Contains(reasons, "unparseable frame") && !strings.Contains(reasons, "never reached it") {
		t.Errorf("the verdict fails, but not because of the dropped frame:\n%s", reasons)
	}
	if v.SkippedFrames == 0 && v.KeyScan.ParseErrors == 0 {
		t.Errorf("neither the codec's skip counter nor the key scanner noticed the dropped frame:\n%s", v.String())
	}
	t.Logf("RED (dropped frame): skipped=%d json-unparseable=%d frames=%d/%d\n%s",
		v.SkippedFrames, v.KeyScan.ParseErrors, v.FramesOriginal, v.KeyScan.FramesTotal,
		strings.Join(v.FailureReasons(), "\n"))
}

// TestVerifyFailsOnUnrepresentableKey is the red for the discarded-key lane:
// content the reader throws away is missing from BOTH sides, so the comparison
// reports zero differences and would call the file safe to delete.
func TestVerifyFailsOnUnrepresentableKey(t *testing.T) {
	src := "../../testdata/sample.echoreplay"
	dst := filepath.Join(t.TempDir(), "unknown_key.echoreplay")
	if n := injectPayloadKey(t, src, dst, 1, testInjectedKey); n == 0 {
		t.Fatal("fixture built with 0 injections")
	}

	v, err := VerifyEchoReplayRoundTrip(dst, VerifyOptions{Allowlist: KnownUnpreserved})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if v.Pass() {
		t.Fatalf("a file carrying an unrepresentable key passed verification:\n%s", v.String())
	}
	if len(v.KeyScan.Findings) == 0 {
		t.Fatalf("the verdict carries no key findings:\n%s", v.String())
	}
	t.Logf("RED (unknown key): %s", v.KeyScan.Summary())
}

// TestVerifyIdentifiesItsSource pins the receipt property: a verdict is about a
// specific set of BYTES, not about a filename. Deleting an original on the
// strength of "some file at this path verified" is how the wrong file gets
// deleted.
func TestVerifyIdentifiesItsSource(t *testing.T) {
	src := "../../testdata/sample.echoreplay"
	v, err := VerifyEchoReplayRoundTrip(src, VerifyOptions{Allowlist: KnownUnpreserved})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(v.SourceSHA256) != 64 {
		t.Errorf("verdict carries no content hash: %q", v.SourceSHA256)
	}
	if v.SourceBytes == 0 {
		t.Error("verdict carries no source size")
	}
	if v.FramesOriginal == 0 || v.FramesOriginal != v.KeyScan.FramesTotal {
		t.Errorf("frames compared (%d) does not match frame lines in the file (%d)",
			v.FramesOriginal, v.KeyScan.FramesTotal)
	}
	t.Logf("receipt: %d bytes sha256=%s frames=%d", v.SourceBytes, v.SourceSHA256, v.FramesOriginal)
}

// TestVerifyHasNoSkipKnob is the D1 pin. The key scan is the only lane that can
// see content the READER discarded; a verification that decides whether an
// irreplaceable recording may be deleted cannot have a knob that turns it off.
// It had one (SkipKeyScan), and with it set the receipt still printed
// "keyscan=0/0 frames (exhaustive) unknown-keys=0" over a file with 1023
// discarded JSON keys, and passed.
//
// Reflection, not a comment, because a comment cannot fail.
func TestVerifyHasNoSkipKnob(t *testing.T) {
	rt := reflect.TypeOf(VerifyOptions{})
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		if strings.Contains(strings.ToLower(name), "skip") {
			t.Errorf("VerifyOptions.%s: a verification that authorizes deleting the original "+
				"must not carry a knob that skips a lane", name)
		}
	}
}

// TestVerifyScanIsAlwaysWitnessed pins the other half of D1: the receipt states
// the scan it actually performed, over the same number of frames the file has.
func TestVerifyScanIsAlwaysWitnessed(t *testing.T) {
	src := filepath.Join("..", "..", "testdata", "sample.echoreplay")
	v, err := VerifyEchoReplayRoundTrip(src, VerifyOptions{Allowlist: KnownUnpreserved})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !v.KeyScan.Ran {
		t.Fatalf("the key scan did not run:\n%s", v.String())
	}
	if !v.Complete() {
		t.Errorf("a verification that returned without error is not marked complete:\n%s", v.String())
	}
	if v.KeyScan.FramesScanned == 0 || v.KeyScan.FramesScanned != v.KeyScan.FramesTotal {
		t.Errorf("scan covered %d of %d frame lines", v.KeyScan.FramesScanned, v.KeyScan.FramesTotal)
	}
	t.Logf("GREEN (scan witnessed): %s", v.KeyScan.Summary())
}

// TestVerifyFailsOnExtraTabField is the red for D3(a). pkg/codec's
// parseFrameLine reads parts[0..2] and drops the rest of the line, so a 4th
// tab-separated payload is content that exists in the file, never enters the
// comparison, and cannot be reconstructed. Before this lane the file below
// produced a clean FIDELITY PASS.
func TestVerifyFailsOnExtraTabField(t *testing.T) {
	src := filepath.Join("..", "..", "testdata", "sample.echoreplay")
	dst := filepath.Join(t.TempDir(), "extra_tab_field.echoreplay")
	n := rebuildWithExtraTabField(t, src, dst, "{\"unreachable\":\"payload\"}")
	if n == 0 {
		t.Fatal("fixture built with 0 extra fields")
	}

	v, err := VerifyEchoReplayRoundTrip(dst, VerifyOptions{Allowlist: KnownUnpreserved})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if v.Pass() {
		t.Fatalf("a file with a discarded 4th payload passed verification:\n%s", v.String())
	}
	reasons := strings.Join(v.FailureReasons(), "\n")
	if !strings.Contains(reasons, "tab-separated field") {
		t.Errorf("the verdict does not name the discarded payload:\n%s", reasons)
	}
	t.Logf("RED (extra tab field): %s", v.KeyScan.Summary())
}

// TestVerifyFailsOnSecondZipMember is the red for D3(b). Both the codec's
// reader and the scanner bind ONE member of the archive; a second member is
// content the round trip cannot represent, and it was silently discarded.
func TestVerifyFailsOnSecondZipMember(t *testing.T) {
	src := filepath.Join("..", "..", "testdata", "sample.echoreplay")
	dst := filepath.Join(t.TempDir(), "two_members.echoreplay")
	rebuildWithSecondMember(t, src, dst, "second.echoreplay")

	v, err := VerifyEchoReplayRoundTrip(dst, VerifyOptions{Allowlist: KnownUnpreserved})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if v.Pass() {
		t.Fatalf("a file with a discarded second zip member passed verification:\n%s", v.String())
	}
	reasons := strings.Join(v.FailureReasons(), "\n")
	if !strings.Contains(reasons, "zip member") {
		t.Errorf("the verdict does not name the discarded member:\n%s", reasons)
	}
	t.Logf("RED (second zip member): %s", v.KeyScan.Summary())
}

// TestCorpusShapeIsOneMemberThreeFields asserts the shape the two lanes above
// are calibrated against, rather than assuming it. If a real recording ever
// carries a 4th field or a second member, this test says so — and the lanes are
// then correctly failing on real content, not on a bad assumption.
//
// Set TAPE_AUDIT_FILE to assert the same shape on an external recording.
func TestCorpusShapeIsOneMemberThreeFields(t *testing.T) {
	files := []string{filepath.Join("..", "..", "testdata", "sample.echoreplay")}
	if extra := os.Getenv("TAPE_AUDIT_FILE"); extra != "" {
		files = append(files, extra)
	}
	for _, src := range files {
		t.Run(filepath.Base(src), func(t *testing.T) {
			zr, err := zip.OpenReader(src)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			members := len(zr.File)
			_ = zr.Close()
			if members != 1 {
				t.Errorf("%s has %d zip members, want 1", src, members)
			}

			counts := map[int]int{}
			lines := 0
			if err := fidelity.EachFrameLine(src, func(_ int, line []byte) error {
				counts[bytes.Count(line, []byte("\t"))+1]++
				lines++
				return nil
			}); err != nil {
				t.Fatalf("scan lines: %v", err)
			}
			if lines == 0 {
				t.Fatal("no frame lines")
			}
			for n := range counts {
				if n > 3 {
					t.Errorf("%s has %d line(s) with %d tab-separated fields, want at most 3", src, counts[n], n)
				}
			}
			t.Logf("%s: members=%d lines=%d tab-field histogram=%v", filepath.Base(src), members, lines, counts)
		})
	}
}

// TestKnownUnpreservedExcusesNothingBeneathItself is the D5 pin, and it is the
// reason the red got much larger. An allowlist entry records that a FIELD has
// no v2 home. Absorbing its whole subtree also excused a reconstruction that
// returned a DIFFERENT VALUE for a sub-field present on both sides — 52 of 138
// SessionResponse paths could not fail at all. Andrew's ruling is errors on any
// difference; an excuse is per path, and only where it was ruled on.
//
// It also reports every path the narrowing un-excused, which is the list the
// ruling on FIDELITY-002 needs.
func TestKnownUnpreservedExcusesNothingBeneathItself(t *testing.T) {
	schemas, err := EchoReplaySchemas()
	if err != nil {
		t.Fatalf("schemas: %v", err)
	}
	var unexcused []string
	for _, s := range schemas {
		for _, p := range s.Paths() {
			for _, suffix := range []string{"", "#presence", "#count"} {
				path := p + suffix
				_, allowed := KnownUnpreserved.Reason(path)
				byParent := excusedByAnAncestor(path)
				if allowed && byParent {
					t.Errorf("%s is excused only because an ANCESTOR is allowlisted: "+
						"a wrong value there cannot fail", path)
				}
				if byParent && !allowed {
					unexcused = append(unexcused, path)
				}
			}
		}
	}
	sort.Strings(unexcused)
	t.Logf("D5 narrowing un-excused %d path(s) that subtree matching absorbed:\n  %s",
		len(unexcused), strings.Join(unexcused, "\n  "))
}

// excusedByAnAncestor reports whether some STRICT ancestor of path is an
// allowlist entry — the old subtree semantics, kept here only to measure what
// they were hiding.
func excusedByAnAncestor(path string) bool {
	for k := range KnownUnpreserved {
		if len(path) <= len(k) || !strings.HasPrefix(path, k) {
			continue
		}
		switch path[len(k)] {
		case '.', '[', '{':
			return true
		}
	}
	return false
}

// --- fixture builders ---------------------------------------------------------

// rebuildWithExtraTabField copies src to dst appending one more tab-separated
// field to every frame line, and returns the number of lines it extended.
func rebuildWithExtraTabField(t *testing.T, src, dst, payload string) int {
	t.Helper()
	n := 0
	copyEchoReplayLines(t, src, dst, "", func(line []byte) []byte {
		n++
		out := append([]byte(nil), line...)
		out = append(out, '\t')
		return append(out, payload...)
	})
	return n
}

// rebuildWithSecondMember copies src to dst and adds a SECOND .echoreplay
// member carrying real frame lines. Only one member is ever read.
func rebuildWithSecondMember(t *testing.T, src, dst, secondName string) {
	t.Helper()
	copyEchoReplayLines(t, src, dst, secondName, nil)
}

// copyEchoReplayLines rebuilds src at dst, optionally editing each line and
// optionally writing a second member holding the same lines.
func copyEchoReplayLines(t *testing.T, src, dst, secondName string, edit func([]byte) []byte) {
	t.Helper()

	zr, err := zip.OpenReader(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer func() { _ = zr.Close() }()
	member := pickReplayMember(t, zr, src)

	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	defer func() { _ = out.Close() }()
	zw := zip.NewWriter(out)

	var second io.Writer
	writeAll := func(w io.Writer) {
		rc, err := member.Open()
		if err != nil {
			t.Fatalf("open member: %v", err)
		}
		defer func() { _ = rc.Close() }()
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 64*1024), 10*1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			if edit != nil {
				line = edit(line)
			}
			if _, err := w.Write(line); err != nil {
				t.Fatalf("write line: %v", err)
			}
			if _, err := io.WriteString(w, "\n"); err != nil {
				t.Fatalf("write newline: %v", err)
			}
		}
		if err := sc.Err(); err != nil {
			t.Fatalf("scan: %v", err)
		}
	}

	w, err := zw.Create(member.Name)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	writeAll(w)

	if secondName != "" {
		second, err = zw.Create(secondName)
		if err != nil {
			t.Fatalf("create second member: %v", err)
		}
		writeAll(second)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}
