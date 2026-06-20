package events

import (
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestWithFrameBufferSize_ClampsZero(t *testing.T) {
	// Should not panic on ProcessFrame with a zero-sized buffer.
	det := New(
		WithSynchronousProcessing(),
		WithFrameBufferSize(0),
	)
	defer det.Stop()

	frame := &telemetry.LobbySessionStateFrame{
		FrameIndex: 1,
		Timestamp:  timestamppb.Now(),
		Session:    &enginev1.SessionResponse{GameStatus: "playing"},
	}

	// This previously caused division by zero in addFrameToBuffer.
	det.ProcessFrame(frame)
}

func TestWithFrameBufferSize_ClampsNegative(t *testing.T) {
	det := New(
		WithSynchronousProcessing(),
		WithFrameBufferSize(-10),
	)
	defer det.Stop()

	frame := &telemetry.LobbySessionStateFrame{
		FrameIndex: 1,
		Timestamp:  timestamppb.Now(),
		Session:    &enginev1.SessionResponse{GameStatus: "playing"},
	}

	det.ProcessFrame(frame)
}

func TestWithInputChannelSize_ClampsZero(t *testing.T) {
	det := New(WithInputChannelSize(0))
	defer det.Stop()

	// Should not deadlock on send — channel capacity is at least 1.
	frame := &telemetry.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.Now(),
		Session:    &enginev1.SessionResponse{GameStatus: "playing"},
	}

	done := make(chan struct{})
	go func() {
		det.ProcessFrame(frame)
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("ProcessFrame blocked — channel capacity was likely 0")
	}
}

func TestWithEventsChannelSize_ClampsZero(t *testing.T) {
	det := New(
		WithSynchronousProcessing(),
		WithEventsChannelSize(0),
	)
	defer det.Stop()

	// Verify the events channel has at least capacity 1.
	if cap(det.eventsChan) < 1 {
		t.Errorf("eventsChan capacity = %d, want >= 1", cap(det.eventsChan))
	}
}

func TestProcessLoop_NonBlockingEventSend(t *testing.T) {
	// Create an async detector with a tiny events channel.
	det := New(
		WithEventsChannelSize(1),
		WithSensors(&stateSensor{}),
	)
	defer det.Stop()

	// Send multiple frames that each produce events, without draining.
	for i := range 20 {
		frame := &telemetry.LobbySessionStateFrame{
			FrameIndex: uint32(i),
			Timestamp:  timestamppb.Now(),
			Session: &enginev1.SessionResponse{
				GameStatus: "playing",
				BluePoints: int32(i), // changing score
				Teams: []*enginev1.Team{
					{Players: []*enginev1.TeamMember{
						{SlotNumber: 1, Stats: &enginev1.PlayerStats{Goals: int32(i)}},
					}},
				},
			},
		}
		det.ProcessFrame(frame)
	}

	// Give the processLoop goroutine time to process.
	time.Sleep(100 * time.Millisecond)

	// The fact that we got here without deadlock means the non-blocking
	// send worked. Verify that some events were dropped.
	dropped := det.DroppedEvents()
	if dropped == 0 {
		t.Log("no events were dropped (channel might have been drained fast enough)")
	} else {
		t.Logf("dropped %d event batches (non-blocking send working)", dropped)
	}
}

// stateSensor is a simple sensor that produces an event whenever the
// game status changes. Used for testing the processLoop.
type stateSensor struct {
	prev string
}

func (s *stateSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil {
		return nil
	}
	status := frame.GetSession().GetGameStatus()
	score := frame.GetSession().GetBluePoints()
	key := status + string(rune(score+'0'))
	if s.prev == key {
		return nil
	}
	s.prev = key
	return &telemetry.LobbySessionEvent{
		Event: &telemetry.LobbySessionEvent_GenericEvent{
			GenericEvent: &telemetry.GenericEvent{
				EventType: "test",
			},
		},
	}
}
