package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/echotools/tape/v4/pkg/codec"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
)

func newShowCommand() *cobra.Command {
	var (
		format     string
		showEvents bool
	)

	cmd := &cobra.Command{
		Use:   "show <file.tape>",
		Short: "Display tape file contents",
		Long:  "Read and display the contents of a tape file. Supports text, json, and summary output formats.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShow(cmd, args[0], format, showEvents)
		},
	}

	cmd.Flags().StringVar(&format, "format", "summary", "output format: summary|text|json")
	cmd.Flags().BoolVar(&showEvents, "events", false, "include events in output")

	return cmd
}

func runShow(cmd *cobra.Command, filePath, format string, showEvents bool) error {
	reader, err := codec.NewReader(filePath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer reader.Close() //nolint:errcheck // best-effort cleanup

	header, err := reader.ReadHeader()
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}

	switch format {
	case "summary":
		return showSummary(cmd, reader, header, filePath)
	case "text":
		return showText(cmd, reader, header, showEvents)
	case "json":
		return showJSON(cmd, reader, header, showEvents)
	default:
		return fmt.Errorf("unknown format: %s (use summary, text, or json)", format)
	}
}

func showSummary(cmd *cobra.Command, reader *codec.Reader, header *capturepb.CaptureHeader, filePath string) error {
	out := cmd.OutOrStdout()
	printf(out, "file: %s\n", filePath)
	printf(out, "capture_id: %s\n", header.GetCaptureId())
	printf(out, "format_version: %d\n", header.GetFormatVersion())
	if header.GetCreatedAt() != nil {
		printf(out, "created_at: %s\n", header.GetCreatedAt().AsTime())
	}
	for k, v := range header.GetMetadata() {
		printf(out, "metadata.%s: %s\n", k, v)
	}
	if ea := header.GetEchoArena(); ea != nil {
		printf(out, "session_id: %s\n", ea.GetSessionId())
		printf(out, "map: %s\n", ea.GetMapName())
		printf(out, "match_type: %s\n", ea.GetMatchType())
	}

	// Count frames by reading through.
	var frameCount uint32
	var eventCount uint32
	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("read frame: %w", err)
		}
		frameCount++
		if ea := frame.GetEchoArena(); ea != nil {
			eventCount += uint32(len(ea.GetEvents())) //nolint:gosec // count fits uint32
		}
	}

	printf(out, "frames: %d\n", frameCount)
	printf(out, "events: %d\n", eventCount)

	footer, err := reader.ReadFooter()
	if err == nil {
		printf(out, "duration_ms: %d\n", footer.GetDurationMs())
	}

	return nil
}

func showText(cmd *cobra.Command, reader *codec.Reader, header *capturepb.CaptureHeader, showEvents bool) error {
	out := cmd.OutOrStdout()
	printf(out, "=== Header ===\ncapture_id: %s\n\n", header.GetCaptureId())

	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("read frame: %w", err)
		}

		ea := frame.GetEchoArena()
		if ea == nil {
			printf(out, "frame %d: offset=%dms (no payload)\n",
				frame.GetFrameIndex(), frame.GetTimestampOffsetMs())
			continue
		}

		printf(out, "frame %d: offset=%dms status=%s clock=%.1f blue=%d orange=%d players=%d\n",
			frame.GetFrameIndex(), frame.GetTimestampOffsetMs(),
			ea.GetGameStatus(), ea.GetGameClock(),
			ea.GetBluePoints(), ea.GetOrangePoints(),
			len(ea.GetPlayers()))

		if showEvents {
			for _, evt := range ea.GetEvents() {
				printf(out, "  event: %s\n", describeEvent(evt))
			}
		}
	}

	return nil
}

func showJSON(cmd *cobra.Command, reader *codec.Reader, header *capturepb.CaptureHeader, showEvents bool) error {
	out := cmd.OutOrStdout()
	marshaler := protojson.MarshalOptions{
		Multiline:       true,
		EmitUnpopulated: false,
	}

	headerJSON, err := marshaler.Marshal(header)
	if err != nil {
		return fmt.Errorf("marshal header: %w", err)
	}
	printf(out, "%s\n", headerJSON)

	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("read frame: %w", err)
		}

		if !showEvents {
			if ea := frame.GetEchoArena(); ea != nil {
				ea.Events = nil
			}
		}

		frameJSON, marshalErr := marshaler.Marshal(frame)
		if marshalErr != nil {
			return fmt.Errorf("marshal frame: %w", marshalErr)
		}
		printf(out, "%s\n", frameJSON)
	}

	return nil
}

func describeEvent(evt *capturepb.EchoEvent) string {
	b, err := protojson.Marshal(evt)
	if err != nil {
		return fmt.Sprintf("(marshal error: %v)", err)
	}
	var compact json.RawMessage = b
	out, err := json.Marshal(compact)
	if err != nil {
		return string(b)
	}
	return string(out)
}
