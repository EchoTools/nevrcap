package events

import (
	"testing"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
)

// TestSynchronousMode_BlockingBug validates that synchronous mode doesn't block
// when the events channel is full and no consumer is reading.
// This test should fail before the fix and pass after.
func TestSynchronousMode_BlockingBug(t *testing.T) {
	// Create detector with synchronous mode and NO buffer for events channel
	detector := New(
		WithSynchronousProcessing(),
		WithEventsChannelSize(0), // No buffer - immediate blocking if sending to channel
	)
	defer detector.Stop()

	// Process multiple frames that will generate events
	// Without a consumer reading from eventsChan, this should block if the bug exists
	done := make(chan struct{})
	timeout := time.After(100 * time.Millisecond)

	go func() {
		// Process frames that generate events in synchronous mode
		// First frame will send to channel, second frame will block waiting for consumer
		for i := 0; i < 2; i++ {
			detector.ProcessFrame(&telemetry.LobbySessionStateFrame{
				FrameIndex: uint32(i),
				Session: &apigame.SessionResponse{
					GameStatus: GameStatusPostMatch, // Generates match ended event
				},
			})
		}
		close(done)
	}()

	select {
	case <-done:
		// Success: ProcessFrame calls completed without blocking
	case <-timeout:
		t.Fatal("ProcessFrame blocked in synchronous mode due to full events channel - this demonstrates the bug")
	}
}

// TestSynchronousMode_ImmediateProcessing validates that synchronous mode
// processes frames immediately in the caller's goroutine without delay.
func TestSynchronousMode_ImmediateProcessing(t *testing.T) {
	detector := New(WithSynchronousProcessing())
	defer detector.Stop()

	// Start a consumer
	eventReceived := make(chan struct{})
	go func() {
		<-detector.EventsChan()
		close(eventReceived)
	}()

	// Process a frame that generates an event
	detector.ProcessFrame(&telemetry.LobbySessionStateFrame{
		FrameIndex: 1,
		Session: &apigame.SessionResponse{
			GameStatus: GameStatusPostMatch,
		},
	})

	// Event should be available immediately (within a short timeout)
	select {
	case <-eventReceived:
		// Success
	case <-time.After(10 * time.Millisecond):
		t.Fatal("Event not received immediately in synchronous mode")
	}
}

// TestSynchronousMode_NoBackgroundGoroutine validates that in synchronous mode,
// the processLoop goroutine is not used (inputChan remains empty).
func TestSynchronousMode_NoBackgroundGoroutine(t *testing.T) {
	detector := New(WithSynchronousProcessing())
	defer detector.Stop()

	// Process a frame
	detector.ProcessFrame(&telemetry.LobbySessionStateFrame{
		FrameIndex: 1,
		Session: &apigame.SessionResponse{
			GameStatus: "playing",
		},
	})

	// In synchronous mode, inputChan should not be used
	select {
	case <-detector.inputChan:
		t.Fatal("inputChan should not receive frames in synchronous mode")
	case <-time.After(50 * time.Millisecond):
		// Success: inputChan was not used
	}
}

// TestAsyncMode_UsesBackgroundGoroutine validates that async mode still works
// as expected with background processing.
func TestAsyncMode_UsesBackgroundGoroutine(t *testing.T) {
	detector := New() // Default is async mode
	defer detector.Stop()

	eventReceived := make(chan struct{})
	go func() {
		<-detector.EventsChan()
		close(eventReceived)
	}()

	// Process a frame that generates an event
	detector.ProcessFrame(&telemetry.LobbySessionStateFrame{
		FrameIndex: 1,
		Session: &apigame.SessionResponse{
			GameStatus: GameStatusPostMatch,
		},
	})

	// Event should be received through the background goroutine
	select {
	case <-eventReceived:
		// Success
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Event not received in async mode")
	}
}

// TestSynchronousMode_MultipleEventsWithConsumer validates that multiple events
// can be processed without blocking in synchronous mode when a consumer is actively reading.
func TestSynchronousMode_MultipleEventsWithConsumer(t *testing.T) {
	detector := New(
		WithSynchronousProcessing(),
		WithEventsChannelSize(10), // Adequate buffer to hold events
	)
	defer detector.Stop()

	// Start consumer before processing frames
	eventsReceived := make(chan []*telemetry.LobbySessionEvent, 10)
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for events := range detector.EventsChan() {
			eventsReceived <- events
		}
	}()

	// Process multiple frames with different statuses to trigger transitions
	// Each transition should generate an event
	statuses := []string{"playing", GameStatusRoundOver, GameStatusPostMatch}
	for i, status := range statuses {
		detector.ProcessFrame(&telemetry.LobbySessionStateFrame{
			FrameIndex: uint32(i),
			Session: &apigame.SessionResponse{
				GameStatus: status,
			},
		})
	}

	// Collect all events
	// We expect 2 events: one for round_over transition, one for post_match transition
	timeout := time.After(200 * time.Millisecond)
	receivedBatches := 0
collectLoop:
	for {
		select {
		case <-eventsReceived:
			receivedBatches++
			if receivedBatches == 2 {
				break collectLoop
			}
		case <-timeout:
			t.Fatalf("Expected 2 event batches, got %d", receivedBatches)
		}
	}
}

// TestSynchronousMode_DropsEventsWhenChannelFull validates that in synchronous mode,
// events are dropped (not blocking) when the channel is full and no consumer is reading.
// This is the expected and desired behavior to maintain synchronous processing guarantees.
func TestSynchronousMode_DropsEventsWhenChannelFull(t *testing.T) {
	detector := New(
		WithSynchronousProcessing(),
		WithEventsChannelSize(1), // Small buffer
	)
	defer detector.Stop()

	// Process multiple frames rapidly without a consumer
	// This should NOT block, even though events will be dropped
	done := make(chan struct{})
	go func() {
		for i := 0; i < 5; i++ {
			detector.ProcessFrame(&telemetry.LobbySessionStateFrame{
				FrameIndex: uint32(i),
				Session: &apigame.SessionResponse{
					GameStatus: GameStatusPostMatch,
				},
			})
		}
		close(done)
	}()

	// Verify ProcessFrame doesn't block
	select {
	case <-done:
		// Success: all frames were processed without blocking
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ProcessFrame blocked despite non-blocking send - fix didn't work")
	}

	// Verify we received some events (likely just 1 since buffer is 1 and no consumer)
	receivedCount := 0
	timeout := time.After(50 * time.Millisecond)
drainLoop:
	for {
		select {
		case <-detector.EventsChan():
			receivedCount++
		case <-timeout:
			break drainLoop
		}
	}

	// We should have received at least 1 event (the buffer held one)
	// but not all 5 (some were dropped due to non-blocking send)
	if receivedCount == 0 {
		t.Fatal("Expected at least 1 event to be buffered")
	}
	if receivedCount == 5 {
		t.Fatal("Expected some events to be dropped, but received all 5")
	}
	t.Logf("Received %d out of 5 events (expected behavior: some dropped)", receivedCount)
}
