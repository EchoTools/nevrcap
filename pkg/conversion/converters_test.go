package conversion

import (
	"os"
	"testing"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"github.com/echotools/nevrcap/pkg/codecs"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestFileConversion tests the file format conversion utilities
func TestFileConversion(t *testing.T) {
	echoReplayFile := "/tmp/test_convert.echoreplay"
	nevrcapFile := "/tmp/test_convert.nevrcap"
	backToEchoFile := "/tmp/test_back.echoreplay"

	defer func() {
		os.Remove(echoReplayFile)
		os.Remove(nevrcapFile)
		os.Remove(backToEchoFile)
	}()

	// Create a test .echoreplay file
	writer, err := codecs.NewEchoReplayWriter(echoReplayFile)
	if err != nil {
		t.Fatalf("Failed to create EchoReplay writer: %v", err)
	}

	frame := createTestFrame(t)
	if err := writer.WriteFrame(frame); err != nil {
		t.Fatalf("Failed to write frame: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Failed to close writer: %v", err)
	}

	// Convert .echoreplay to .nevrcap
	if err := ConvertEchoReplayToNevrcap(echoReplayFile, nevrcapFile); err != nil {
		t.Fatalf("Failed to convert echoreplay to nevrcap: %v", err)
	}

	// Convert .nevrcap back to .echoreplay
	if err := ConvertNevrcapToEchoReplay(nevrcapFile, backToEchoFile); err != nil {
		t.Fatalf("Failed to convert nevrcap back to echoreplay: %v", err)
	}

	// Verify the round-trip conversion
	reader, err := codecs.NewEchoReplayReader(backToEchoFile)
	if err != nil {
		t.Fatalf("Failed to create reader for converted file: %v", err)
	}
	defer reader.Close()

	frames, err := reader.ReadFrames()
	if err != nil {
		t.Fatalf("Failed to read converted frames: %v", err)
	}

	if len(frames) != 1 {
		t.Errorf("Expected 1 frame after conversion, got %d", len(frames))
	}
}

// Helper functions for creating test data

func createTestFrame(t *testing.T) *rtapi.LobbySessionStateFrame {
	sessionResponse := &apigame.SessionResponse{
		SessionId:        "test-session",
		GameStatus:       "running",
		BluePoints:       0,
		OrangePoints:     0,
		BlueRoundScore:   0,
		OrangeRoundScore: 0,
		Teams:            []*apigame.Team{},
	}

	bonesResponse := &apigame.PlayerBonesResponse{
		UserBones: []*apigame.UserBones{},
		ErrCode:   0,
	}

	return &rtapi.LobbySessionStateFrame{
		FrameIndex:  0,
		Timestamp:   timestamppb.Now(),
		Events:      []*rtapi.LobbySessionEvent{},
		Session:     sessionResponse,
		PlayerBones: bonesResponse,
	}
}

func TestConversionGeneratesEvents(t *testing.T) {
	echoReplayFile := t.TempDir() + "/events.echoreplay"
	nevrcapFile := t.TempDir() + "/events.nevrcap"

	// Create echoreplay with transition
	writer, err := codecs.NewEchoReplayWriter(echoReplayFile)
	if err != nil {
		t.Fatal(err)
	}

	// Frame 1: Playing
	f1 := createTestFrame(t)
	f1.Session.GameStatus = "playing"
	f1.Timestamp = timestamppb.New(time.Now())
	writer.WriteFrame(f1)

	// Frame 2: Round Over (should trigger event)
	f2 := createTestFrame(t)
	f2.Session.GameStatus = "round_over"
	f2.Timestamp = timestamppb.New(time.Now().Add(100 * time.Millisecond))
	writer.WriteFrame(f2)

	writer.Close()

	// Convert
	if err := ConvertEchoReplayToNevrcap(echoReplayFile, nevrcapFile); err != nil {
		t.Fatalf("Conversion failed: %v", err)
	}

	// Read nevrcap and check for events
	reader, err := codecs.NewNevrCapReader(nevrcapFile)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	// Read header
	if _, err := reader.ReadHeader(); err != nil {
		t.Fatal(err)
	}

	// Frame 1
	rf1, err := reader.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if len(rf1.Events) != 0 {
		t.Errorf("Expected 0 events in frame 1, got %d", len(rf1.Events))
	}

	// Frame 2
	rf2, err := reader.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	// We expect 1 event (RoundEnded)
	if len(rf2.Events) != 1 {
		t.Errorf("Expected 1 event in frame 2, got %d", len(rf2.Events))
	}
}
