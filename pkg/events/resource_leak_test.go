package events

import (
	"sync"
	"testing"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
)

// TestInputChannelDraining validates that inputChan is properly drained when Stop() is called
// This test addresses the potential resource leak where frames could remain buffered indefinitely
func TestInputChannelDraining(t *testing.T) {
	// Create detector with small channel to make it easier to fill
	detector := New(WithInputChannelSize(5))

	// Fill the input channel with frames
	for i := 0; i < 5; i++ {
		frame := &telemetry.LobbySessionStateFrame{
			FrameIndex: uint32(i),
			Session: &apigame.SessionResponse{
				GameStatus: "playing",
			},
		}
		detector.ProcessFrame(frame)
	}

	// Give processLoop a moment to start processing
	time.Sleep(10 * time.Millisecond)

	// Add more frames that might still be in the channel
	for i := 5; i < 10; i++ {
		frame := &telemetry.LobbySessionStateFrame{
			FrameIndex: uint32(i),
			Session: &apigame.SessionResponse{
				GameStatus: "playing",
			},
		}
		detector.ProcessFrame(frame)
	}

	// Stop the detector - this should drain any remaining frames in inputChan
	detector.Stop()

	// Verify that Stop() returned, meaning wg.Wait() completed
	// If inputChan wasn't drained, processLoop might hang indefinitely
	// The test passing means the goroutine exited properly
}

// TestEventsChanRaceCondition validates that there's no race condition when closing eventsChan
// while processLoop is trying to send to it
func TestEventsChanRaceCondition(t *testing.T) {
	// Use synchronous mode to have more control over timing
	detector := New(WithEventsChannelSize(1))

	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Goroutine to continuously send frames that will generate events
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stopChan:
				return
			default:
				frame := &telemetry.LobbySessionStateFrame{
					FrameIndex: uint32(i),
					Session: &apigame.SessionResponse{
						GameStatus: "playing",
					},
				}
				detector.ProcessFrame(frame)
				i++
				if i%10 == 0 {
					time.Sleep(time.Microsecond)
				}
			}
		}
	}()

	// Let frames accumulate
	time.Sleep(50 * time.Millisecond)

	// Stop the detector while frames are being processed
	// This previously could cause a panic if processLoop tried to send to
	// eventsChan after Stop() closed it
	detector.Stop()

	close(stopChan)
	wg.Wait()

	// If we get here without panic, the race condition is handled
}

// TestStopWhileProcessingFrames validates proper shutdown under heavy load
func TestStopWhileProcessingFrames(t *testing.T) {
	detector := New(
		WithInputChannelSize(100),
		WithEventsChannelSize(10),
	)

	var wg sync.WaitGroup
	stopProducer := make(chan struct{})

	// Start multiple producers
	for p := 0; p < 3; p++ {
		wg.Add(1)
		go func(producerID int) {
			defer wg.Done()
			i := 0
			for {
				select {
				case <-stopProducer:
					return
				default:
					frame := &telemetry.LobbySessionStateFrame{
						FrameIndex: uint32(i*1000 + producerID),
						Session: &apigame.SessionResponse{
							GameStatus: "playing",
						},
					}
					detector.ProcessFrame(frame)
					i++
					if i%50 == 0 {
						time.Sleep(time.Microsecond)
					}
				}
			}
		}(p)
	}

	// Start a consumer that's intentionally slow
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range detector.EventsChan() {
			// Slow consumer - this makes events back up
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Let the system run under load
	time.Sleep(100 * time.Millisecond)

	// Stop producers
	close(stopProducer)

	// Stop detector while there might be:
	// 1. Frames still in inputChan
	// 2. Events waiting to be sent to eventsChan
	// 3. The consumer is slow
	detector.Stop()

	// Wait for all goroutines to finish
	wg.Wait()

	// If we reach here without hanging or panicking, the fix works
}

// TestMultipleStopCalls ensures Stop() is idempotent and doesn't panic
func TestMultipleStopCalls(t *testing.T) {
	detector := New()

	// Process some frames
	for i := 0; i < 5; i++ {
		frame := &telemetry.LobbySessionStateFrame{
			FrameIndex: uint32(i),
			Session: &apigame.SessionResponse{
				GameStatus: "playing",
			},
		}
		detector.ProcessFrame(frame)
	}

	// Call Stop() multiple times
	detector.Stop()

	// These should not panic
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Second Stop() call panicked: %v", r)
			}
		}()
		detector.Stop()
	}()

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Third Stop() call panicked: %v", r)
			}
		}()
		detector.Stop()
	}()
}
