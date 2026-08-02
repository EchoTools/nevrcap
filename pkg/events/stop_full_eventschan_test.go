package events

import (
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
)

// TestAsyncDetector_StopWithFullUndrainedEventsChanReturns proves GH #26: Stop()
// must return within a bounded time even when eventsChan is full and nothing is
// draining it. The issue's claimed deadlock (processLoop blocked on a full
// eventsChan send while Stop() wg.Wait()s) cannot occur because the send in
// processLoop is non-blocking: a full channel takes the default branch and drops
// the batch instead of blocking (events.go:292-303). This test feeds more
// event-producing frames than the events channel can buffer, never drains, and
// requires Stop() to complete.
func TestAsyncDetector_StopWithFullUndrainedEventsChanReturns(t *testing.T) {
	det := NewWithDefaultSensors()

	// Prime the sensors; this frame enqueues nothing.
	det.ProcessFrame(&telemetry.LobbySessionStateFrame{Session: &enginev1.SessionResponse{}})

	// Feed more event-producing frames than the events channel buffer (10) can
	// hold, without ever draining. This guarantees eventsChan is full while the
	// processLoop may still be sending.
	for i := int32(1); i <= batchOverflowCount; i++ {
		det.ProcessFrame(scoreFrame(i))
	}

	// Stop() must return even though eventsChan is full and nothing drains it.
	stopped := make(chan struct{})
	go func() {
		det.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
		// Stop returned within the deadline — no deadlock.
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() deadlocked with a full, undrained events channel")
	}
}
