package conversion

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/echotools/tape/pkg/codec"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestConvertFile_Nevrcap(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "test.nevrcap")
	outputPath := filepath.Join(dir, "test.tape")

	// Write a v1 nevrcap file with 3 frames.
	header := &telemetryv1.TelemetryHeader{
		CaptureId: "convert-test",
		CreatedAt: timestamppb.Now(),
		Metadata:  map[string]string{"version": "1"},
	}

	frames := make([]*telemetryv1.LobbySessionStateFrame, 3)
	for i := range frames {
		ts := header.GetCreatedAt().AsTime().Add(
			100 * 1000 * 1000 * int64ToNsDuration(int64(i)),
		)
		frames[i] = &telemetryv1.LobbySessionStateFrame{
			FrameIndex: uint32(i), //nolint:gosec // test index fits uint32
			Timestamp:  timestamppb.New(ts),
			Session: &enginev1.SessionResponse{
				SessionId:  "sess-1",
				GameStatus: "playing",
				BluePoints: int32(i), //nolint:gosec // test value fits int32
				Teams: []*enginev1.Team{
					{Players: []*enginev1.TeamMember{{SlotNumber: 0, DisplayName: "P1"}}},
					{Players: []*enginev1.TeamMember{{SlotNumber: 1, DisplayName: "P2"}}},
				},
			},
		}
	}

	writeLegacyV1File(t, inputPath, header, frames...)

	result, err := ConvertFile(inputPath, outputPath)
	if err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}

	if result.FrameCount != 3 {
		t.Errorf("FrameCount = %d, want 3", result.FrameCount)
	}
	if result.InputPath != inputPath {
		t.Errorf("InputPath = %q, want %q", result.InputPath, inputPath)
	}
	if result.OutputPath != outputPath {
		t.Errorf("OutputPath = %q, want %q", result.OutputPath, outputPath)
	}

	// Verify the output is a valid tape file.
	verifyTapeFile(t, outputPath, 3)
}

func TestConvertFile_EchoReplay(t *testing.T) {
	const samplePath = "../../testdata/sample.echoreplay"
	if _, err := os.Stat(samplePath); os.IsNotExist(err) {
		t.Skip("testdata/sample.echoreplay not available")
	}

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "sample.tape")

	result, err := ConvertFile(samplePath, outputPath)
	if err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}

	if result.FrameCount == 0 {
		t.Error("expected non-zero frame count")
	}

	t.Logf("converted %d frames, %d events", result.FrameCount, result.EventCount)

	// Verify the output is a valid tape file.
	verifyTapeFile(t, outputPath, result.FrameCount)
}

func TestConvertFile_UnsupportedFormat(t *testing.T) {
	_, err := ConvertFile("test.xyz", "out.tape")
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

func TestConvertFile_MissingInput(t *testing.T) {
	_, err := ConvertFile("/nonexistent/file.nevrcap", "/tmp/out.tape")
	if err == nil {
		t.Fatal("expected error for missing input")
	}
}

// verifyTapeFile opens a tape file and reads all frames, checking the count.
func verifyTapeFile(t *testing.T, path string, expectedFrames uint32) {
	t.Helper()

	reader, err := codec.NewReader(path)
	if err != nil {
		t.Fatalf("open tape for verification: %v", err)
	}
	defer reader.Close()

	hdr, err := reader.ReadHeader()
	if err != nil {
		t.Fatalf("read tape header: %v", err)
	}
	if hdr.FormatVersion != 2 {
		t.Errorf("FormatVersion = %d, want 2", hdr.FormatVersion)
	}

	var count uint32
	for {
		_, err := reader.ReadFrame()
		if err != nil {
			break
		}
		count++
	}

	if count != expectedFrames {
		t.Errorf("frame count = %d, want %d", count, expectedFrames)
	}

	footer, err := reader.ReadFooter()
	if err != nil {
		t.Fatalf("read footer: %v", err)
	}
	if footer.FrameCount != expectedFrames {
		t.Errorf("footer.FrameCount = %d, want %d", footer.FrameCount, expectedFrames)
	}
}

// int64ToNsDuration converts an int64 to time.Duration for multiplication.
// This avoids the linter complaint about untyped int * Duration.
func int64ToNsDuration(n int64) time.Duration {
	return time.Duration(n)
}
