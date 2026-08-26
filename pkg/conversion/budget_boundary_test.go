package conversion_test

import (
	"errors"
	"testing"

	"github.com/echotools/tape/v4/pkg/codec"
	"github.com/echotools/tape/v4/pkg/conversion"
)

// Companion to pkg/codec's boundary test: the two accumulators in this package
// had the same off-by-one. A capture holding exactly MaxFrameCount frames is AT
// budget and must load; only a capture past it is refused.

const goldenTapeFrames = 1023

func openGolden(t *testing.T, budget int64) *codec.Reader {
	t.Helper()
	r, err := codec.NewReader("../../testdata/sample.tape.golden", codec.WithMaxFrameCount(budget))
	if err != nil {
		t.Fatalf("open golden: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestOpenSession_ExactlyAtBudgetSucceeds(t *testing.T) {
	sess, err := conversion.OpenSession(openGolden(t, goldenTapeFrames))
	if err != nil {
		t.Fatalf("a capture of exactly MaxFrameCount frames must load, got %v", err)
	}
	if sess == nil {
		t.Fatal("session is nil")
	}
}

func TestOpenSession_OverBudgetStillRejected(t *testing.T) {
	if _, err := conversion.OpenSession(openGolden(t, goldenTapeFrames-1)); !errors.Is(err, codec.ErrMaxFrameCount) {
		t.Fatalf("a capture exceeding MaxFrameCount must be refused, got %v", err)
	}
}

func TestNewSessionReconstructor_ExactlyAtBudgetSucceeds(t *testing.T) {
	rec, err := conversion.NewSessionReconstructor(openGolden(t, goldenTapeFrames))
	if err != nil {
		t.Fatalf("a capture of exactly MaxFrameCount frames must load, got %v", err)
	}
	if rec == nil {
		t.Fatal("reconstructor is nil")
	}
}

func TestNewSessionReconstructor_OverBudgetStillRejected(t *testing.T) {
	if _, err := conversion.NewSessionReconstructor(openGolden(t, goldenTapeFrames-1)); !errors.Is(err, codec.ErrMaxFrameCount) {
		t.Fatalf("a capture exceeding MaxFrameCount must be refused, got %v", err)
	}
}
