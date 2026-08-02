package codec

import (
	"strconv"
	"strings"
	"testing"
)

// TestFixProtojsonUint64EncodingMultipleMatches pins byte-for-byte output for
// JSON carrying many string-encoded uint64 fields. The fix runs on the
// reconstruction write path; third-party echoreplay parsers depend on these
// exact bytes (GH #24).
func TestFixProtojsonUint64EncodingMultipleMatches(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "three player userids",
			input:    `{"players":[{"userid":"111","level":5},{"userid":"222","level":7},{"userid":"333","level":9}]}`,
			expected: `{"players":[{"userid":111,"level":5},{"userid":222,"level":7},{"userid":333,"level":9}]}`,
		},
		{
			name:     "two rules_changed_at",
			input:    `{"rules_changed_at":"1234567890123456789","rules_changed_at":"9876543210987654321"}`,
			expected: `{"rules_changed_at":1234567890123456789,"rules_changed_at":9876543210987654321}`,
		},
		{
			name:     "interleaved userid and rules_changed_at",
			input:    `{"userid":"111","rules_changed_at":"222","userid":"333","rules_changed_at":"444","userid":"555"}`,
			expected: `{"userid":111,"rules_changed_at":222,"userid":333,"rules_changed_at":444,"userid":555}`,
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

// TestFixStringEncodedNumberByteIdentityEdgeCases pins the exact skip and
// offset-advance semantics of fixStringEncodedNumber on malformed inputs:
// empty values, non-digit values, unterminated numbers, and a pattern whose
// opening quote would sit on a prior match's closing quote. Byte identity is
// the contract — the function runs on the reconstruction write path.
func TestFixStringEncodedNumberByteIdentityEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty value unchanged",
			input:    `{"userid":""}`,
			expected: `{"userid":""}`,
		},
		{
			name:     "non-digit value unchanged",
			input:    `{"userid":"abc"}`,
			expected: `{"userid":"abc"}`,
		},
		{
			name:     "digits followed by non-quote unchanged",
			input:    `{"userid":"123a"}`,
			expected: `{"userid":"123a"}`,
		},
		{
			name:     "unterminated number unchanged",
			input:    `{"userid":"123`,
			expected: `{"userid":"123`,
		},
		{
			name:     "closing quote at end of buffer still fixed",
			input:    `{"userid":"123"`,
			expected: `{"userid":123`,
		},
		{
			name:     "empty value between valid matches",
			input:    `{"userid":"111","userid":"","userid":"456"}`,
			expected: `{"userid":111,"userid":"","userid":456}`,
		},
		{
			name:     "missing key quote after closing quote not reprocessed",
			input:    `{"userid":"123"userid":"456"}`,
			expected: `{"userid":123userid":"456"}`,
		},
		{
			name:     "key quote at closing quote is a fresh match",
			input:    `{"userid":"123""userid":"456"}`,
			expected: `{"userid":123"userid":456}`,
		},
		{
			name:     "non-digit userid leaves rules_changed_at fixable",
			input:    `{"userid":"123a","rules_changed_at":"456"}`,
			expected: `{"userid":"123a","rules_changed_at":456}`,
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

// TestFixStringEncodedNumberBoundedAllocations is a regression guard against
// reintroducing the O(n^2) behavior of GH #24, where every match allocated a
// fresh copy of the whole remaining buffer. The single-pass rewrite allocates
// once per pattern pass regardless of match count.
func TestFixStringEncodedNumberBoundedAllocations(t *testing.T) {
	input := manyMatchInput(200)
	allocs := testing.AllocsPerRun(10, func() {
		_ = FixProtojsonUint64Encoding(input)
	})
	if allocs > 2 {
		t.Fatalf("FixProtojsonUint64Encoding allocated %.0f times for 400 matches; want at most 2 (one per pattern pass)", allocs)
	}
}

// BenchmarkFixProtojsonUint64EncodingManyMatches measures the fix over a frame
// with hundreds of string-encoded uint64 fields — the shape that made the old
// per-match reallocation quadratic.
func BenchmarkFixProtojsonUint64EncodingManyMatches(b *testing.B) {
	input := manyMatchInput(200)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = FixProtojsonUint64Encoding(input)
	}
}

// manyMatchInput builds a JSON document with n userid and n
// rules_changed_at string-encoded fields.
func manyMatchInput(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"sessionid":"S","players":[`)
	for i := range n {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"userid":"`)
		b.WriteString(strconv.Itoa(100000 + i))
		b.WriteString(`","rules_changed_at":"`)
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(`"}`)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}
