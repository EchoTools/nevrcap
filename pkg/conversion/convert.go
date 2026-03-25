package conversion

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/echotools/tape/pkg/codec"
	"github.com/echotools/tape/pkg/events"
	"github.com/echotools/tape/pkg/processing"
)

const (
	extEchoReplay = ".echoreplay"
	extNevrcap    = ".nevrcap"
	extTape       = ".tape"
)

// ConvertResult holds statistics from a single file conversion.
type ConvertResult struct {
	InputPath  string
	OutputPath string
	FrameCount uint32
	EventCount uint32
}

// ConvertFile converts a legacy capture file (.echoreplay or .nevrcap) to
// the v2 tape format. The input format is detected by file extension.
func ConvertFile(inputPath, outputPath string) (*ConvertResult, error) {
	ext := filepath.Ext(inputPath)
	switch ext {
	case extEchoReplay:
		return convertEchoReplay(inputPath, outputPath)
	case extNevrcap, extTape:
		return convertLegacy(inputPath, outputPath)
	default:
		return nil, fmt.Errorf("unsupported input format: %s", ext)
	}
}

func convertEchoReplay(inputPath, outputPath string) (*ConvertResult, error) {
	reader, err := codec.NewEchoReplayReader(inputPath)
	if err != nil {
		return nil, fmt.Errorf("open echoreplay: %w", err)
	}
	defer reader.Close() //nolint:errcheck // best-effort cleanup; primary errors returned from pipeline

	return convertFromV1Reader(&echoReplayAdapter{reader: reader}, inputPath, outputPath)
}

func convertLegacy(inputPath, outputPath string) (*ConvertResult, error) {
	reader, err := codec.NewLegacyReader(inputPath)
	if err != nil {
		return nil, fmt.Errorf("open nevrcap: %w", err)
	}
	defer reader.Close() //nolint:errcheck // best-effort cleanup; primary errors returned from pipeline

	return convertFromV1Reader(&legacyAdapter{reader: reader}, inputPath, outputPath)
}

// v1Reader abstracts over EchoReplay and LegacyReader for the conversion pipeline.
type v1Reader interface {
	ReadHeader() (*telemetryv1.TelemetryHeader, error)
	ReadFrame() (*telemetryv1.LobbySessionStateFrame, error)
	Close() error
}

// echoReplayAdapter wraps EchoReplay to implement v1Reader.
// EchoReplay doesn't have ReadHeader, so we synthesize one from the first frame.
type echoReplayAdapter struct {
	reader     *codec.EchoReplay
	firstFrame *telemetryv1.LobbySessionStateFrame
}

func (a *echoReplayAdapter) ReadHeader() (*telemetryv1.TelemetryHeader, error) {
	// EchoReplay files don't have a separate header. Read the first frame
	// to extract session metadata, then return a synthetic header.
	frame, err := a.reader.ReadFrame()
	if err != nil {
		return nil, fmt.Errorf("read first frame for header: %w", err)
	}
	a.firstFrame = frame

	header := &telemetryv1.TelemetryHeader{
		CaptureId: "echoreplay-import",
		CreatedAt: frame.GetTimestamp(),
	}
	return header, nil
}

func (a *echoReplayAdapter) ReadFrame() (*telemetryv1.LobbySessionStateFrame, error) {
	if a.firstFrame != nil {
		f := a.firstFrame
		a.firstFrame = nil
		return f, nil
	}
	return a.reader.ReadFrame()
}

func (a *echoReplayAdapter) Close() error {
	return a.reader.Close()
}

// legacyAdapter wraps LegacyReader to implement v1Reader.
type legacyAdapter struct {
	reader *codec.LegacyReader
}

func (a *legacyAdapter) ReadHeader() (*telemetryv1.TelemetryHeader, error) {
	return a.reader.ReadHeader()
}

func (a *legacyAdapter) ReadFrame() (*telemetryv1.LobbySessionStateFrame, error) {
	return a.reader.ReadFrame()
}

func (a *legacyAdapter) Close() error {
	return a.reader.Close()
}

