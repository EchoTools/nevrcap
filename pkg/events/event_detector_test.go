package events

import (
	"sync"
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
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

	// Send a playing frame first so the sensor initializes, then trigger round_over
	detector.ProcessFrame(createPostMatchTestFrame("playing", 1, 0))
	assertNoEvents(t, detector, 50*time.Millisecond)

	detector.ProcessFrame(createPostMatchTestFrame(GameStatusRoundOver, 1, 0))
	if events := mustReceiveEvents(t, detector, 100*time.Millisecond); len(events) != 1 {
		t.Fatalf("expected 1 round over event, got %d", len(events))
	}

	detector.Reset()

	// After reset sensors re-initialize; send playing then round_over again
	detector.ProcessFrame(createPostMatchTestFrame("playing", 2, 2))
	assertNoEvents(t, detector, 50*time.Millisecond)

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
		if sensor.Len() > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("sensor did not observe any frames: %d", sensor.Len())
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
	for i := range totalFrames {
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

func newTestAsyncDetector(tb testing.TB) *AsyncDetector {
	tb.Helper()
	detector := New(WithSensors(NewRoundEndSensor(), NewMatchEndSensor()))
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
	mu     sync.Mutex
	frames []*telemetry.LobbySessionStateFrame
}

func (r *recordingSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	r.mu.Lock()
	r.frames = append(r.frames, frame)
	r.mu.Unlock()
	return nil
}

func (r *recordingSensor) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.frames)
}

func newStatusOnlyFrame(status string) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{GameStatus: status},
	}
}
