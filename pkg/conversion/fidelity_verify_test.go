package conversion

import (
	"path/filepath"
	"strings"
	"testing"
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
