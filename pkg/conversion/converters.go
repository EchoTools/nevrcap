package conversion

import (
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/echotools/tape/pkg/codec"
)

// ConvertNevrcapToEchoReplay converts a .nevrcap file to a .echoreplay file
func ConvertNevrcapToEchoReplay(nevrcapPath, echoReplayPath string) error {
	// Read the .nevrcap file
	nevrcapReader, err := codec.NewLegacyReader(nevrcapPath)
	if err != nil {
		return fmt.Errorf("failed to open nevrcap file: %w", err)
	}
	defer nevrcapReader.Close()

	// Read header (for metadata)
	header, err := nevrcapReader.ReadHeader()
	if err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	// Create the .echoreplay file
	echoWriter, err := codec.NewEchoReplayWriter(echoReplayPath)
	if err != nil {
		return fmt.Errorf("failed to create echoreplay file: %w", err)
	}
	defer echoWriter.Close()

	// Convert frames
	for {
		frame, err := nevrcapReader.ReadFrame()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break // End of file
			}
			return fmt.Errorf("failed to read frame: %w", err)
		}

		// Write in legacy echoreplay format (timestamp + session JSON).
		// Frames without a Session but with PlayerBones still carry useful data.
		if frame.Session == nil && frame.PlayerBones != nil {
			slog.Warn("frame has PlayerBones but no Session", "nevrcap", nevrcapPath)
		}
		if err := echoWriter.WriteFrame(frame); err != nil {
			return fmt.Errorf("failed to write frame to echoreplay: %w", err)
		}
	}

	// Finalize the echoreplay file
	if err := echoWriter.Finalize(); err != nil {
		return fmt.Errorf("failed to finalize echoreplay file: %w", err)
	}

	slog.Info("converted nevrcap to echoreplay",
		"input", nevrcapPath,
		"output", echoReplayPath,
	)
	if header.Metadata != nil {
		slog.Info("source metadata", "metadata", header.Metadata)
	}

	return nil
}
