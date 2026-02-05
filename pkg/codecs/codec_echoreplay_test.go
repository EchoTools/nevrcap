package codecs

import (
	"archive/zip"
	"os"
	"testing"
)

// TestEchoReplayCodec tests the EchoReplay codec
func TestEchoReplayCodec(t *testing.T) {
	tempFile := "/tmp/test.echoreplay"
	defer os.Remove(tempFile)

	// Test writing
	writer, err := NewEchoReplayWriter(tempFile)
	if err != nil {
		t.Fatalf("Failed to create EchoReplay writer: %v", err)
	}

	// Write frames
	frame := createTestFrame(t)
	if err := writer.WriteFrame(frame); err != nil {
		t.Fatalf("Failed to write frame: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Failed to close writer: %v", err)
	}

	// Test reading
	reader, err := NewEchoReplayReader(tempFile)
	if err != nil {
		t.Fatalf("Failed to create EchoReplay reader: %v", err)
	}
	defer reader.Close()

	frames, err := reader.ReadFrames()
	if err != nil {
		t.Fatalf("Failed to read frames: %v", err)
	}

	if len(frames) != 1 {
		t.Errorf("Expected 1 frame, got %d", len(frames))
	}

	if frames[0].FrameIndex != frame.FrameIndex {
		t.Errorf("Expected frame index %d, got %d", frame.FrameIndex, frames[0].FrameIndex)
	}
}

func TestEchoReplayReader_Resilience(t *testing.T) {
	tmpFile := t.TempDir() + "/bad_content.echoreplay"

	// Create a zip file manually
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatal(err)
	}

	zw := zip.NewWriter(f)
	// Create the inner file with the same name as the zip (minus extension)
	// The reader expects this or .echoreplay
	w, err := zw.Create("bad_content")
	if err != nil {
		t.Fatal(err)
	}

	// Write mixed content
	// 1. Good line with player bones
	w.Write([]byte("2023/01/01 12:00:00.000\t{\"session_id\":\"1\"}\t{\"user_bones\":[]}\n"))
	// 2. Bad timestamp
	w.Write([]byte("BAD_TIMESTAMP\t{\"session_id\":\"2\"}\t{\"user_bones\":[]}\n"))
	// 3. Bad JSON
	w.Write([]byte("2023/01/01 12:00:01.000\t{bad_json}\t{\"user_bones\":[]}\n"))
	// 4. Good line without player bones (Spark format)
	w.Write([]byte("2023/01/01 12:00:02.000\t{\"session_id\":\"4\"}\n"))
	// 5. Good line with player bones
	w.Write([]byte("2023/01/01 12:00:03.000\t{\"session_id\":\"5\"}\t{\"user_bones\":[]}\n"))

	zw.Close()
	f.Close()

	// Read it back
	reader, err := NewEchoReplayReader(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	defer reader.Close()

	frames, err := reader.ReadFrames()
	if err != nil {
		t.Fatalf("ReadFrames failed: %v", err)
	}

	// We expect 3 good frames (1, 4, and 5)
	// Lines 2 and 3 are invalid and should be skipped
	// Line 4 is now valid (Spark format without player bones)
	if len(frames) != 3 {
		t.Errorf("Expected 3 valid frames, got %d", len(frames))
	}

	if len(frames) > 0 && frames[0].Session.SessionId != "1" {
		t.Errorf("Expected first frame session 1, got %s", frames[0].Session.SessionId)
	}
	if len(frames) > 1 && frames[1].Session.SessionId != "4" {
		t.Errorf("Expected second frame session 4, got %s", frames[1].Session.SessionId)
	}
	if len(frames) > 2 && frames[2].Session.SessionId != "5" {
		t.Errorf("Expected third frame session 5, got %s", frames[2].Session.SessionId)
	}
}

