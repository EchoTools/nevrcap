package codecs

import (
	"os"
	"testing"
	"time"

	apigame "github.com/echotools/nevr-common/v4/gen/go/apigame/v1"
	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestRoundTripPreservesTimestamps verifies that timestamps are preserved exactly
// during echoreplay -> nevrcap -> echoreplay conversion.
// BUG: Currently fails due to +6 hour offset being applied during conversion.
func TestRoundTripPreservesTimestamps(t *testing.T) {
	// Create a frame with a specific, known timestamp
	// Use UTC to avoid timezone issues
	originalTime := time.Date(2026, 1, 20, 4, 50, 55, 24*1000000, time.UTC)

	frame := &telemetry.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(originalTime),
		Session: &apigame.SessionResponse{
			SessionId:    "07450BBB-06BF-4E7E-9C04-EBCD4AF043D4",
			GameStatus:   "running",
			BluePoints:   0,
			OrangePoints: 0,
		},
		PlayerBones: &apigame.PlayerBonesResponse{
			UserBones: []*apigame.UserBones{},
			ErrCode:   0,
		},
	}

	// Create temporary files for the round-trip
	tmpEchoReplay1 := t.TempDir() + "/test1.echoreplay"
	tmpNevrcap := t.TempDir() + "/test.nevrcap"
	tmpEchoReplay2 := t.TempDir() + "/test2.echoreplay"

	// Step 1: Write to .echoreplay
	writer1, err := NewEchoReplayWriter(tmpEchoReplay1)
	if err != nil {
		t.Fatalf("Failed to create first echoreplay writer: %v", err)
	}
	if err := writer1.WriteFrame(frame); err != nil {
		t.Fatalf("Failed to write frame to echoreplay: %v", err)
	}
	if err := writer1.Close(); err != nil {
		t.Fatalf("Failed to close first echoreplay writer: %v", err)
	}

	// Step 2: Convert .echoreplay -> .nevrcap
	reader1, err := NewEchoReplayReader(tmpEchoReplay1)
	if err != nil {
		t.Fatalf("Failed to create echoreplay reader: %v", err)
	}
	writerNevrcap, err := NewNevrCapWriter(tmpNevrcap)
	if err != nil {
		t.Fatalf("Failed to create nevrcap writer: %v", err)
	}
	for {
		f, err := reader1.ReadFrame()
		if err != nil {
			break
		}
		if err := writerNevrcap.WriteFrame(f); err != nil {
			t.Fatalf("Failed to write frame to nevrcap: %v", err)
		}
	}
	reader1.Close()
	writerNevrcap.Close()

	// Step 3: Convert .nevrcap -> .echoreplay
	readerNevrcap, err := NewNevrCapReader(tmpNevrcap)
	if err != nil {
		t.Fatalf("Failed to create nevrcap reader: %v", err)
	}
	writer2, err := NewEchoReplayWriter(tmpEchoReplay2)
	if err != nil {
		t.Fatalf("Failed to create second echoreplay writer: %v", err)
	}
	for {
		f, err := readerNevrcap.ReadFrame()
		if err != nil {
			break
		}
		if err := writer2.WriteFrame(f); err != nil {
			t.Fatalf("Failed to write frame to second echoreplay: %v", err)
		}
	}
	readerNevrcap.Close()
	writer2.Close()

	// Step 4: Read back the round-tripped frame
	reader2, err := NewEchoReplayReader(tmpEchoReplay2)
	if err != nil {
		t.Fatalf("Failed to create second echoreplay reader: %v", err)
	}
	defer reader2.Close()

	roundTrippedFrame, err := reader2.ReadFrame()
	if err != nil {
		t.Fatalf("Failed to read round-tripped frame: %v", err)
	}

	// Verify timestamps match exactly
	originalTimestamp := frame.Timestamp.AsTime()
	roundTrippedTimestamp := roundTrippedFrame.Timestamp.AsTime()

	// Compare timestamps - they should be identical
	if !originalTimestamp.Equal(roundTrippedTimestamp) {
		t.Errorf("Timestamp mismatch:\n  Original:     %v\n  Round-tripped: %v\n  Difference:   %v",
			originalTimestamp.Format(EchoReplayTimeFormat),
			roundTrippedTimestamp.Format(EchoReplayTimeFormat),
			roundTrippedTimestamp.Sub(originalTimestamp))
	}

	// Also verify the timestamp components individually
	if originalTimestamp.Year() != roundTrippedTimestamp.Year() ||
		originalTimestamp.Month() != roundTrippedTimestamp.Month() ||
		originalTimestamp.Day() != roundTrippedTimestamp.Day() ||
		originalTimestamp.Hour() != roundTrippedTimestamp.Hour() ||
		originalTimestamp.Minute() != roundTrippedTimestamp.Minute() ||
		originalTimestamp.Second() != roundTrippedTimestamp.Second() {
		t.Errorf("Timestamp components mismatch:\n  Original:     %04d/%02d/%02d %02d:%02d:%02d\n  Round-tripped: %04d/%02d/%02d %02d:%02d:%02d",
			originalTimestamp.Year(), originalTimestamp.Month(), originalTimestamp.Day(),
			originalTimestamp.Hour(), originalTimestamp.Minute(), originalTimestamp.Second(),
			roundTrippedTimestamp.Year(), roundTrippedTimestamp.Month(), roundTrippedTimestamp.Day(),
			roundTrippedTimestamp.Hour(), roundTrippedTimestamp.Minute(), roundTrippedTimestamp.Second())
	}
}

