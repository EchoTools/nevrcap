package main

import (
	"errors"
	"fmt"
	"io"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/echotools/tape/pkg/codec"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
)

func newTrimCommand() *cobra.Command {
	var (
		startMs uint32
		endMs   uint32
		output  string
	)

	cmd := &cobra.Command{
		Use:   "trim <file.tape>",
		Short: "Extract a time range from a tape file",
		Long: `Read a .tape file and write a new .tape containing only the frames
within the specified time range [--start, --end]. Frame timestamps are
adjusted to start from 0. The header is preserved; the footer is regenerated
with updated duration and indexes.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" {
				return fmt.Errorf("output file required (use -o <file.tape>)")
			}
			if endMs > 0 && endMs <= startMs {
				return fmt.Errorf("--end (%d) must be greater than --start (%d)", endMs, startMs)
			}
			return runTrim(cmd, args[0], output, startMs, endMs)
		},
	}

	cmd.Flags().Uint32Var(&startMs, "start", 0, "start time in milliseconds (inclusive)")
	cmd.Flags().Uint32Var(&endMs, "end", 0, "end time in milliseconds (inclusive, 0 = no limit)")
	cmd.Flags().StringVarP(&output, "o", "o", "", "output file path")

	return cmd
}

func runTrim(cmd *cobra.Command, inputPath, outputPath string, startMs, endMs uint32) error {
	out := cmd.OutOrStdout()

	reader, err := codec.NewReader(inputPath)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer reader.Close() //nolint:errcheck // best-effort cleanup

	header, err := reader.ReadHeader()
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}

	writer, err := codec.NewWriter(outputPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}

	// Write header (preserved from source).
	if writeErr := writer.WriteHeader(header); writeErr != nil {
		writer.Close() //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("write header: %w", writeErr)
	}

	var (
		framesRead    uint32
		framesWritten uint32
		baseOffset    uint32 // timestamp of the first included frame
		lastWrittenTs uint32 // adjusted timestamp of last written frame
		baseSet       bool
	)

	for {
		frame, readErr := reader.ReadFrame()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			writer.Close() //nolint:errcheck // best-effort cleanup
			return fmt.Errorf("read frame %d: %w", framesRead, readErr)
		}

		framesRead++
		tsMs := frame.GetTimestampOffsetMs()

		// Skip frames before the start time.
		if tsMs < startMs {
			continue
		}

		// Stop after the end time (if set).
		if endMs > 0 && tsMs > endMs {
			break
		}

		// Record the base offset from the first included frame.
		if !baseSet {
			baseOffset = tsMs
			baseSet = true
		}

		// Clone the frame so we don't mutate the reader's buffer.
		trimmed := proto.Clone(frame).(*capturepb.Frame)
		adjustedTs := tsMs - baseOffset
		trimmed.SetTimestampOffsetMs(adjustedTs)
		trimmed.SetFrameIndex(framesWritten)
		lastWrittenTs = adjustedTs

		if writeErr := writer.WriteFrame(trimmed); writeErr != nil {
			writer.Close() //nolint:errcheck // best-effort cleanup
			return fmt.Errorf("write frame %d: %w", framesWritten, writeErr)
		}

		framesWritten++
	}

	// Close writes the footer with updated frame count and duration.
	if closeErr := writer.Close(); closeErr != nil {
		return fmt.Errorf("close output: %w", closeErr)
	}

	printf(out, "trimmed %s -> %s\n", inputPath, outputPath)
	printf(out, "  source frames read: %d\n", framesRead)
	printf(out, "  output frames: %d\n", framesWritten)
	if framesWritten > 0 {
		printf(out, "  source range: %dms - %dms\n", baseOffset, baseOffset+lastWrittenTs)
		printf(out, "  output duration: %dms\n", lastWrittenTs)
	} else {
		printf(out, "  no frames in range [%dms, %dms]\n", startMs, endMs)
	}

	return nil
}
