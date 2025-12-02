package events

import (
	"testing"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func BenchmarkAsyncDetector_ProcessFrame(b *testing.B) {
	detector := New()
	defer detector.Stop()

	frame := createPostMatchTestFrame("playing", 1, 0)

	// Drain events channel in background
	go func() {
		for range detector.EventsChan() {
		}
	}()

	b.ReportAllocs()

	for b.Loop() {
		detector.ProcessFrame(frame)
	}
}

func BenchmarkAsyncDetector_ProcessFrame_WithTransition(b *testing.B) {
	frames := []*rtapi.LobbySessionStateFrame{
		createPostMatchTestFrame("playing", 2, 1),
		createPostMatchTestFrame("post_match", 3, 1),
	}

	b.ReportAllocs()

	for b.Loop() {
		detector := New()

		// Drain events channel in background
		go func() {
			for range detector.EventsChan() {
			}
		}()

		for _, frame := range frames {
			detector.ProcessFrame(frame)
		}

		detector.Stop()
	}
}

func BenchmarkAsyncDetector_ProcessFrame_FullBuffer(b *testing.B) {
	detector := New()
	defer detector.Stop()

	// Drain events channel in background
	go func() {
		for range detector.EventsChan() {
		}
	}()

	// Fill the buffer to capacity
	for i := 0; i < DefaultFrameBufferCapacity; i++ {
		frame := createPostMatchTestFrame("playing", int32(i%3), int32(i%2))
		detector.ProcessFrame(frame)
	}

	frame := createPostMatchTestFrame("playing", 1, 0)

	b.ReportAllocs()

	for b.Loop() {
		detector.ProcessFrame(frame)
	}
}

func BenchmarkAsyncDetector_ProcessFrame_Sequence(b *testing.B) {
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
		detector := New()

		// Drain events channel in background
		go func() {
			for range detector.EventsChan() {
			}
		}()

		// Simulate a realistic sequence of frames
		for _, frame := range frames {
			detector.ProcessFrame(frame)
		}

		detector.Stop()
	}
}
