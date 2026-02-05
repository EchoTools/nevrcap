package codecs

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// TestEchoReplayNoExponentNotation verifies that float values are never stored with e-notation
func TestEchoReplayNoExponentNotation(t *testing.T) {
	tempFile := "/tmp/test_no_exponent.echoreplay"
	defer os.Remove(tempFile)

	// Create writer
	writer, err := NewEchoReplayWriter(tempFile)
	if err != nil {
		t.Fatalf("Failed to create writer: %v", err)
	}

	// Create a test frame
	frame := createTestFrame(t)

	// Write the frame
	if err := writer.WriteFrame(frame); err != nil {
		t.Fatalf("Failed to write frame: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Failed to close writer: %v", err)
	}

	// Read the raw content from the zip file to verify no e-notation
	zipReader, err := zip.OpenReader(tempFile)
	if err != nil {
		t.Fatalf("Failed to open zip: %v", err)
	}
	defer zipReader.Close()

	// Find the echoreplay file inside the zip
	var content string
	for _, file := range zipReader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("Failed to open file in zip: %v", err)
		}
		defer rc.Close()

		buf := new(bytes.Buffer)
		if _, err := io.Copy(buf, rc); err != nil {
			t.Fatalf("Failed to read file content: %v", err)
		}
		content = buf.String()
		break
	}

	// Verify no e-notation exists in the file
	// Look for patterns like "1e-6", "1E-6", "1e+6", "1E+6"
	eNotationPatterns := []string{"e-", "E-", "e+", "E+"}
	for _, pattern := range eNotationPatterns {
		// Make sure we're checking numeric context (preceded by a digit)
		idx := strings.Index(content, pattern)
		if idx > 0 {
			// Check if it's actually part of a number (has a digit before it)
			prevChar := content[idx-1]
			if prevChar >= '0' && prevChar <= '9' {
				// Found e-notation in a number
				start := idx - 20
				if start < 0 {
					start = 0
				}
				end := idx + 20
				if end > len(content) {
					end = len(content)
				}
				t.Errorf("Found e-notation pattern '%s' in echoreplay file! Context: ...%s...", pattern, content[start:end])
			}
		}
	}

	t.Logf("Successfully verified no e-notation in echoreplay file")
	t.Logf("Content length: %d bytes", len(content))
}
