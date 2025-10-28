package nevrcap

import (
	"testing"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func BenchmarkReadFrameTo(b *testing.B) {
	// Create a temporary echoreplay file with test data
	tmpFile := b.TempDir() + "/test.echoreplay"

	// Create writer and populate with sample frames
	writer, err := NewEchoReplayCodecWriter(tmpFile)
	if err != nil {
		b.Fatalf("Failed to create writer: %v", err)
	}

	// Write 1000 sample frames
	sampleFrame := &rtapi.LobbySessionStateFrame{
		Timestamp: timestamppb.New(time.Now()),
		Session: &apigame.SessionResponse{
			SessionId: "test-session-id",
		},
		PlayerBones: &apigame.PlayerBonesResponse{},
	}

	for range 1000 {
		if err := writer.WriteFrame(sampleFrame); err != nil {
			b.Fatalf("Failed to write frame: %v", err)
		}
	}

	if err := writer.Close(); err != nil {
		b.Fatalf("Failed to close writer: %v", err)
	}

	// Create reader
	reader, err := NewEchoReplayFileReader(tmpFile)
	if err != nil {
		b.Fatalf("Failed to create reader: %v", err)
	}
	defer reader.Close()

	// Preallocate frame for reuse
	frame := &rtapi.LobbySessionStateFrame{
		Session:     &apigame.SessionResponse{},
		PlayerBones: &apigame.PlayerBonesResponse{},
		Timestamp:   &timestamppb.Timestamp{},
	}

	b.ReportAllocs()

	for i := 0; b.Loop(); i++ {
		// Reset reader position for each iteration
		if i%1000 == 0 && i > 0 {
			reader.Close()
			reader, err = NewEchoReplayFileReader(tmpFile)
			if err != nil {
				b.Fatalf("Failed to recreate reader: %v", err)
			}
		}

		ok, err := reader.ReadFrameTo(frame)
		if err != nil || !ok {
			// Recreate reader when EOF is reached
			reader.Close()
			reader, err = NewEchoReplayFileReader(tmpFile)
			if err != nil {
				b.Fatalf("Failed to recreate reader: %v", err)
			}
			ok, err = reader.ReadFrameTo(frame)
			if err != nil || !ok {
				b.Fatalf("Failed to read frame: %v", err)
			}
		}
	}
}

func BenchmarkNewEchoReplayFileReader(b *testing.B) {
	// Create a temporary echoreplay file with test data
	tmpFile := b.TempDir() + "/test.echoreplay"

	// Create writer and populate with sample frames
	writer, err := NewEchoReplayCodecWriter(tmpFile)
	if err != nil {
		b.Fatalf("Failed to create writer: %v", err)
	}

	// Write 1000 sample frames
	sampleFrame := &rtapi.LobbySessionStateFrame{
		Timestamp: timestamppb.New(time.Now()),
		Session: &apigame.SessionResponse{
			SessionId: "test-session-id",
		},
		PlayerBones: &apigame.PlayerBonesResponse{},
	}

	for i := 0; i < 1000; i++ {
		if err := writer.WriteFrame(sampleFrame); err != nil {
			b.Fatalf("Failed to write frame: %v", err)
		}
	}

	if err := writer.Close(); err != nil {
		b.Fatalf("Failed to close writer: %v", err)
	}

	b.ReportAllocs()

	for b.Loop() {
		reader, err := NewEchoReplayFileReader(tmpFile)
		if err != nil {
			b.Fatalf("Failed to create reader: %v", err)
		}
		reader.Close()
	}
}
