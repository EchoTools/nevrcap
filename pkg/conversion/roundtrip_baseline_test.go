package conversion

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/echotools/tape/v4/pkg/codec"
	"google.golang.org/protobuf/proto"
)

// TestEchoReplayRoundTripFidelity is the ABSOLUTE BASELINE: a real echoreplay
// read -> written -> read again must preserve every SessionResponse field
// (weapon, ordnance, possession, grab state, everything). This proves whether
// the echoreplay codec itself is lossless, independent of the v2 conversion.
func TestEchoReplayRoundTripFidelity(t *testing.T) {
	src := filepath.Join("..", "..", "testdata", "sample.echoreplay")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no sample echoreplay: %v", err)
	}

	r1, err := codec.NewEchoReplayReader(src)
	if err != nil {
		t.Fatalf("open original: %v", err)
	}
	orig, err := r1.ReadFrames()
	_ = r1.Close()
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	if len(orig) == 0 {
		t.Fatal("original had zero frames")
	}

	tmp := filepath.Join(t.TempDir(), "roundtrip.echoreplay")
	w, err := codec.NewEchoReplayWriter(tmp)
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	for _, f := range orig {
		if err := w.WriteFrame(f); err != nil {
			t.Fatalf("write frame: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	r2, err := codec.NewEchoReplayReader(tmp)
	if err != nil {
		t.Fatalf("open round-tripped: %v", err)
	}
	rt, err := r2.ReadFrames()
	_ = r2.Close()
	if err != nil {
		t.Fatalf("read round-tripped: %v", err)
	}

	if len(rt) != len(orig) {
		t.Fatalf("frame count changed: orig %d -> round-trip %d", len(orig), len(rt))
	}

	sessionMismatch := 0
	frameMismatch := 0
	for i := range orig {
		if !proto.Equal(orig[i].GetSession(), rt[i].GetSession()) {
			sessionMismatch++
			if sessionMismatch <= 3 {
				t.Errorf("frame %d: SessionResponse lost fidelity in round-trip", i)
			}
		}
		if !proto.Equal(orig[i], rt[i]) {
			frameMismatch++
		}
	}
	t.Logf("round-tripped %d frames | SessionResponse mismatches: %d | whole-frame mismatches: %d",
		len(orig), sessionMismatch, frameMismatch)
	if sessionMismatch > 0 {
		t.Fatalf("ECHOREPLAY ROUND-TRIP IS LOSSY: %d/%d frames lost SessionResponse data", sessionMismatch, len(orig))
	}
}