// TestRoundTripPreservesSessionID verifies that session IDs are preserved exactly
// during echoreplay -> nevrcap -> echoreplay conversion.
// BUG: Currently fails due to session ID being modified from
//
//	"07450BBB-06BF-4E7E-9C04-EBCD4AF043D4" to
//	"07450BBB-06BF-40000000E-9C04-EBCD4AF043D4"
func TestRoundTripPreservesSessionID(t *testing.T) {
	originalSessionID := "07450BBB-06BF-4E7E-9C04-EBCD4AF043D4"

	frame := &telemetry.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.Now(),
		Session: &apigame.SessionResponse{
			SessionId:    originalSessionID,
			GameStatus:   "running",
			BluePoints:   0,
			OrangePoints: 0,
		},
	}

	// Create temporary files for the round-trip
	tmpEchoReplay1 := t.TempDir() + "/test1.echoreplay"
	tmpNevrcap := t.TempDir() + "/test.nevrcap"
	tmpEchoReplay2 := t.TempDir() + "/test2.echoreplay"

	// Step 1: Write to .echoreplay
	writer1, err := NewEchoReplayWriter(tmpEchoReplay1)
	if err != nil {
		t.Fatalf("Failed to create first echoreplay writer: %v", err)
	}
	if err := writer1.WriteFrame(frame); err != nil {
		t.Fatalf("Failed to write frame to echoreplay: %v", err)
	}
	if err := writer1.Close(); err != nil {
		t.Fatalf("Failed to close first echoreplay writer: %v", err)
	}

	// Step 2: Convert .echoreplay -> .nevrcap
	reader1, err := NewEchoReplayReader(tmpEchoReplay1)
	if err != nil {
		t.Fatalf("Failed to create echoreplay reader: %v", err)
	}
	writerNevrcap, err := NewNevrCapWriter(tmpNevrcap)
	if err != nil {
		t.Fatalf("Failed to create nevrcap writer: %v", err)
	}
	for {
		f, err := reader1.ReadFrame()
		if err != nil {
			break
		}
		if err := writerNevrcap.WriteFrame(f); err != nil {
			t.Fatalf("Failed to write frame to nevrcap: %v", err)
		}
	}
	reader1.Close()
	writerNevrcap.Close()

	// Step 3: Convert .nevrcap -> .echoreplay
	readerNevrcap, err := NewNevrCapReader(tmpNevrcap)
	if err != nil {
		t.Fatalf("Failed to create nevrcap reader: %v", err)
	}
	writer2, err := NewEchoReplayWriter(tmpEchoReplay2)
	if err != nil {
		t.Fatalf("Failed to create second echoreplay writer: %v", err)
	}
	for {
		f, err := readerNevrcap.ReadFrame()
		if err != nil {
			break
		}
		if err := writer2.WriteFrame(f); err != nil {
			t.Fatalf("Failed to write frame to second echoreplay: %v", err)
		}
	}
	readerNevrcap.Close()
	writer2.Close()

	// Step 4: Read back the round-tripped frame
	reader2, err := NewEchoReplayReader(tmpEchoReplay2)
	if err != nil {
		t.Fatalf("Failed to create second echoreplay reader: %v", err)
	}
	defer reader2.Close()

	roundTrippedFrame, err := reader2.ReadFrame()
	if err != nil {
		t.Fatalf("Failed to read round-tripped frame: %v", err)
	}

	// Verify session IDs match exactly
	if roundTrippedFrame.Session.SessionId != originalSessionID {
		t.Errorf("Session ID mismatch:\n  Original:     %s\n  Round-tripped: %s",
			originalSessionID,
			roundTrippedFrame.Session.SessionId)
	}
}

