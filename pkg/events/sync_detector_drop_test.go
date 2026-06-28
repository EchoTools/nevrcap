package events

import (
	"testing"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
)

// scoreFrame builds a frame whose blue score forces a ScoreboardUpdated event
// relative to any different previous score, so each successive distinct value
// yields exactly one event batch.
func scoreFrame(bluePoints int32) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			GameStatus: GameStatusPlaying,
			BluePoints: bluePoints,
		},
	}
}

// drainChan non-blockingly drains every queued batch, returning the total
// number of events drained. This mirrors pkg/conversion/convert.go's
// drainEvents (convert.go:252-261).
func drainChan(det *AsyncDetector) int {
	n := 0
	for {
		select {
		case batch := <-det.EventsChan():
			n += len(batch)
		default:
			return n
		}
	}
}

// batchOverflowCount is one larger than the events channel buffer (10, set in
// events.go:117). Feeding this many event-producing frames without draining
// guarantees the buffer overflows and the drop branch (events.go:219-223) runs.
const batchOverflowCount = 25

// TestSyncDetector_ConversionPatternNoEventLoss reproduces the conversion
// pipeline's exact use of the synchronous detector: one ProcessFrame per frame
// (pkg/processing/frame_processor.go:80) followed by a full drain of the events
// channel before the next frame (pkg/conversion/convert.go:229,232,252-261).
// Under that pattern the events channel never holds more than one batch, so the
// drop branch in processFrameSync (events.go:219-223) is unreachable and no
// events are lost — even across far more frames than the channel can buffer and
// even when one frame emits a large simultaneous batch.
func TestSyncDetector_ConversionPatternNoEventLoss(t *testing.T) {
	det := NewWithDefaultSensors(WithSynchronousProcessing())
	defer det.Stop()

	// Init frame: empty session primes the sensors, emits nothing.
	det.ProcessFrame(&telemetry.LobbySessionStateFrame{Session: &enginev1.SessionResponse{}})
	total := drainChan(det)

	// One frame with a large simultaneous join (a single big batch).
	det.ProcessFrame(frameWithPlayers(scrambledSlots...))
	total += drainChan(det)

	// More distinct-score frames than the events channel buffer, draining after
	// every frame exactly like convert.go does.
	for i := int32(1); i <= batchOverflowCount; i++ {
		det.ProcessFrame(scoreFrame(i))
		total += drainChan(det)
	}

	if got := det.DroppedEvents(); got != 0 {
		t.Fatalf("conversion drain pattern dropped %d event batch(es); the events channel overflowed", got)
	}
	if total == 0 {
		t.Fatal("expected the pattern to produce events; test did not exercise event flow")
	}
}

// TestSyncDetector_DropsWhenNeverDrained shows the drop branch
// (events.go:219-223) is live, not dead code: feeding more event-producing
// frames than the channel can buffer WITHOUT draining overflows it and
// increments the dropped-events counter. This is NOT the conversion pattern; it
// proves the conversion path's safety depends specifically on draining every
// frame (convert.go:232).
func TestSyncDetector_DropsWhenNeverDrained(t *testing.T) {
	det := NewWithDefaultSensors(WithSynchronousProcessing())
	defer det.Stop()

	// Prime sensors; this frame enqueues nothing.
	det.ProcessFrame(&telemetry.LobbySessionStateFrame{Session: &enginev1.SessionResponse{}})

	// Feed event-producing frames without ever draining the channel.
	for i := int32(1); i <= batchOverflowCount; i++ {
		det.ProcessFrame(scoreFrame(i))
	}

	if got := det.DroppedEvents(); got == 0 {
		t.Fatal("expected dropped event batches when the channel is never drained")
	}
}
