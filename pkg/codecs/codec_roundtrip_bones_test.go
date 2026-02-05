package codecs

import (
	"os"
	"testing"
)

// TestRoundTripPreservesBoneData verifies that player bones data is preserved
// during echoreplay -> nevrcap -> echoreplay conversion.
func TestRoundTripPreservesBoneData(t *testing.T) {
	originalFile := "../../test_with_bones.echoreplay"

	if _, err := os.Stat(originalFile); os.IsNotExist(err) {
		t.Skip("Test file test_with_bones.echoreplay not found, skipping test")
	}

	tmpNevrcap := t.TempDir() + "/test_bones.nevrcap"
	tmpRoundTrip := t.TempDir() + "/test_bones_roundtrip.echoreplay"

	// Step 1: Read original and check for bones
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
	originalBonesCount := 0
	for _, frame := range originalFrames {
		if frame.PlayerBones != nil && len(frame.PlayerBones.UserBones) > 0 {
			originalBonesCount++
		}
	}

	t.Logf("Original file: %d frames, %d with bone data", originalCount, originalBonesCount)

	if originalBonesCount == 0 {
		t.Skip("Original file has no bone data, skipping test")
	}

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

	// Step 4: Read round-tripped file and compare bone data
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
	roundTripBonesCount := 0
	for _, frame := range roundTrippedFrames {
		if frame.PlayerBones != nil && len(frame.PlayerBones.UserBones) > 0 {
			roundTripBonesCount++
		}
	}

	t.Logf("Round-trip file: %d frames, %d with bone data", roundTripCount, roundTripBonesCount)

	// Verify frame count
	if roundTripCount != originalCount {
		t.Errorf("Frame count mismatch:\n  Original: %d\n  Round-trip: %d",
			originalCount, roundTripCount)
	}

	// Verify bone data count
	if roundTripBonesCount != originalBonesCount {
		t.Errorf("Bone data frame count mismatch:\n  Original: %d\n  Round-trip: %d\n  Lost: %d",
			originalBonesCount, roundTripBonesCount, originalBonesCount-roundTripBonesCount)
	}

	// Verify first frame with bones has matching data
	for i := 0; i < len(originalFrames) && i < len(roundTrippedFrames); i++ {
		origBones := originalFrames[i].PlayerBones
		rtBones := roundTrippedFrames[i].PlayerBones

		if origBones != nil && len(origBones.UserBones) > 0 {
			if rtBones == nil || len(rtBones.UserBones) == 0 {
				t.Errorf("Frame %d: Original has bone data, round-trip does not", i)
				break
			}

			// Check bone count matches
			if len(origBones.UserBones) != len(rtBones.UserBones) {
				t.Errorf("Frame %d: Bone count mismatch: original=%d, round-trip=%d",
					i, len(origBones.UserBones), len(rtBones.UserBones))
			}

			// Only check first frame in detail
			break
		}
	}
}
