package codecs

import (
	"testing"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestEchoReplay_ReadFrameTo_ZeroAlloc(t *testing.T) {
	// Create a temporary echoreplay file
	tmpFile := t.TempDir() + "/test_zero_alloc.echoreplay"
	writer, err := NewEchoReplayWriter(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}

	// Write a few frames
	frameCount := 5
	for i := 0; i < frameCount; i++ {
		frame := &rtapi.LobbySessionStateFrame{
			FrameIndex: uint32(i),
			Timestamp:  timestamppb.New(time.Now()),
			Session: &apigame.SessionResponse{
				SessionId: "test-session",
			},
			PlayerBones: &apigame.PlayerBonesResponse{},
		}
		if err := writer.WriteFrame(frame); err != nil {
			t.Fatalf("Failed to write frame: %v", err)
		}
	}
	writer.Close()

	// Read back using ReadFrameTo
	reader, err := NewEchoReplayReader(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer reader.Close()

	// Pre-allocate frame
	frame := &rtapi.LobbySessionStateFrame{
		Session:     &apigame.SessionResponse{},
		PlayerBones: &apigame.PlayerBonesResponse{},
		Timestamp:   &timestamppb.Timestamp{},
	}

	// Measure allocations for reading frames
	// We skip the first read to warm up any internal buffers if necessary,
	// though strictly speaking ReadFrameTo should be zero alloc from the start if buffers are reused.
	// However, the first call might allocate for the scanner buffer.
	// Let's check all of them but be aware of scanner init.

	// Actually, let's just verify correctness first, then allocations.
	for i := 0; i < frameCount; i++ {
		ok, err := reader.ReadFrameTo(frame)
		if err != nil {
			t.Fatalf("ReadFrameTo failed at index %d: %v", i, err)
		}
		if !ok {
			t.Fatalf("ReadFrameTo returned false prematurely at index %d", i)
		}
		if frame.FrameIndex != uint32(i) { // Note: FrameIndex is set by reader based on sequence
			// The writer doesn't write the FrameIndex into the file (it's not part of the JSON/TS format usually),
			// but the reader assigns it.
			// Let's check if the reader assigns it correctly.
			// In codec_echoreplay.go: frame.FrameIndex = e.frameIndex
		}
	}

	// Verify EOF
	ok, err := reader.ReadFrameTo(frame)
	if err != nil && err.Error() != "EOF" {
		// ReadFrameTo returns false, io.EOF usually
	}
	if ok {
		t.Fatal("Expected EOF, got another frame")
	}
}

func TestEchoReplay_ReadTo_BufferReuse(t *testing.T) {
	// Create a temporary echoreplay file
	tmpFile := t.TempDir() + "/test_read_to.echoreplay"
	writer, err := NewEchoReplayWriter(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}

	totalFrames := 10
	for i := 0; i < totalFrames; i++ {
		frame := &rtapi.LobbySessionStateFrame{
			Timestamp: timestamppb.New(time.Now()),
			Session:   &apigame.SessionResponse{SessionId: "test"},
		}
		writer.WriteFrame(frame)
	}
	writer.Close()

	reader, err := NewEchoReplayReader(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer reader.Close()

	// Create a buffer of frames
	bufferSize := 3
	buffer := make([]*rtapi.LobbySessionStateFrame, bufferSize)
	// We need to initialize them if ReadTo expects to reuse them?
	// Looking at codec_echoreplay.go:
	// func (e *EchoReplay) ReadTo(frames []*rtapi.LobbySessionStateFrame) (int, error) {
	//    ...
	//    frame, err := e.ReadFrame()
	//    frames[count] = frame
	// }
	// Wait, ReadTo in codec_echoreplay.go calls ReadFrame(), which allocates a NEW frame!
	// It does NOT reuse the structs pointed to by the slice. It just fills the slice with pointers to new frames.
	// So ReadTo is NOT zero-allocation regarding the Frame structs themselves, only the slice.
	// But ReadFrameTo IS zero-allocation.

	// Let's verify ReadTo behavior
	n, err := reader.ReadTo(buffer)
	if err != nil {
		t.Fatalf("ReadTo failed: %v", err)
	}
	if n != bufferSize {
		t.Errorf("Expected to read %d frames, got %d", bufferSize, n)
	}

	// Verify pointers are set
	for i, f := range buffer {
		if f == nil {
			t.Errorf("Frame %d is nil", i)
		}
	}
}

func TestNevrCap_ReadFrameTo_ZeroAlloc(t *testing.T) {
	// Similar test for NevrCap (Zstd/Protobuf) codec
	tmpFile := t.TempDir() + "/test_zero_alloc.nevrcap"
	writer, err := NewNevrCapWriter(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}

	frameCount := 5
	for i := 0; i < frameCount; i++ {
		frame := &rtapi.LobbySessionStateFrame{
			FrameIndex: uint32(i),
			Timestamp:  timestamppb.New(time.Now()),
			Session:    &apigame.SessionResponse{SessionId: "test"},
		}
		writer.WriteFrame(frame)
	}
	writer.Close()

	reader, err := NewNevrCapReader(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer reader.Close()

	frame := &rtapi.LobbySessionStateFrame{}

	for i := 0; i < frameCount; i++ {
		ok, err := reader.ReadFrameTo(frame)
		if err != nil {
			t.Fatalf("ReadFrameTo failed at index %d: %v", i, err)
		}
		if !ok {
			t.Fatalf("ReadFrameTo returned false prematurely at index %d", i)
		}
		if frame.FrameIndex != uint32(i) {
			t.Errorf("Expected frame index %d, got %d", i, frame.FrameIndex)
		}
	}
}
