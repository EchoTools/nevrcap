package nevrcap

import (
	"testing"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
)

func TestEventDetector_ProcessFrameRoundOverTransition(t *testing.T) {
	detector := newTestEventDetector(t)

	detector.ProcessFrame(createPostMatchTestFrame("playing", 1, 0))
	assertNoEvents(t, detector, 50*time.Millisecond)

	detector.ProcessFrame(createPostMatchTestFrame(GameStatusRoundOver, 2, 1))
	events := mustReceiveEvents(t, detector, 100*time.Millisecond)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].GetRoundEnded() == nil {
		t.Fatalf("expected round ended event, got %#v", events[0].Event)
	}
}

func TestEventDetector_ProcessFramePostMatchTransition(t *testing.T) {
	detector := newTestEventDetector(t)

	detector.ProcessFrame(createPostMatchTestFrame("playing", 1, 0))
	assertNoEvents(t, detector, 50*time.Millisecond)

	detector.ProcessFrame(createPostMatchTestFrame(GameStatusPostMatch, 3, 2))
	events := mustReceiveEvents(t, detector, 100*time.Millisecond)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].GetMatchEnded() == nil {
		t.Fatalf("expected match ended event, got %#v", events[0].Event)
	}
}

func TestEventDetector_ProcessFrameInitialPostMatch(t *testing.T) {
	detector := newTestEventDetector(t)

	detector.ProcessFrame(createPostMatchTestFrame(GameStatusPostMatch, 5, 4))
	events := mustReceiveEvents(t, detector, 100*time.Millisecond)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].GetMatchEnded() == nil {
		t.Fatalf("expected match ended event, got %#v", events[0].Event)
	}
}

func TestEventDetector_ProcessFrameNoTransitionNoEvent(t *testing.T) {
	detector := newTestEventDetector(t)

	detector.ProcessFrame(createPostMatchTestFrame("playing", 1, 0))
	assertNoEvents(t, detector, 50*time.Millisecond)

	detector.ProcessFrame(createPostMatchTestFrame("playing", 2, 1))
	assertNoEvents(t, detector, 50*time.Millisecond)
}

func TestEventDetector_ProcessFrameNilSession(t *testing.T) {
	detector := newTestEventDetector(t)

	detector.ProcessFrame(&rtapi.LobbySessionStateFrame{})
	assertNoEvents(t, detector, 50*time.Millisecond)
}

func TestEventDetector_ProcessFrameNilFrame(t *testing.T) {
	detector := newTestEventDetector(t)

	detector.ProcessFrame(nil)
	assertNoEvents(t, detector, 50*time.Millisecond)
}

func TestEventDetector_ResetDoesNotEmitWithoutNewTransition(t *testing.T) {
	detector := newTestEventDetector(t)

	detector.ProcessFrame(createPostMatchTestFrame(GameStatusRoundOver, 1, 0))
	if events := mustReceiveEvents(t, detector, 100*time.Millisecond); len(events) != 1 {
		t.Fatalf("expected 1 round over event, got %d", len(events))
	}

	detector.Reset()

	detector.ProcessFrame(createPostMatchTestFrame(GameStatusRoundOver, 2, 2))
	assertNoEvents(t, detector, 50*time.Millisecond)
}

