package codecs

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestNevrCap_writeDelimitedMessage(t *testing.T) {
	tests := []struct {
		name    string
		message []byte
	}{
		{"empty message", []byte{}},
		{"short message", []byte{0x01, 0x02, 0x03}},
		{"long message", bytes.Repeat([]byte{0xAB}, 300)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			codec := &NevrCap{writer: &buf}
			err := codec.writeDelimitedMessage(tt.message)
			if err != nil {
				t.Fatalf("writeDelimitedMessage() error = %v", err)
			}

			// Now decode the varint length
			var (
				length uint64
				shift  uint
			)
			for i := 0; i < 10; i++ {
				b, err := buf.ReadByte()
				if err != nil {
					t.Fatalf("failed to read varint: %v", err)
				}
				length |= uint64(b&0x7F) << shift
				if b&0x80 == 0 {
					break
				}
				shift += 7
			}
			if int(length) != len(tt.message) {
				t.Errorf("length mismatch: got %d, want %d", length, len(tt.message))
			}
			got := make([]byte, length)
			_, err = io.ReadFull(&buf, got)
			if err != nil {
				t.Fatalf("failed to read message: %v", err)
			}
			if !bytes.Equal(got, tt.message) {
				t.Errorf("message mismatch: got %v, want %v", got, tt.message)
			}
		})
	}
}

func BenchmarkNevrCap_writeDelimitedMessage(b *testing.B) {
	msg := bytes.Repeat([]byte{0x42}, 1024)
	var buf bytes.Buffer
	codec := &NevrCap{writer: &buf}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := codec.writeDelimitedMessage(msg); err != nil {
			b.Fatal(err)
		}
	}
}

// TestZstdCodec tests the Zstd codec for .nevrcap files
func TestZstdCodec(t *testing.T) {
	tempFile := "/tmp/test.nevrcap"
	defer os.Remove(tempFile)

	// Test writing
	writer, err := NewNevrCapWriter(tempFile)
	if err != nil {
		t.Fatalf("Failed to create Zstd writer: %v", err)
	}

	// Write header
	header := &rtapi.TelemetryHeader{
		CaptureId: "test-capture",
		CreatedAt: timestamppb.Now(),
		Metadata: map[string]string{
			"test": "true",
		},
	}

	if err := writer.WriteHeader(header); err != nil {
		t.Fatalf("Failed to write header: %v", err)
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
	reader, err := NewNevrCapReader(tempFile)
	if err != nil {
		t.Fatalf("Failed to create Zstd reader: %v", err)
	}
	defer reader.Close()

	// Read header
	readHeader, err := reader.ReadHeader()
	if err != nil {
		t.Fatalf("Failed to read header: %v", err)
	}

	if readHeader.CaptureId != header.CaptureId {
		t.Errorf("Expected capture ID %s, got %s", header.CaptureId, readHeader.CaptureId)
	}

	// Read frame
	readFrame, err := reader.ReadFrame()
	if err != nil {
		t.Fatalf("Failed to read frame: %v", err)
	}

	if readFrame.FrameIndex != frame.FrameIndex {
		t.Errorf("Expected frame index %d, got %d", frame.FrameIndex, readFrame.FrameIndex)
	}
}
