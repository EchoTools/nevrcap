package events

import (
	"testing"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
)

func TestAsyncDetector_ProcessFrameRoundOverTransition(t *testing.T) {
	detector := newTestAsyncDetector(t)

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

func TestAsyncDetector_ProcessFramePostMatchTransition(t *testing.T) {
	detector := newTestAsyncDetector(t)

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

func TestAsyncDetector_ProcessFrameInitialPostMatch(t *testing.T) {
	detector := newTestAsyncDetector(t)

	detector.ProcessFrame(createPostMatchTestFrame(GameStatusPostMatch, 5, 4))
	events := mustReceiveEvents(t, detector, 100*time.Millisecond)

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].GetMatchEnded() == nil {
		t.Fatalf("expected match ended event, got %#v", events[0].Event)
	}
}

func TestAsyncDetector_ProcessFrameNoTransitionNoEvent(t *testing.T) {
	detector := newTestAsyncDetector(t)

	detector.ProcessFrame(createPostMatchTestFrame("playing", 1, 0))
	assertNoEvents(t, detector, 50*time.Millisecond)

	detector.ProcessFrame(createPostMatchTestFrame("playing", 2, 1))
	assertNoEvents(t, detector, 50*time.Millisecond)
}

func TestAsyncDetector_ProcessFrameNilSession(t *testing.T) {
	detector := newTestAsyncDetector(t)

	detector.ProcessFrame(&telemetry.LobbySessionStateFrame{})
	assertNoEvents(t, detector, 50*time.Millisecond)
}

func TestAsyncDetector_ProcessFrameNilFrame(t *testing.T) {
	detector := newTestAsyncDetector(t)

	detector.ProcessFrame(nil)
	assertNoEvents(t, detector, 50*time.Millisecond)
}

func TestAsyncDetector_ResetClearsState(t *testing.T) {
	detector := newTestAsyncDetector(t)

	detector.ProcessFrame(createPostMatchTestFrame(GameStatusRoundOver, 1, 0))
	if events := mustReceiveEvents(t, detector, 100*time.Millisecond); len(events) != 1 {
		t.Fatalf("expected 1 round over event, got %d", len(events))
	}

	detector.Reset()

	detector.ProcessFrame(createPostMatchTestFrame(GameStatusRoundOver, 2, 2))
	if events := mustReceiveEvents(t, detector, 100*time.Millisecond); len(events) != 1 {
		t.Fatalf("expected 1 round over event after reset, got %d", len(events))
	}
}

func TestAsyncDetector_StopClosesEventsChan(t *testing.T) {
	detector := New()
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

func TestAsyncDetector_SensorIntegrationReceivesFrames(t *testing.T) {
	detector := newTestAsyncDetector(t)
	sensor := &recordingSensor{}
	detector.sensors = []Sensor{sensor}

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

func TestAsyncDetector_AddFrameToBufferWraps(t *testing.T) {
	detector := &AsyncDetector{
		frameBuffer: make([]*telemetry.LobbySessionStateFrame, DefaultFrameBufferCapacity),
	}

	totalFrames := DefaultFrameBufferCapacity + 3
	frames := make([]*telemetry.LobbySessionStateFrame, totalFrames)
	for i := 0; i < totalFrames; i++ {
		frame := &telemetry.LobbySessionStateFrame{FrameIndex: uint32(i)}
		frames[i] = frame
		detector.addFrameToBuffer(frame)
	}

	if detector.frameCount != DefaultFrameBufferCapacity {
		t.Fatalf("frameCount expected %d got %d", DefaultFrameBufferCapacity, detector.frameCount)
	}

	if got := detector.lastFrame(); got != frames[len(frames)-1] {
		t.Fatalf("lastFrame mismatch: got index %d", got.GetFrameIndex())
	}
}

func TestAsyncDetector_detectPostMatchEventIgnoresInvalidIndex(t *testing.T) {
	ed := &AsyncDetector{frameBuffer: make([]*telemetry.LobbySessionStateFrame, 1)}
	if events := ed.detectPostMatchEvent(-1, nil); events != nil {
		t.Fatalf("expected nil events for negative index, got %v", events)
	}
	if events := ed.detectPostMatchEvent(len(ed.frameBuffer), nil); events != nil {
		t.Fatalf("expected nil events for out-of-range index, got %v", events)
	}
}

func TestAsyncDetector_detectPostMatchEventSkipsNilFrame(t *testing.T) {
	ed := &AsyncDetector{frameBuffer: make([]*telemetry.LobbySessionStateFrame, 1)}
	ed.frameBuffer[0] = nil
	if events := ed.detectPostMatchEvent(0, nil); events != nil {
		t.Fatalf("expected nil events for nil frame, got %v", events)
	}
}

func TestAsyncDetector_detectPostMatchEventSkipsNilSession(t *testing.T) {
	ed := &AsyncDetector{frameBuffer: make([]*telemetry.LobbySessionStateFrame, 1)}
	ed.frameBuffer[0] = &telemetry.LobbySessionStateFrame{}
	if events := ed.detectPostMatchEvent(0, nil); events != nil {
		t.Fatalf("expected nil events for nil session, got %v", events)
	}
}

func TestAsyncDetector_detectPostMatchEventSkipsRepeatedStatus(t *testing.T) {
	ed := &AsyncDetector{frameBuffer: make([]*telemetry.LobbySessionStateFrame, 1)}
	prev := newStatusOnlyFrame("playing")
	ed.previousGameStatusFrame = prev
	ed.frameBuffer[0] = newStatusOnlyFrame("playing")
	if events := ed.detectPostMatchEvent(0, nil); events != nil {
		t.Fatalf("expected nil events for repeated status, got %v", events)
	}
	if ed.previousGameStatusFrame != prev {
		t.Fatalf("previous frame should remain unchanged on repeated status")
	}
}

func TestAsyncDetector_detectPostMatchEventUpdatesPreviousOnTransition(t *testing.T) {
	ed := &AsyncDetector{frameBuffer: make([]*telemetry.LobbySessionStateFrame, 1)}
	prev := newStatusOnlyFrame("playing")
	current := newStatusOnlyFrame(GameStatusRoundOver)
	ed.previousGameStatusFrame = prev
	ed.frameBuffer[0] = current
	if events := ed.detectPostMatchEvent(0, nil); events == nil {
		t.Fatalf("expected events for transition")
	}
	if ed.previousGameStatusFrame != current {
		t.Fatalf("previous frame should update to current on transition")
	}
}

func TestAsyncDetector_detectPostMatchEventEmitsRoundEnded(t *testing.T) {
	ed := &AsyncDetector{frameBuffer: make([]*telemetry.LobbySessionStateFrame, 1)}
	ed.previousGameStatusFrame = newStatusOnlyFrame("playing")
	ed.frameBuffer[0] = newStatusOnlyFrame(GameStatusRoundOver)
	events := ed.detectPostMatchEvent(0, nil)
	if len(events) != 1 {
		t.Fatalf("expected 1 event got %d", len(events))
	}
	if events[0].GetRoundEnded() == nil {
		t.Fatalf("expected round ended event, got %#v", events[0])
	}
}

func TestAsyncDetector_detectPostMatchEventEmitsMatchEnded(t *testing.T) {
	ed := &AsyncDetector{frameBuffer: make([]*telemetry.LobbySessionStateFrame, 1)}
	ed.previousGameStatusFrame = newStatusOnlyFrame(GameStatusRoundOver)
	ed.frameBuffer[0] = newStatusOnlyFrame(GameStatusPostMatch)
	events := ed.detectPostMatchEvent(0, nil)
	if len(events) != 1 {
		t.Fatalf("expected 1 event got %d", len(events))
	}
	if events[0].GetMatchEnded() == nil {
		t.Fatalf("expected match ended event, got %#v", events[0])
	}
}

func TestAsyncDetector_detectPostMatchEventInitialMatchEnded(t *testing.T) {
	ed := &AsyncDetector{frameBuffer: make([]*telemetry.LobbySessionStateFrame, 1)}
	ed.frameBuffer[0] = newStatusOnlyFrame(GameStatusPostMatch)
	events := ed.detectPostMatchEvent(0, nil)
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

func newTestAsyncDetector(tb testing.TB) *AsyncDetector {
	tb.Helper()
	detector := New()
	tb.Cleanup(detector.Stop)
	return detector
}

func mustReceiveEvents(tb testing.TB, detector *AsyncDetector, timeout time.Duration) []*telemetry.LobbySessionEvent {
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

func assertNoEvents(tb testing.TB, detector *AsyncDetector, timeout time.Duration) {
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
	frames []*telemetry.LobbySessionStateFrame
}

func (r *recordingSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	r.frames = append(r.frames, frame)
	return nil
}

func newStatusOnlyFrame(status string) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{GameStatus: status},
	}
}
