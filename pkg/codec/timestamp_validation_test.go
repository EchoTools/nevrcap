package codec

import (
	"testing"
	"time"
)

// fastParseTimestamp used to validate only the length, so any 23-byte input
// produced a plausible time.Date instead of an error. A fabricated timestamp
// becomes the synthesized header's baseTime and shifts every frame offset in
// the capture, and the bad line is counted as a good frame rather than a skip.

func TestFastParseTimestamp_ValidRoundTrips(t *testing.T) {
	got, err := fastParseTimestamp([]byte("2026/03/15 14:30:00.250"))
	if err != nil {
		t.Fatalf("valid timestamp rejected: %v", err)
	}
	want := time.Date(2026, 3, 15, 14, 30, 0, 250*1000000, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestFastParseTimestamp_RejectsGarbage(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		// The original report: 23 bytes of arbitrary text was accepted and
		// yielded year 60660.
		{"all letters", "garbageGARBAGEgarbageGA"},
		{"letters in year", "20x6/03/15 14:30:00.250"},
		{"letter in month", "2026/0a/15 14:30:00.250"},
		{"letter in millis", "2026/03/15 14:30:00.2z0"},
		{"wrong date separator", "2026-03/15 14:30:00.250"},
		{"wrong second separator", "2026/03-15 14:30:00.250"},
		{"missing space", "2026/03/15T14:30:00.250"},
		{"wrong time separator", "2026/03/15 14-30:00.250"},
		{"wrong millis separator", "2026/03/15 14:30:00,250"},
		{"month out of range", "2026/13/15 14:30:00.250"},
		{"month zero", "2026/00/15 14:30:00.250"},
		{"day out of range", "2026/03/40 14:30:00.250"},
		{"day zero", "2026/03/00 14:30:00.250"},
		{"hour out of range", "2026/03/15 24:30:00.250"},
		{"minute out of range", "2026/03/15 14:60:00.250"},
		{"too short", "2026/03/15 14:30:00.25"},
		{"too long", "2026/03/15 14:30:00.2500"},
		{"empty", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := fastParseTimestamp([]byte(tc.in))
			if err == nil {
				t.Fatalf("accepted %q and fabricated %v; want an error", tc.in, got)
			}
			if !got.IsZero() {
				t.Fatalf("rejected %q but returned non-zero time %v", tc.in, got)
			}
		})
	}
}

// Leap seconds: the engine can emit :60, and it must not be refused.
func TestFastParseTimestamp_AcceptsLeapSecond(t *testing.T) {
	if _, err := fastParseTimestamp([]byte("2026/06/30 23:59:60.000")); err != nil {
		t.Fatalf("leap second rejected: %v", err)
	}
}

// Every timestamp the writer emits must parse back — the two halves of the
// fixed-width codec have to agree.
func TestFastParseTimestamp_RoundTripsWriterOutput(t *testing.T) {
	stamps := []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 12, 31, 23, 59, 59, 999*1000000, time.UTC),
		time.Date(2025, 7, 4, 9, 5, 3, 7*1000000, time.UTC),
	}
	for _, want := range stamps {
		var buf [23]byte
		fastFormatTimestamp(buf[:], want)
		got, err := fastParseTimestamp(buf[:])
		if err != nil {
			t.Fatalf("writer emitted %q which the parser rejects: %v", buf, err)
		}
		if !got.Equal(want) {
			t.Fatalf("round-trip: got %v, want %v (wire %q)", got, want, buf)
		}
	}
}
