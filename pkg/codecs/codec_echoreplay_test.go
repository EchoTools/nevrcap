package codecs

import (
	"archive/zip"
	"os"
	"testing"
)

// TestEchoReplayCodec tests the EchoReplay codec
func TestEchoReplayCodec(t *testing.T) {
	tempFile := "/tmp/test.echoreplay"
	defer os.Remove(tempFile)

	// Test writing
	writer, err := NewEchoReplayWriter(tempFile)
	if err != nil {
		t.Fatalf("Failed to create EchoReplay writer: %v", err)
	}

	// Write frames
	frame := createTestFrame(t)
	if err := writer.WriteFrame(frame); err != nil {
		t.Fatalf("Failed to write frame: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Failed to close writer: %v", err)
	}

	// Test reading
	reader, err := NewEchoReplayReader(tempFile)
	if err != nil {
		t.Fatalf("Failed to create EchoReplay reader: %v", err)
	}
	defer reader.Close()

	frames, err := reader.ReadFrames()
	if err != nil {
		t.Fatalf("Failed to read frames: %v", err)
	}

	if len(frames) != 1 {
		t.Errorf("Expected 1 frame, got %d", len(frames))
	}

	if frames[0].FrameIndex != frame.FrameIndex {
		t.Errorf("Expected frame index %d, got %d", frame.FrameIndex, frames[0].FrameIndex)
	}
}

func TestEchoReplayReader_Resilience(t *testing.T) {
	tmpFile := t.TempDir() + "/bad_content.echoreplay"

	// Create a zip file manually
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatal(err)
	}

	zw := zip.NewWriter(f)
	// Create the inner file with the same name as the zip (minus extension)
	// The reader expects this or .echoreplay
	w, err := zw.Create("bad_content")
	if err != nil {
		t.Fatal(err)
	}

	// Write mixed content
	// 1. Good line
	w.Write([]byte("2023/01/01 12:00:00.000\t{\"session_id\":\"1\"}\t{\"user_bones\":[]}\n"))
	// 2. Bad timestamp
	w.Write([]byte("BAD_TIMESTAMP\t{\"session_id\":\"2\"}\t{\"user_bones\":[]}\n"))
	// 3. Bad JSON
	w.Write([]byte("2023/01/01 12:00:01.000\t{bad_json}\t{\"user_bones\":[]}\n"))
	// 4. Missing columns
	w.Write([]byte("2023/01/01 12:00:02.000\t{\"session_id\":\"4\"}\n"))
	// 5. Good line
	w.Write([]byte("2023/01/01 12:00:03.000\t{\"session_id\":\"5\"}\t{\"user_bones\":[]}\n"))

	zw.Close()
	f.Close()

	// Read it back
	reader, err := NewEchoReplayReader(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer reader.Close()

	frames, err := reader.ReadFrames()
	if err != nil {
		t.Fatalf("ReadFrames failed: %v", err)
	}

	// We expect 2 good frames (1 and 5)
	// The reader implementation currently skips invalid lines (see ReadFrame implementation)
	if len(frames) != 2 {
		t.Errorf("Expected 2 valid frames, got %d", len(frames))
	}

	if len(frames) > 0 && frames[0].Session.SessionId != "1" {
		t.Errorf("Expected first frame session 1, got %s", frames[0].Session.SessionId)
	}
	if len(frames) > 1 && frames[1].Session.SessionId != "5" {
		t.Errorf("Expected second frame session 5, got %s", frames[1].Session.SessionId)
	}
}