// convertFromV1Reader is the core pipeline: read v1 frames, detect events,
// map to v2, and write tape output.
func convertFromV1Reader(reader v1Reader, inputPath, outputPath string) (*ConvertResult, error) {
	v1Header, err := reader.ReadHeader()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	// Read the first frame to enrich the header with session data.
	firstFrame, err := reader.ReadFrame()
	if err != nil {
		return nil, fmt.Errorf("read first frame: %w", err)
	}

	v2Header := MapHeaderFromSession(v1Header, firstFrame.GetSession())
	baseTime := v1Header.GetCreatedAt().AsTime()

	writer, err := codec.NewWriter(outputPath)
	if err != nil {
		return nil, fmt.Errorf("create writer: %w", err)
	}

	// closeWriter is used on error paths to ensure the writer is closed.
	// On the success path, writer.Close() is called explicitly.
	writerClosed := false
	closeWriter := func() {
		if !writerClosed {
			writer.Close() //nolint:errcheck // best-effort cleanup on error path
			writerClosed = true
		}
	}

	if err := writer.WriteHeader(v2Header); err != nil {
		closeWriter()
		return nil, fmt.Errorf("write header: %w", err)
	}

	// Set up event detection.
	detector := events.NewWithDefaultSensors(events.WithSynchronousProcessing())
	processor := processing.NewWithDetector(detector)
	defer processor.Stop()

	result := &ConvertResult{
		InputPath:  inputPath,
		OutputPath: outputPath,
	}

	// Process first frame.
	if err := processAndWriteFrame(processor, writer, firstFrame, baseTime, result); err != nil {
		closeWriter()
		return nil, err
	}

	// Process remaining frames.
	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			closeWriter()
			return nil, fmt.Errorf("read frame: %w", err)
		}

		if err := processAndWriteFrame(processor, writer, frame, baseTime, result); err != nil {
			closeWriter()
			return nil, err
		}
	}

	// Close writer (writes footer).
	writerClosed = true
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close writer: %w", err)
	}

	// Verify output.
	if err := verifyOutput(outputPath, result.FrameCount); err != nil {
		return nil, fmt.Errorf("verify output: %w", err)
	}

	return result, nil
}

func processAndWriteFrame(
	processor *processing.Processor,
	writer *codec.Writer,
	v1Frame *telemetryv1.LobbySessionStateFrame,
	baseTime time.Time,
	result *ConvertResult,
) error {
	// Run event detection on the v1 frame.
	processor.DetectEvents(v1Frame)

	// Drain any detected events and attach them to the frame.
	drainEvents(processor, v1Frame)

	// Map v1 frame to v2.
	v2Frame := MapFrame(v1Frame, baseTime)

	// Count events on the mapped frame.
	if ea := v2Frame.GetEchoArena(); ea != nil {
		result.EventCount += uint32(len(ea.GetEvents())) //nolint:gosec // event count fits uint32
	}

	if err := writer.WriteFrame(v2Frame); err != nil {
		return fmt.Errorf("write frame %d: %w", result.FrameCount, err)
	}

	result.FrameCount++
	return nil
}

// drainEvents pulls all pending events from the processor channel and
// appends them to the v1 frame's event list (before mapping to v2).
func drainEvents(processor *processing.Processor, frame *telemetryv1.LobbySessionStateFrame) {
	for {
		select {
		case evts := <-processor.EventsChan():
			frame.Events = append(frame.Events, evts...)
		default:
			return
		}
	}
}

func verifyOutput(outputPath string, expectedFrames uint32) error {
	reader, err := codec.NewReader(outputPath)
	if err != nil {
		return fmt.Errorf("open for verification: %w", err)
	}
	defer reader.Close() //nolint:errcheck // best-effort cleanup; primary errors returned from pipeline

	if _, err := reader.ReadHeader(); err != nil {
		return fmt.Errorf("read header: %w", err)
	}

	var count uint32
	for {
		_, err := reader.ReadFrame()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("read frame %d: %w", count, err)
		}
		count++
	}

	if count != expectedFrames {
		return fmt.Errorf("frame count mismatch: output has %d, expected %d", count, expectedFrames)
	}

	return nil
}
