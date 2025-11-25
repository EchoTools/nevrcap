package nevrcap

import (
	"testing"

	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
)

func BenchmarkEventDetector_detectPostMatchEventRoundOver(b *testing.B) {
	detector := &EventDetector{}
	detector.frameBuffer[0] = newStatusOnlyFrame(GameStatusRoundOver)
	prev := newStatusOnlyFrame("playing")

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		detector.previousGameStatusFrame = prev
		if events := detector.detectPostMatchEvent(0); len(events) == 0 {
			b.Fatalf("expected round over event, iteration %d", i)
		}
	}
}

func BenchmarkEventDetector_detectPostMatchEventMatchEnded(b *testing.B) {
	detector := &EventDetector{}
	detector.frameBuffer[0] = newStatusOnlyFrame(GameStatusPostMatch)
	prev := newStatusOnlyFrame(GameStatusRoundOver)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		detector.previousGameStatusFrame = prev
		if events := detector.detectPostMatchEvent(0); len(events) == 0 {
			b.Fatalf("expected match ended event, iteration %d", i)
		}
	}
}

func BenchmarkEventDetector_addFrameToBuffer(b *testing.B) {
	detector := &EventDetector{}
	frames := make([]*rtapi.LobbySessionStateFrame, MaxFrameBufferCapacity)
	for i := range frames {
		frames[i] = &rtapi.LobbySessionStateFrame{FrameIndex: uint32(i)}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		detector.addFrameToBuffer(frames[i%len(frames)])
	}
}

func BenchmarkEventDetector_detectEventsWithSensors(b *testing.B) {
	detector := &EventDetector{
		sensors: []EventSensor{benchEventSensor{}, benchEventSensor{}},
	}
	roundOver := newStatusOnlyFrame(GameStatusRoundOver)
	postMatch := newStatusOnlyFrame(GameStatusPostMatch)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		detector.previousGameStatusFrame = roundOver
		detector.addFrameToBuffer(postMatch)
		if events := detector.detectEvents(); len(events) == 0 {
			b.Fatalf("expected events from sensors or detectors at iteration %d", i)
		}
	}
}

func BenchmarkEventDetector_detectEventsNoTransition(b *testing.B) {
	detector := &EventDetector{}
	playing := newStatusOnlyFrame("playing")

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		detector.previousGameStatusFrame = playing
		detector.addFrameToBuffer(playing)
		if events := detector.detectEvents(); len(events) != 0 {
			b.Fatalf("expected no events on iteration %d", i)
		}
	}
}

type benchEventSensor struct{}

func (benchEventSensor) AddFrame(frame *rtapi.LobbySessionStateFrame) *rtapi.LobbySessionEvent {
	if frame == nil {
		return nil
	}
	return &rtapi.LobbySessionEvent{}
}
