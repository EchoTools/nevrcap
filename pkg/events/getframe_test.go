package events

import (
	"testing"

	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
)

func TestAsyncDetector_getFrame(t *testing.T) {
	tests := []struct {
		name        string
		bufferSize  int
		framesToAdd int
		offset      int
		wantNil     bool
		wantFrameID uint32 // Expected frame ID (we'll use FrameIndex as identifier)
	}{
		{
			name:        "empty buffer returns nil",
			bufferSize:  5,
			framesToAdd: 0,
			offset:      0,
			wantNil:     true,
		},
		{
			name:        "offset 0 returns most recent frame",
			bufferSize:  5,
			framesToAdd: 3,
			offset:      0,
			wantNil:     false,
			wantFrameID: 2, // Most recent is frame with ID 2 (0-indexed)
		},
		{
			name:        "offset 1 returns previous frame",
			bufferSize:  5,
			framesToAdd: 3,
			offset:      1,
			wantNil:     false,
			wantFrameID: 1,
		},
		{
			name:        "offset 2 returns second previous frame",
			bufferSize:  5,
			framesToAdd: 3,
			offset:      2,
			wantNil:     false,
			wantFrameID: 0,
		},
		{
			name:        "offset beyond frameCount returns nil",
			bufferSize:  5,
			framesToAdd: 3,
			offset:      3,
			wantNil:     true,
		},
		{
			name:        "offset much larger than frameCount returns nil",
			bufferSize:  5,
			framesToAdd: 3,
			offset:      10,
			wantNil:     true,
		},
		{
			name:        "single frame - offset 0",
			bufferSize:  5,
			framesToAdd: 1,
			offset:      0,
			wantNil:     false,
			wantFrameID: 0,
		},
		{
			name:        "single frame - offset 1 returns nil",
			bufferSize:  5,
			framesToAdd: 1,
			offset:      1,
			wantNil:     true,
		},
		{
			name:        "full buffer - offset 0",
			bufferSize:  5,
			framesToAdd: 5,
			offset:      0,
			wantNil:     false,
			wantFrameID: 4,
		},
		{
			name:        "full buffer - offset 4 (oldest)",
			bufferSize:  5,
			framesToAdd: 5,
			offset:      4,
			wantNil:     false,
			wantFrameID: 0,
		},
		{
			name:        "full buffer - offset beyond capacity returns nil",
			bufferSize:  5,
			framesToAdd: 5,
			offset:      5,
			wantNil:     true,
		},
		{
			name:        "wrapped buffer - offset 0",
			bufferSize:  5,
			framesToAdd: 7,
			offset:      0,
			wantNil:     false,
			wantFrameID: 6, // Most recent
		},
		{
			name:        "wrapped buffer - offset 1",
			bufferSize:  5,
			framesToAdd: 7,
			offset:      1,
			wantNil:     false,
			wantFrameID: 5,
		},
		{
			name:        "wrapped buffer - offset 4 (oldest available)",
			bufferSize:  5,
			framesToAdd: 7,
			offset:      4,
			wantNil:     false,
			wantFrameID: 2, // Oldest in buffer (frames 0-1 were overwritten)
		},
		{
			name:        "wrapped buffer - offset beyond frameCount returns nil",
			bufferSize:  5,
			framesToAdd: 7,
			offset:      5,
			wantNil:     true,
		},
		{
			name:        "wrapped multiple times - offset 0",
			bufferSize:  3,
			framesToAdd: 10,
			offset:      0,
			wantNil:     false,
			wantFrameID: 9,
		},
		{
			name:        "wrapped multiple times - offset 2 (oldest)",
			bufferSize:  3,
			framesToAdd: 10,
			offset:      2,
			wantNil:     false,
			wantFrameID: 7, // Frames 0-6 were overwritten
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create detector with specific buffer size
			ed := &AsyncDetector{
				frameBuffer: make([]*rtapi.LobbySessionStateFrame, tt.bufferSize),
				writeIndex:  0,
				frameCount:  0,
			}

			// Add frames with unique IDs (using FrameIndex field as identifier)
			for i := 0; i < tt.framesToAdd; i++ {
				frame := &rtapi.LobbySessionStateFrame{
					FrameIndex: uint32(i), // Use as unique identifier
				}
				ed.addFrameToBuffer(frame)
			}

			// Test getFrame
			got := ed.getFrame(tt.offset)

			if tt.wantNil {
				if got != nil {
					t.Errorf("getFrame(%d) = %v, want nil (frameCount=%d, writeIndex=%d)",
						tt.offset, got.FrameIndex, ed.frameCount, ed.writeIndex)
				}
			} else {
				if got == nil {
					t.Errorf("getFrame(%d) = nil, want frame with ID %d (frameCount=%d, writeIndex=%d)",
						tt.offset, tt.wantFrameID, ed.frameCount, ed.writeIndex)
					return
				}
				if got.FrameIndex != tt.wantFrameID {
					t.Errorf("getFrame(%d) returned frame with ID %d, want %d (frameCount=%d, writeIndex=%d)",
						tt.offset, got.FrameIndex, tt.wantFrameID, ed.frameCount, ed.writeIndex)
				}
			}
		})
	}
}

// TestAsyncDetector_getFrame_SequentialAccess tests accessing frames sequentially
func TestAsyncDetector_getFrame_SequentialAccess(t *testing.T) {
	bufferSize := 5
	framesToAdd := 7

	ed := &AsyncDetector{
		frameBuffer: make([]*rtapi.LobbySessionStateFrame, bufferSize),
		writeIndex:  0,
		frameCount:  0,
	}

	// Add frames
	for i := 0; i < framesToAdd; i++ {
		frame := &rtapi.LobbySessionStateFrame{
			FrameIndex: uint32(i),
		}
		ed.addFrameToBuffer(frame)
	}

	// Expected frames in buffer: [2, 3, 4, 5, 6]
	// writeIndex should be at 2 (wrapped around)
	// frameCount should be 5 (buffer size)

	// Access all frames sequentially
	expected := []uint32{6, 5, 4, 3, 2} // Most recent to oldest
	for i := 0; i < len(expected); i++ {
		frame := ed.getFrame(i)
		if frame == nil {
			t.Errorf("getFrame(%d) = nil, want frame with ID %d", i, expected[i])
			continue
		}
		if frame.FrameIndex != expected[i] {
			t.Errorf("getFrame(%d) = frame with ID %d, want %d", i, frame.FrameIndex, expected[i])
		}
	}

	// Accessing beyond available frames should return nil
	if frame := ed.getFrame(5); frame != nil {
		t.Errorf("getFrame(5) = %v, want nil (beyond frameCount)", frame.FrameIndex)
	}
}
