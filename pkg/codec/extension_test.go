package codec

import (
	"os"
	"testing"

	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestIsValidExtension(t *testing.T) {
	tests := []struct {
		ext  string
		want bool
	}{
		{".tape", true},
		{".nevrcap", true},
		{".echoreplay", false},
		{".zip", false},
		{"", false},
		{"tape", false},  // missing dot
		{".TAPE", false}, // case-sensitive
		{".Tape", false}, // case-sensitive
	}
	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			if got := IsValidExtension(tt.ext); got != tt.want {
				t.Errorf("IsValidExtension(%q) = %v, want %v", tt.ext, got, tt.want)
			}
		})
	}
}

func TestDefaultOutputFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"capture.nevrcap", "capture.tape"},
		{"capture.tape", "capture.tape"},
		{"capture.echoreplay", "capture.tape"},
		{"capture", "capture.tape"},
		{"/path/to/file.nevrcap", "/path/to/file.tape"},
		{"file.tar.gz", "file.tar.tape"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := DefaultOutputFilename(tt.input); got != tt.want {
				t.Errorf("DefaultOutputFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtensionConstants(t *testing.T) {
	if ExtTape != ".tape" {
		t.Errorf("ExtTape = %q, want %q", ExtTape, ".tape")
	}
	if ExtNevrcap != ".nevrcap" {
		t.Errorf("ExtNevrcap = %q, want %q", ExtNevrcap, ".nevrcap")
	}
}

// TestReadWriteBothExtensions verifies that the codec can read and write files
// with both .tape and .nevrcap extensions (same binary format).
func TestReadWriteBothExtensions(t *testing.T) {
	extensions := []string{ExtTape, ExtNevrcap}

	for _, ext := range extensions {
		t.Run(ext, func(t *testing.T) {
			tmpFile := t.TempDir() + "/test" + ext
			defer os.Remove(tmpFile)

			// Write
			writer, err := NewLegacyWriter(tmpFile)
			if err != nil {
				t.Fatalf("NewLegacyWriter(%q): %v", tmpFile, err)
			}

			header := &telemetry.TelemetryHeader{
				CaptureId: "ext-test-" + ext,
				CreatedAt: timestamppb.Now(),
			}
			if err := writer.WriteHeader(header); err != nil {
				t.Fatalf("WriteHeader: %v", err)
			}

			frame := createTestFrame(t)
			if err := writer.WriteFrame(frame); err != nil {
				t.Fatalf("WriteFrame: %v", err)
			}
			if err := writer.Close(); err != nil {
				t.Fatalf("Close writer: %v", err)
			}

			// Read
			reader, err := NewLegacyReader(tmpFile)
			if err != nil {
				t.Fatalf("NewLegacyReader(%q): %v", tmpFile, err)
			}
			defer reader.Close()

			readHeader, err := reader.ReadHeader()
			if err != nil {
				t.Fatalf("ReadHeader: %v", err)
			}
			if readHeader.CaptureId != header.CaptureId {
				t.Errorf("CaptureId = %q, want %q", readHeader.CaptureId, header.CaptureId)
			}

			readFrame, err := reader.ReadFrame()
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}
			if readFrame.FrameIndex != frame.FrameIndex {
				t.Errorf("FrameIndex = %d, want %d", readFrame.FrameIndex, frame.FrameIndex)
			}
		})
	}
}

// TestRoundTripCrossExtension writes with one extension and reads back — the
// binary format is identical regardless of extension.
func TestRoundTripCrossExtension(t *testing.T) {
	dir := t.TempDir()
	tapePath := dir + "/capture.tape"
	nevrcapPath := dir + "/capture.nevrcap"

	// Write as .tape
	writer, err := NewLegacyWriter(tapePath)
	if err != nil {
		t.Fatalf("NewLegacyWriter: %v", err)
	}
	header := &telemetry.TelemetryHeader{
		CaptureId: "cross-ext-test",
		CreatedAt: timestamppb.Now(),
	}
	if err := writer.WriteHeader(header); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	frame := createTestFrame(t)
	if err := writer.WriteFrame(frame); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	writer.Close()

	// Copy the .tape file to .nevrcap — same bytes, different name
	data, err := os.ReadFile(tapePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := os.WriteFile(nevrcapPath, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Read from .nevrcap copy
	reader, err := NewLegacyReader(nevrcapPath)
	if err != nil {
		t.Fatalf("NewLegacyReader: %v", err)
	}
	defer reader.Close()

	readHeader, err := reader.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if readHeader.CaptureId != header.CaptureId {
		t.Errorf("CaptureId = %q, want %q", readHeader.CaptureId, header.CaptureId)
	}
}
