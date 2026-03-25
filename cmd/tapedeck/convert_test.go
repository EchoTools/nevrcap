package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestConvertCommand_DryRun(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "test.nevrcap")
	writeTestNevrcap(t, inputPath)

	out, err := runTapedeck(t, "convert", "--dry-run", inputPath)
	if err != nil {
		t.Fatalf("tapedeck convert --dry-run: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "test.nevrcap") {
		t.Errorf("expected input path in output, got: %s", out)
	}
	if !strings.Contains(out, ".tape") {
		t.Errorf("expected .tape extension in output, got: %s", out)
	}
	if !strings.Contains(out, "would be converted") {
		t.Errorf("expected 'would be converted' in output, got: %s", out)
	}
}

func TestConvertCommand_SingleFile(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "test.nevrcap")
	writeTestNevrcap(t, inputPath)

	outputDir := filepath.Join(dir, "out")

	out, err := runTapedeck(t, "convert", "--output-dir", outputDir, inputPath)
	if err != nil {
		t.Fatalf("tapedeck convert: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "converted: 1") {
		t.Errorf("expected 'converted: 1' in output, got: %s", out)
	}

	// Verify output file exists.
	outputPath := filepath.Join(outputDir, "test.tape")
	if _, statErr := os.Stat(outputPath); os.IsNotExist(statErr) {
		t.Errorf("output file not created: %s", outputPath)
	}
}

func TestConvertCommand_SkipExisting(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "test.nevrcap")
	writeTestNevrcap(t, inputPath)

	// Create existing output file.
	outputPath := filepath.Join(dir, "test.tape")
	if err := os.WriteFile(outputPath, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runTapedeck(t, "convert", inputPath)
	if err != nil {
		t.Fatalf("tapedeck convert: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "skipped: 1") {
		t.Errorf("expected 'skipped: 1' in output, got: %s", out)
	}
}

func TestConvertCommand_NoArgs(t *testing.T) {
	_, err := runTapedeck(t, "convert")
	if err == nil {
		t.Fatal("expected error with no args")
	}
}

// runTapedeck runs the tapedeck CLI via `go run .` in the cmd/tapedeck directory.
func runTapedeck(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmdArgs := append([]string{"run", "."}, args...)
	cmd := exec.Command("go", cmdArgs...)
	cmd.Dir = filepath.Join(findModuleRoot(t), "cmd", "tapedeck")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// findModuleRoot walks up from the test binary's working directory to find go.mod.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find module root")
		}
		dir = parent
	}
}

// writeTestNevrcap creates a minimal v1 nevrcap file for testing.
func writeTestNevrcap(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	enc, err := zstd.NewWriter(f, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		f.Close() //nolint:errcheck // test helper
		t.Fatal(err)
	}

	writeMsg := func(msg proto.Message) {
		t.Helper()
		data, marshalErr := proto.Marshal(msg)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
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
		if _, writeErr := enc.Write(buf[:i]); writeErr != nil {
			t.Fatal(writeErr)
		}
		if _, writeErr := enc.Write(data); writeErr != nil {
			t.Fatal(writeErr)
		}
	}

	now := timestamppb.Now()
	writeMsg(&telemetryv1.TelemetryHeader{
		CaptureId: "cli-test",
		CreatedAt: now,
	})
	writeMsg(&telemetryv1.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  now,
		Session: &enginev1.SessionResponse{
			SessionId:  "sess-cli",
			GameStatus: "playing",
		},
	})

	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