// TestFixProtojsonUint64Encoding tests the uint64 string-to-number conversion
func TestFixProtojsonUint64Encoding(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "userid string to number",
			input:    `{"name":"Test","userid":"4355631379520676917","level":50}`,
			expected: `{"name":"Test","userid":4355631379520676917,"level":50}`,
		},
		{
			name:     "rules_changed_at string to number",
			input:    `{"sessionid":"ABC","rules_changed_at":"1234567890123456789"}`,
			expected: `{"sessionid":"ABC","rules_changed_at":1234567890123456789}`,
		},
		{
			name:     "both fields in same JSON",
			input:    `{"userid":"123","rules_changed_at":"456","other":"value"}`,
			expected: `{"userid":123,"rules_changed_at":456,"other":"value"}`,
		},
		{
			name:     "zero values",
			input:    `{"userid":"0","rules_changed_at":"0"}`,
			expected: `{"userid":0,"rules_changed_at":0}`,
		},
		{
			name:     "no uint64 fields - unchanged",
			input:    `{"name":"Test","level":50}`,
			expected: `{"name":"Test","level":50}`,
		},
		{
			name:     "multiple players with userids",
			input:    `{"players":[{"userid":"111"},{"userid":"222"}]}`,
			expected: `{"players":[{"userid":111},{"userid":222}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FixProtojsonUint64Encoding([]byte(tt.input))
			if string(result) != tt.expected {
				t.Errorf("FixProtojsonUint64Encoding() = %q, want %q", string(result), tt.expected)
			}
		})
	}
}

// TestFixExponentNotation tests the scientific notation to decimal conversion
func TestFixExponentNotation(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "positive exponent small",
			input:    `{"value":1e6}`,
			expected: `{"value":1000000}`,
		},
		{
			name:     "negative exponent",
			input:    `{"value":1e-6}`,
			expected: `{"value":0.000001}`,
		},
		{
			name:     "decimal with positive exponent",
			input:    `{"value":1.5e3}`,
			expected: `{"value":1500}`,
		},
		{
			name:     "decimal with negative exponent",
			input:    `{"value":1.5e-3}`,
			expected: `{"value":0.0015}`,
		},
		{
			name:     "negative number with exponent",
			input:    `{"value":-1.5e-3}`,
			expected: `{"value":-0.0015}`,
		},
		{
			name:     "multiple values with exponents",
			input:    `{"x":1e-6,"y":2e-6,"z":3e-6}`,
			expected: `{"x":0.000001,"y":0.000002,"z":0.000003}`,
		},
		{
			name:     "mixed exponents and regular numbers",
			input:    `{"a":1.4444444,"b":1e-6,"c":42}`,
			expected: `{"a":1.4444444,"b":0.000001,"c":42}`,
		},
		{
			name:     "capital E notation",
			input:    `{"value":1.5E-3}`,
			expected: `{"value":0.0015}`,
		},
		{
			name:     "no exponents - unchanged",
			input:    `{"value":1.4444444,"other":42.123}`,
			expected: `{"value":1.4444444,"other":42.123}`,
		},
		{
			name:     "very small number",
			input:    `{"value":1.23e-10}`,
			expected: `{"value":0.000000000123}`,
		},
		{
			name:     "very large number",
			input:    `{"value":1.23e10}`,
			expected: `{"value":12300000000}`,
		},
		{
			name:     "explicit positive exponent",
			input:    `{"value":1.5e+3}`,
			expected: `{"value":1500}`,
		},
		{
			name:     "UUID with hex that looks like scientific notation",
			input:    `{"session_id":"07450BBB-06BF-4E7E-9C04-EBCD4AF043D4"}`,
			expected: `{"session_id":"07450BBB-06BF-4E7E-9C04-EBCD4AF043D4"}`,
		},
		{
			name:     "multiple UUIDs and numbers with exponents",
			input:    `{"id":"ABC-4E7-DEF","value":1e-6,"uuid":"12-3E4-56"}`,
			expected: `{"id":"ABC-4E7-DEF","value":0.000001,"uuid":"12-3E4-56"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FixExponentNotation([]byte(tt.input))
			if string(result) != tt.expected {
				t.Errorf("FixExponentNotation() = %q, want %q", string(result), tt.expected)
			}
		})
	}
}
