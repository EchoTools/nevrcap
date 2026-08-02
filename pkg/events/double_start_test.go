package events

import (
	"testing"
	"time"
)

// TestAsyncDetector_DoubleStartDoesNotRace proves R3: New() already starts the
// background processLoop, and the public Start() must be idempotent so a
// redundant call cannot spawn a second worker. Pre-fix, a second Start() made
// two processLoop goroutines pull frames from the same inputChan and mutate the
// same shared state — writeIndex, frameCount, frameBuffer (events.go:330-337)
// and the reused eventBuffer — with no synchronization. Under -race that is
// reported as data races.
func TestAsyncDetector_DoubleStartDoesNotRace(t *testing.T) {
	det := NewWithDefaultSensors()
	det.Start() // redundant second start — must be a no-op
	t.Cleanup(det.Stop)

	// Feed far more event-producing frames than the input channel can buffer so
	// both hypothetical workers would pull frames concurrently and interleave
	// writes to the shared ring buffer and reused event buffer.
	for i := int32(1); i <= 2000; i++ {
		det.ProcessFrame(scoreFrame(i))
	}
}

// TestAsyncDetector_StartAfterStopIsNoop pins the documented semantics of Start
// after Stop: a stopped detector cannot be restarted, so Start is a no-op. The
// Stop-Start-Stop sequence must neither hang nor panic, and the events channel
// stays closed.
func TestAsyncDetector_StartAfterStopIsNoop(t *testing.T) {
	det := New()
	det.Stop()

	// Must not spawn a loop or panic.
	det.Start()

	// Stop is idempotent via stopOnce; calling it again must not panic.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Start-after-Stop sequence panicked: %v", r)
			}
		}()
		det.Stop()
	}()

	// The events channel must remain closed (the detector is done).
	select {
	case _, ok := <-det.EventsChan():
		if ok {
			t.Fatal("expected events channel to be closed after Stop")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for events channel close")
	}
}