// TestRoundTripPreservesFrameCount verifies that all frames are preserved
// during echoreplay -> nevrcap -> echoreplay conversion.
// BUG: Currently fails due to 1 frame being lost (12,817 -> 12,816).
func TestRoundTripPreservesFrameCount(t *testing.T) {
	// Create multiple frames with distinct data
	frameCount := 100
	frames := make([]*telemetry.LobbySessionStateFrame, frameCount)

	baseTime := time.Date(2026, 1, 20, 4, 50, 55, 0, time.UTC)

	for i := 0; i < frameCount; i++ {
		frames[i] = &telemetry.LobbySessionStateFrame{
			FrameIndex: uint32(i),
			Timestamp:  timestamppb.New(baseTime.Add(time.Duration(i) * time.Second)),
			Session: &apigame.SessionResponse{
				SessionId:    "test-session",
				GameStatus:   "running",
				BluePoints:   int32(i),
				OrangePoints: int32(i * 10),
			},
		}
	}

	// Create temporary files for the round-trip
	tmpEchoReplay1 := t.TempDir() + "/test1.echoreplay"
	tmpNevrcap := t.TempDir() + "/test.nevrcap"
	tmpEchoReplay2 := t.TempDir() + "/test2.echoreplay"

	// Step 1: Write all frames to .echoreplay
	writer1, err := NewEchoReplayWriter(tmpEchoReplay1)
	if err != nil {
		t.Fatalf("Failed to create first echoreplay writer: %v", err)
	}
	for _, frame := range frames {
		if err := writer1.WriteFrame(frame); err != nil {
			t.Fatalf("Failed to write frame to echoreplay: %v", err)
		}
	}
	if err := writer1.Close(); err != nil {
		t.Fatalf("Failed to close first echoreplay writer: %v", err)
	}

	// Step 2: Convert .echoreplay -> .nevrcap
	reader1, err := NewEchoReplayReader(tmpEchoReplay1)
	if err != nil {
		t.Fatalf("Failed to create echoreplay reader: %v", err)
	}
	writerNevrcap, err := NewNevrCapWriter(tmpNevrcap)
	if err != nil {
		t.Fatalf("Failed to create nevrcap writer: %v", err)
	}
	intermediateCount := 0
	for {
		f, err := reader1.ReadFrame()
		if err != nil {
			break
		}
		intermediateCount++
		if err := writerNevrcap.WriteFrame(f); err != nil {
			t.Fatalf("Failed to write frame to nevrcap: %v", err)
		}
	}
	reader1.Close()
	writerNevrcap.Close()

	if intermediateCount != frameCount {
		t.Errorf("Frame count mismatch after echoreplay->nevrcap:\n  Original: %d\n  Read:     %d",
			frameCount, intermediateCount)
	}

	// Step 3: Convert .nevrcap -> .echoreplay
	readerNevrcap, err := NewNevrCapReader(tmpNevrcap)
	if err != nil {
		t.Fatalf("Failed to create nevrcap reader: %v", err)
	}
	writer2, err := NewEchoReplayWriter(tmpEchoReplay2)
	if err != nil {
		t.Fatalf("Failed to create second echoreplay writer: %v", err)
	}
	roundTripCount := 0
	for {
		f, err := readerNevrcap.ReadFrame()
		if err != nil {
			break
		}
		roundTripCount++
		if err := writer2.WriteFrame(f); err != nil {
			t.Fatalf("Failed to write frame to second echoreplay: %v", err)
		}
	}
	readerNevrcap.Close()
	writer2.Close()

	if roundTripCount != frameCount {
		t.Errorf("Frame count mismatch after nevrcap->echoreplay:\n  Original: %d\n  Read:     %d",
			frameCount, roundTripCount)
	}

	// Step 4: Verify all frames by reading back
	reader2, err := NewEchoReplayReader(tmpEchoReplay2)
	if err != nil {
		t.Fatalf("Failed to create second echoreplay reader: %v", err)
	}
	defer reader2.Close()

	roundTrippedFrames, err := reader2.ReadFrames()
	if err != nil {
		t.Fatalf("Failed to read round-tripped frames: %v", err)
	}

	// Verify the exact count
	if len(roundTrippedFrames) != frameCount {
		t.Errorf("Frame count mismatch:\n  Original:     %d\n  Round-tripped: %d\n  Lost:         %d",
			frameCount, len(roundTrippedFrames), frameCount-len(roundTrippedFrames))
	}

	for i, frame := range roundTrippedFrames {
		expectedBluePoints := int32(i)
		expectedOrangePoints := int32(i * 10)

		if frame.Session.BluePoints != expectedBluePoints {
			t.Errorf("Frame %d: BluePoints mismatch: expected %v, got %v",
				i, expectedBluePoints, frame.Session.BluePoints)
		}

		if frame.Session.OrangePoints != expectedOrangePoints {
			t.Errorf("Frame %d: OrangePoints mismatch: expected %v, got %v",
				i, expectedOrangePoints, frame.Session.OrangePoints)
		}
	}
}

