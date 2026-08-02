package events

import (
	"sync"
	"testing"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
)

// stopRaceFrame builds a frame whose blue_points forces a ScoreboardUpdated
// event, guaranteeing processFrameSync reaches the channel send.
func stopRaceFrame(bluePoints int32) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			GameStatus: GameStatusPlaying,
			BluePoints: bluePoints,
		},
	}
}

// TestSyncDetector_StopDuringProcessFrameDoesNotPanic proves F-14: sync-mode
// Stop() calls cancel() then immediately close(ed.eventsChan) without waiting
// for an in-flight processFrameSync to return (events.go:142-149). If
// ProcessFrame is running in a concurrent goroutine and reaches the event-send
// select (events.go:213-223) after the channel is closed, the send panics.
//
// The test runs many iterations of concurrent ProcessFrame and Stop. Under
// `-race` the unsynchronized close and send on the same channel is detected
// even if the panic does not fire in this particular run.
//
// Currently RED under -race: Stop() has no synchronization with in-flight
// processFrameSync calls.
func TestSyncDetector_StopDuringProcessFrameDoesNotPanic(t *testing.T) {
	// Run enough iterations that the race detector has a chance to observe the
	// conflicting channel operations (close in Stop, send in ProcessFrame).
	for range 200 {
		det := NewWithDefaultSensors(WithSynchronousProcessing())

		// Prime the sensors so ProcessFrame produces events.
		det.ProcessFrame(stopRaceFrame(0))

		var wg sync.WaitGroup
		wg.Add(1)

		// ProcessFrame in a goroutine — this is the realistic misuse: a library
		// caller processing frames from one goroutine while tearing down from
		// another.
		go func() {
			defer wg.Done()
			// Recover from a possible panic so the test reports it cleanly.
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic during ProcessFrame: %v", r)
				}
			}()
			det.ProcessFrame(stopRaceFrame(1))
		}()

		// Stop from the calling goroutine — no synchronization with ProcessFrame.
		det.Stop()
		wg.Wait()
	}
}
