package events

import (
	"sync"
	"testing"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
)

func TestAsyncDetector_ConcurrentReset(t *testing.T) {
	detector := New()
	// We don't defer Stop() here because we need to control when it happens relative to the consumer

	var wgProducers sync.WaitGroup
	var wgConsumer sync.WaitGroup
	done := make(chan struct{})

	// Goroutine 1: Pump frames
	wgProducers.Add(1)
	go func() {
		defer wgProducers.Done()
		i := 0
		for {
			select {
			case <-done:
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
				// Small sleep to yield
				if i%100 == 0 {
					time.Sleep(time.Microsecond)
				}
			}
		}
	}()

	// Goroutine 2: Reset periodically
	wgProducers.Add(1)
	go func() {
		defer wgProducers.Done()
		for i := 0; i < 10; i++ {
			time.Sleep(10 * time.Millisecond)
			detector.Reset()
		}
		close(done)
	}()

	// Goroutine 3: Consume events
	wgConsumer.Add(1)
	go func() {
		defer wgConsumer.Done()
		for range detector.EventsChan() {
			// Consume until closed
		}
	}()

	// Wait for producers to finish
	wgProducers.Wait()

	// Stop detector (closes events channel)
	detector.Stop()

	// Wait for consumer to finish
	wgConsumer.Wait()
}

type mockSensor struct {
	id string
}

func (m *mockSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil {
		return nil
	}
	// Return an event every time
	// We use RoundEnded as a placeholder since we don't have Custom event type easily accessible
	return &telemetry.LobbySessionEvent{
		Event: &telemetry.LobbySessionEvent_RoundEnded{
			RoundEnded: &rtapi.RoundEnded{},
		},
	}
}

func TestAsyncDetector_MultipleSensors(t *testing.T) {
	// Add two sensors
	s1 := &mockSensor{id: "s1"}
	s2 := &mockSensor{id: "s2"}

	detector := New(WithSensors(s1, s2))
	defer detector.Stop()

	// Process a frame
	frame := &telemetry.LobbySessionStateFrame{
		FrameIndex: 1,
		Session: &apigame.SessionResponse{
			GameStatus: "playing",
		},
	}
	detector.ProcessFrame(frame)

	// We expect events from both sensors + potentially internal events (none here)
	select {
	case events := <-detector.EventsChan():
		if len(events) != 2 {
			t.Errorf("Expected 2 events, got %d", len(events))
		}
		// Since we can't easily distinguish the events without custom data fields,
		// we just verify the count for now.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for events")
	}
}