// TestRoundTripWithRealFileData tests round-trip conversion on the actual problematic file
// This test requires the test file to exist and will be skipped if not found.
func TestRoundTripWithRealFileData(t *testing.T) {
	originalFile := "../../rec_2026-01-19_22-50-54.echoreplay"

	if _, err := os.Stat(originalFile); os.IsNotExist(err) {
		originalFile = "../../../rec_2026-01-19_22-50-54.echoreplay"
		if _, err := os.Stat(originalFile); os.IsNotExist(err) {
			t.Skip("Test file rec_2026-01-19_22-50-54.echoreplay not found, skipping test")
		}
	}

	tmpNevrcap := t.TempDir() + "/rec_converted.nevrcap"
	tmpRoundTrip := t.TempDir() + "/rec_roundtrip.echoreplay"

	// Step 1: Read original file and count frames
	reader1, err := NewEchoReplayReader(originalFile)
	if err != nil {
		t.Fatalf("Failed to open original file: %v", err)
	}

	originalFrames, err := reader1.ReadFrames()
	if err != nil {
		t.Fatalf("Failed to read original frames: %v", err)
	}
	reader1.Close()

	originalCount := len(originalFrames)
	t.Logf("Original file has %d frames", originalCount)

	// Step 2: Convert to nevrcap
	reader2, err := NewEchoReplayReader(originalFile)
	if err != nil {
		t.Fatalf("Failed to open original file for conversion: %v", err)
	}
	writerNevrcap, err := NewNevrCapWriter(tmpNevrcap)
	if err != nil {
		t.Fatalf("Failed to create nevrcap writer: %v", err)
	}
	for {
		f, err := reader2.ReadFrame()
		if err != nil {
			break
		}
		if err := writerNevrcap.WriteFrame(f); err != nil {
			t.Fatalf("Failed to write frame to nevrcap: %v", err)
		}
	}
	reader2.Close()
	writerNevrcap.Close()

	// Step 3: Convert back to echoreplay
	readerNevrcap, err := NewNevrCapReader(tmpNevrcap)
	if err != nil {
		t.Fatalf("Failed to create nevrcap reader: %v", err)
	}
	writer2, err := NewEchoReplayWriter(tmpRoundTrip)
	if err != nil {
		t.Fatalf("Failed to create echoreplay writer: %v", err)
	}
	for {
		f, err := readerNevrcap.ReadFrame()
		if err != nil {
			break
		}
		if err := writer2.WriteFrame(f); err != nil {
			t.Fatalf("Failed to write frame to echoreplay: %v", err)
		}
	}
	readerNevrcap.Close()
	writer2.Close()

	// Step 4: Read round-tripped file and compare
	reader3, err := NewEchoReplayReader(tmpRoundTrip)
	if err != nil {
		t.Fatalf("Failed to open round-trip file: %v", err)
	}

	roundTrippedFrames, err := reader3.ReadFrames()
	if err != nil {
		t.Fatalf("Failed to read round-trip frames: %v", err)
	}
	reader3.Close()

	roundTripCount := len(roundTrippedFrames)
	t.Logf("Round-trip file has %d frames", roundTripCount)

	// Verify frame count
	if roundTripCount != originalCount {
		t.Errorf("Frame count mismatch:\n  Original:     %d\n  Round-tripped: %d\n  Lost:         %d",
			originalCount, roundTripCount, originalCount-roundTripCount)
	}

	// Sample check: verify first frame timestamp and session ID
	if len(originalFrames) > 0 && len(roundTrippedFrames) > 0 {
		origFirst := originalFrames[0]
		rtFirst := roundTrippedFrames[0]

		// Check timestamp
		if !origFirst.Timestamp.AsTime().Equal(rtFirst.Timestamp.AsTime()) {
			t.Errorf("First frame timestamp mismatch:\n  Original:     %v\n  Round-tripped: %v",
				origFirst.Timestamp.AsTime().Format(EchoReplayTimeFormat),
				rtFirst.Timestamp.AsTime().Format(EchoReplayTimeFormat))
		}

		// Check session ID
		if origFirst.Session.SessionId != rtFirst.Session.SessionId {
			t.Errorf("First frame session ID mismatch:\n  Original:     %s\n  Round-tripped: %s",
				origFirst.Session.SessionId,
				rtFirst.Session.SessionId)
		}
	}
}
