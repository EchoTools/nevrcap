package nevrcap

import (
	"testing"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func BenchmarkEventDetector_AddFrame(b *testing.B) {
	detector := NewEventDetector()
	frame := createPostMatchTestFrame("playing", 1, 0)

	b.ReportAllocs()

	for b.Loop() {
		detector.AddFrame(frame)
	}
}

func BenchmarkEventDetector_AddFrame_WithTransition(b *testing.B) {
	frames := []*rtapi.LobbySessionStateFrame{
		createPostMatchTestFrame("playing", 2, 1),
		createPostMatchTestFrame("post_match", 3, 1),
	}

	b.ReportAllocs()

	for b.Loop() {
		detector := NewEventDetector()
		for _, frame := range frames {
			detector.AddFrame(frame)
		}
	}
}

func BenchmarkEventDetector_AddFrame_FullBuffer(b *testing.B) {
	detector := NewEventDetector()

	// Fill the buffer to capacity
	for i := 0; i < detector.frameCount; i++ {
		frame := createPostMatchTestFrame("playing", int32(i%3), int32(i%2))
		detector.AddFrame(frame)
	}

	frame := createPostMatchTestFrame("playing", 1, 0)

	b.ReportAllocs()

	for b.Loop() {
		detector.AddFrame(frame)
	}
}

func BenchmarkEventDetector_AddFrame_Sequence(b *testing.B) {
	// Pre-create all frames before timing
	frames := make([]*rtapi.LobbySessionStateFrame, 100)
	for j := 0; j < 100; j++ {
		var status string
		switch {
		case j < 10:
			status = "pre_match"
		case j < 80:
			status = "playing"
		case j < 85:
			status = "score"
		case j < 90:
			status = "round_over"
		default:
			status = "post_match"
		}

		frames[j] = &rtapi.LobbySessionStateFrame{
			FrameIndex: uint32(j),
			Timestamp:  timestamppb.New(time.Now()),
			Session: &apigame.SessionResponse{
				GameStatus:   status,
				BluePoints:   int32(j / 20),
				OrangePoints: int32(j / 30),
			},
		}
	}

	b.ReportAllocs()

	for b.Loop() {
		detector := NewEventDetector()

		// Simulate a realistic sequence of frames
		for _, frame := range frames {
			detector.AddFrame(frame)
		}
	}
}
