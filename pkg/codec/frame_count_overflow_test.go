package codec

import (
	"errors"
	"math"
	"path/filepath"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
)

// TestWriter_WriteFrame_OverflowErrors proves GH #21: when frameCount reaches
// math.MaxUint32, the next WriteFrame returns ErrFrameCountOverflow instead of
// wrapping back to 0 and corrupting the footer.
func TestWriter_WriteFrame_OverflowErrors(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "overflow.tape")
	w, err := NewWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	// Force the counter to just below overflow so we don't write 4B frames.
	w.frameCount = math.MaxUint32

	frame := &capturepb.Frame{FrameIndex: 0}
	err = w.WriteFrame(frame)
	if !errors.Is(err, ErrFrameCountOverflow) {
		t.Fatalf("WriteFrame at MaxUint32: want ErrFrameCountOverflow, got %v", err)
	}
	_ = w.Close()
}

// TestWriter_WriteFrame_BelowMaxSucceeds proves the companion case: writing
// when frameCount is MaxUint32-1 succeeds (no false positive on the overflow
// check).
func TestWriter_WriteFrame_BelowMaxSucceeds(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "belowmax.tape")
	w, err := NewWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	w.frameCount = math.MaxUint32 - 1

	frame := &capturepb.Frame{FrameIndex: 0, Payload: &capturepb.Frame_EchoArena{
		EchoArena: &capturepb.EchoArenaFrame{},
	}}
	if err := w.WriteFrame(frame); err != nil {
		t.Fatalf("WriteFrame at MaxUint32-1: %v", err)
	}
	if w.frameCount != math.MaxUint32 {
		t.Errorf("frameCount = %d, want MaxUint32", w.frameCount)
	}
	_ = w.Close()
}
