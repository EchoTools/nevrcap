package codec

import (
	"os"
	"testing"

	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// writeLegacyV1File writes a v1-format file (zstd-compressed,
// varint-length-delimited protobuf messages) for testing the reader.
// This replicates what the deleted NewLegacyWriter produced.
func writeLegacyV1File(t *testing.T, filename string, header *telemetry.TelemetryHeader, frames ...*telemetry.LobbySessionStateFrame) {
	t.Helper()

	f, err := os.Create(filename)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	enc, err := zstd.NewWriter(f, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		f.Close()
		t.Fatalf("create zstd encoder: %v", err)
	}

	writeMsg := func(msg proto.Message) {
		t.Helper()
		data, err := proto.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// Write varint length prefix
		var buf [10]byte
		length := uint64(len(data))
		i := 0
		for length >= 0x80 {
			buf[i] = byte(length) | 0x80
			length >>= 7
			i++
		}
		buf[i] = byte(length)
		i++
		if _, err := enc.Write(buf[:i]); err != nil {
			t.Fatalf("write varint: %v", err)
		}
		if _, err := enc.Write(data); err != nil {
			t.Fatalf("write data: %v", err)
		}
	}

	writeMsg(header)
	for _, frame := range frames {
		writeMsg(frame)
	}

	if err := enc.Close(); err != nil {
		t.Fatalf("close encoder: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
}

// TestLegacyReaderReadBack writes a file using the v1 wire format and
// verifies LegacyReader can read it back.
func TestLegacyReaderReadBack(t *testing.T) {
	tempFile := t.TempDir() + "/test.nevrcap"

	header := &telemetry.TelemetryHeader{
		CaptureId: "test-capture",
		CreatedAt: timestamppb.Now(),
		Metadata:  map[string]string{"test": "true"},
	}
	frame := createTestFrame(t)

	writeLegacyV1File(t, tempFile, header, frame)

	reader, err := NewLegacyReader(tempFile)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer reader.Close()

	readHeader, err := reader.ReadHeader()
	if err != nil {
		t.Fatalf("Failed to read header: %v", err)
	}
	if readHeader.CaptureId != header.CaptureId {
		t.Errorf("CaptureId = %q, want %q", readHeader.CaptureId, header.CaptureId)
	}

	readFrame, err := reader.ReadFrame()
	if err != nil {
		t.Fatalf("Failed to read frame: %v", err)
	}
	if readFrame.FrameIndex != frame.FrameIndex {
		t.Errorf("FrameIndex = %d, want %d", readFrame.FrameIndex, frame.FrameIndex)
	}
}
