package codec

import (
	"errors"
	"testing"
)

// The accumulation guards must agree with Limits.checkFrameBudget on the
// boundary. A capture holding exactly MaxFrameCount frames is AT budget, not
// over it, and must load. The guards used to run before the read with >=, so
// the iteration that would have returned EOF tripped the guard first and a
// capture of exactly the budgeted size was refused.

const sampleEchoReplayFrames = 1023

func TestEchoReplay_ReadFrames_ExactlyAtBudgetSucceeds(t *testing.T) {
	r, err := NewEchoReplayReader("../../testdata/sample.echoreplay",
		WithMaxFrameCount(sampleEchoReplayFrames))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close() //nolint:errcheck // test cleanup

	frames, err := r.ReadFrames()
	if err != nil {
		t.Fatalf("a capture of exactly MaxFrameCount frames must load, got %v", err)
	}
	if len(frames) != sampleEchoReplayFrames {
		t.Fatalf("got %d frames, want %d", len(frames), sampleEchoReplayFrames)
	}
}

func TestEchoReplay_ReadFrames_OverBudgetStillRejected(t *testing.T) {
	r, err := NewEchoReplayReader("../../testdata/sample.echoreplay",
		WithMaxFrameCount(sampleEchoReplayFrames-1))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close() //nolint:errcheck // test cleanup

	if _, err := r.ReadFrames(); !errors.Is(err, ErrMaxFrameCount) {
		t.Fatalf("a capture exceeding MaxFrameCount must be refused, got %v", err)
	}
}
