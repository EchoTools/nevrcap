package conversion

import (
	"errors"
	"fmt"
	"io"

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

		// Write in legacy echoreplay format (timestamp + session JSON)
		if frame.Session != nil {
			if err := echoWriter.WriteFrame(frame); err != nil {
				return fmt.Errorf("failed to write frame to echoreplay: %w", err)
			}
		}
	}

	// Finalize the echoreplay file
	if err := echoWriter.Finalize(); err != nil {
		return fmt.Errorf("failed to finalize echoreplay file: %w", err)
	}

	fmt.Printf("Successfully converted %s to %s\n", nevrcapPath, echoReplayPath)
	if header.Metadata != nil {
		fmt.Printf("Source metadata: %v\n", header.Metadata)
	}

	return nil
}
