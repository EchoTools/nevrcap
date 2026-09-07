package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"

	"github.com/echotools/tape/v4/pkg/codec"
	"github.com/echotools/tape/v4/pkg/conversion"
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
	addDictFlag(cmd)

	return cmd
}

func runShow(cmd *cobra.Command, filePath, format string, showEvents bool) error {
	reader, err := openTape(cmd, filePath)
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
	}
	// game_type replaces EchoArenaHeader.match_type and lives on the container
	// header, not in the game band, because it is what SCOPES the game band. It
	// prints verbatim: the engine's spelling is the value, and narrowing it for
	// display would hide the thing an operator is most likely checking.
	if gt := header.GetGameType(); gt != "" {
		facts := conversion.DeriveGameType(gt)
		printf(out, "game_type: %s (mode=%s private=%v tournament=%v)\n",
			facts.Symbol, facts.Mode, facts.Private, facts.Tournament)
	}
	if p := header.GetProducer(); p != "" {
		printf(out, "producer: %s\n", p)
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

	// F9's counter reaching a human. The reader skips envelope variants it does
	// not know so that a new envelope kind does not break a deployed binary, and
	// AGENTS.md §4 forbids that skip being silent: "a skipped line, a rejected
	// frame, an unmapped enum must be counted and the counter must have a
	// consumer." This is the consumer.
	//
	// Printed only when non-zero, deliberately. A "skipped_envelopes: 0" line on
	// every healthy capture is a line operators learn to skip past, and then the
	// one time it says 3 they skip past that too. Silence is the normal state and
	// the message is written to explain itself to someone seeing it for the first
	// time — the actionable fact is not the count, it is that their tapedeck is
	// older than the file.
	if skipped := reader.SkippedEnvelopes(); skipped > 0 {
		printf(out, "skipped_envelopes: %d (written by a newer tape version; "+
			"this binary does not know those envelope kinds and did not read them)\n", skipped)
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
