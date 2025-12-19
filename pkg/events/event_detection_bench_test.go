package events

import (
	"testing"
)

func BenchmarkAsyncDetector_detectPostMatchEventRoundOver(b *testing.B) {
	detector := &AsyncDetector{frameBuffer: make([]*telemetry.LobbySessionStateFrame, 1)}
	detector.frameBuffer[0] = newStatusOnlyFrame(GameStatusRoundOver)
	prev := newStatusOnlyFrame("playing")
	var buf []*telemetry.LobbySessionEvent

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		detector.previousGameStatusFrame = prev
		buf = buf[:0]
		if events := detector.detectPostMatchEvent(0, buf); len(events) == 0 {
			b.Fatalf("expected round over event, iteration %d", i)
		}
	}
}

func BenchmarkAsyncDetector_detectPostMatchEventMatchEnded(b *testing.B) {
	detector := &AsyncDetector{frameBuffer: make([]*telemetry.LobbySessionStateFrame, 1)}
	detector.frameBuffer[0] = newStatusOnlyFrame(GameStatusPostMatch)
	prev := newStatusOnlyFrame(GameStatusRoundOver)
	var buf []*telemetry.LobbySessionEvent

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		detector.previousGameStatusFrame = prev
		buf = buf[:0]
		if events := detector.detectPostMatchEvent(0, buf); len(events) == 0 {
			b.Fatalf("expected match ended event, iteration %d", i)
		}
	}
}

func BenchmarkAsyncDetector_addFrameToBuffer(b *testing.B) {
	detector := &AsyncDetector{frameBuffer: make([]*telemetry.LobbySessionStateFrame, DefaultFrameBufferCapacity)}
	frames := make([]*telemetry.LobbySessionStateFrame, DefaultFrameBufferCapacity)
	for i := range frames {
		frames[i] = &telemetry.LobbySessionStateFrame{FrameIndex: uint32(i)}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		detector.addFrameToBuffer(frames[i%len(frames)])
	}
}

func BenchmarkAsyncDetector_detectEventsWithSensors(b *testing.B) {
	detector := &AsyncDetector{
		sensors:     []Sensor{benchSensor{}, benchSensor{}},
		frameBuffer: make([]*telemetry.LobbySessionStateFrame, DefaultFrameBufferCapacity),
	}
	roundOver := newStatusOnlyFrame(GameStatusRoundOver)
	postMatch := newStatusOnlyFrame(GameStatusPostMatch)
	var buf []*telemetry.LobbySessionEvent

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		detector.previousGameStatusFrame = roundOver
		detector.addFrameToBuffer(postMatch)
		buf = buf[:0]
		if events := detector.detectEvents(buf); len(events) == 0 {
			b.Fatalf("expected events from sensors or detectors at iteration %d", i)
		}
	}
}

func BenchmarkAsyncDetector_detectEventsNoTransition(b *testing.B) {
	detector := &AsyncDetector{frameBuffer: make([]*telemetry.LobbySessionStateFrame, DefaultFrameBufferCapacity)}
	playing := newStatusOnlyFrame("playing")
	var buf []*telemetry.LobbySessionEvent

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		detector.previousGameStatusFrame = playing
		detector.addFrameToBuffer(playing)
		buf = buf[:0]
		if events := detector.detectEvents(buf); len(events) != 0 {
			b.Fatalf("expected no events on iteration %d", i)
		}
	}
}

type benchSensor struct{}

func (benchSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil {
		return nil
	}
	return &telemetry.LobbySessionEvent{}
}