func TestEventDetector_StopClosesEventsChan(t *testing.T) {
	detector := NewEventDetector()
	detector.Stop()

	select {
	case _, ok := <-detector.EventsChan():
		if ok {
			t.Fatal("expected events channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for events channel close")
	}
}

func TestEventDetector_SensorIntegrationReceivesFrames(t *testing.T) {
	detector := newTestEventDetector(t)
	sensor := &recordingSensor{}
	detector.sensors = []EventSensor{sensor}

	detector.ProcessFrame(createPostMatchTestFrame("playing", 1, 0))

	deadline := time.After(200 * time.Millisecond)
	for {
		if len(sensor.frames) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("sensor did not observe any frames: %d", len(sensor.frames))
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestEventDetector_AddFrameToBufferWraps(t *testing.T) {
	detector := &EventDetector{}

	totalFrames := MaxFrameBufferCapacity + 3
	frames := make([]*rtapi.LobbySessionStateFrame, totalFrames)
	for i := 0; i < totalFrames; i++ {
		frame := &rtapi.LobbySessionStateFrame{FrameIndex: uint32(i)}
		frames[i] = frame
		detector.addFrameToBuffer(frame)
	}

	if detector.frameCount != MaxFrameBufferCapacity {
		t.Fatalf("frameCount expected %d got %d", MaxFrameBufferCapacity, detector.frameCount)
	}

	if got := detector.lastFrame(); got != frames[len(frames)-1] {
		t.Fatalf("lastFrame mismatch: got index %d", got.GetFrameIndex())
	}
}

func TestEventDetector_detectPostMatchEventIgnoresInvalidIndex(t *testing.T) {
	ed := &EventDetector{}
	if events := ed.detectPostMatchEvent(-1); events != nil {
		t.Fatalf("expected nil events for negative index, got %v", events)
	}
	if events := ed.detectPostMatchEvent(len(ed.frameBuffer)); events != nil {
		t.Fatalf("expected nil events for out-of-range index, got %v", events)
	}
}

func TestEventDetector_detectPostMatchEventSkipsNilFrame(t *testing.T) {
	ed := &EventDetector{}
	ed.frameBuffer[0] = nil
	if events := ed.detectPostMatchEvent(0); events != nil {
		t.Fatalf("expected nil events for nil frame, got %v", events)
	}
}

func TestEventDetector_detectPostMatchEventSkipsNilSession(t *testing.T) {
	ed := &EventDetector{}
	ed.frameBuffer[0] = &rtapi.LobbySessionStateFrame{}
	if events := ed.detectPostMatchEvent(0); events != nil {
		t.Fatalf("expected nil events for nil session, got %v", events)
	}
}

func TestEventDetector_detectPostMatchEventSkipsRepeatedStatus(t *testing.T) {
	ed := &EventDetector{}
	prev := newStatusOnlyFrame("playing")
	ed.previousGameStatusFrame = prev
	ed.frameBuffer[0] = newStatusOnlyFrame("playing")
	if events := ed.detectPostMatchEvent(0); events != nil {
		t.Fatalf("expected nil events for repeated status, got %v", events)
	}
	if ed.previousGameStatusFrame != prev {
		t.Fatalf("previous frame should remain unchanged on repeated status")
	}
}

func TestEventDetector_detectPostMatchEventUpdatesPreviousOnTransition(t *testing.T) {
	ed := &EventDetector{}
	prev := newStatusOnlyFrame("playing")
	current := newStatusOnlyFrame(GameStatusRoundOver)
	ed.previousGameStatusFrame = prev
	ed.frameBuffer[0] = current
	if events := ed.detectPostMatchEvent(0); events == nil {
		t.Fatalf("expected events for transition")
	}
	if ed.previousGameStatusFrame != current {
		t.Fatalf("previous frame should update to current on transition")
	}
}

func TestEventDetector_detectPostMatchEventEmitsRoundEnded(t *testing.T) {
	ed := &EventDetector{}
	ed.previousGameStatusFrame = newStatusOnlyFrame("playing")
	ed.frameBuffer[0] = newStatusOnlyFrame(GameStatusRoundOver)
	events := ed.detectPostMatchEvent(0)
	if len(events) != 1 {
		t.Fatalf("expected 1 event got %d", len(events))
	}
	if events[0].GetRoundEnded() == nil {
		t.Fatalf("expected round ended event, got %#v", events[0])
	}
}

func TestEventDetector_detectPostMatchEventEmitsMatchEnded(t *testing.T) {
	ed := &EventDetector{}
	ed.previousGameStatusFrame = newStatusOnlyFrame(GameStatusRoundOver)
	ed.frameBuffer[0] = newStatusOnlyFrame(GameStatusPostMatch)
	events := ed.detectPostMatchEvent(0)
	if len(events) != 1 {
		t.Fatalf("expected 1 event got %d", len(events))
	}
	if events[0].GetMatchEnded() == nil {
		t.Fatalf("expected match ended event, got %#v", events[0])
	}
}

func TestEventDetector_detectPostMatchEventInitialMatchEnded(t *testing.T) {
	ed := &EventDetector{}
	ed.frameBuffer[0] = newStatusOnlyFrame(GameStatusPostMatch)
	events := ed.detectPostMatchEvent(0)
	if len(events) != 1 {
		t.Fatalf("expected 1 event got %d", len(events))
	}
	if events[0].GetMatchEnded() == nil {
		t.Fatalf("expected match ended event, got %#v", events[0])
	}
	if ed.previousGameStatusFrame != ed.frameBuffer[0] {
		t.Fatalf("previous frame should update when none was set")
	}
}

func newTestEventDetector(tb testing.TB) *EventDetector {
	tb.Helper()
	detector := NewEventDetector()
	tb.Cleanup(detector.Stop)
	return detector
}

func mustReceiveEvents(tb testing.TB, detector *EventDetector, timeout time.Duration) []*rtapi.LobbySessionEvent {
	tb.Helper()
	select {
	case events, ok := <-detector.EventsChan():
		if !ok {
			tb.Fatalf("events channel closed before receiving events")
		}
		return events
	case <-time.After(timeout):
		tb.Fatalf("timeout waiting for events")
		return nil
	}
}

func assertNoEvents(tb testing.TB, detector *EventDetector, timeout time.Duration) {
	tb.Helper()
	select {
	case events, ok := <-detector.EventsChan():
		if !ok {
			tb.Fatalf("events channel closed while waiting for absence of events")
		}
		if len(events) > 0 {
			tb.Fatalf("unexpected events: %v", events)
		}
	case <-time.After(timeout):
	}
}

type recordingSensor struct {
	frames []*rtapi.LobbySessionStateFrame
}

func (r *recordingSensor) AddFrame(frame *rtapi.LobbySessionStateFrame) *rtapi.LobbySessionEvent {
	r.frames = append(r.frames, frame)
	return nil
}

func newStatusOnlyFrame(status string) *rtapi.LobbySessionStateFrame {
	return &rtapi.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{GameStatus: status},
	}
}
