package codec

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// writeBonesFixture builds a one-member echoreplay from the given raw lines.
func writeBonesFixture(t *testing.T, lines []string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "bones.echoreplay")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("bones")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	for _, l := range lines {
		if _, err := w.Write([]byte(l + "\n")); err != nil {
			t.Fatalf("write line: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	return path
}

// TestDroppedBonesAreCounted is the CANONICAL-001 §3 guard. parseFrameLine
// discarded a bones payload it could not unmarshal without recording anything:
// the session parsed, so the line was not a skipped frame either, and the loss
// was invisible to every counter the reader exposed.
//
// A capture whose bones fail to parse still yields a full frame count and a
// zero SkippedFrames, which reads as a clean conversion.
func TestDroppedBonesAreCounted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		lines       []string
		wantFrames  int
		wantDropped uint32
	}{
		{
			name: "well-formed bones are not counted",
			lines: []string{
				`2023/01/01 12:00:00.000` + "\t" + `{"session_id":"1"}` + "\t" + `{"user_bones":[]}`,
				`2023/01/01 12:00:01.000` + "\t" + `{"session_id":"2"}` + "\t" + `{"user_bones":[]}`,
			},
			wantFrames: 2,
		},
		{
			name: "an absent payload is not a dropped payload",
			lines: []string{
				`2023/01/01 12:00:00.000` + "\t" + `{"session_id":"1"}`,
			},
			wantFrames: 1,
		},
		{
			name: "malformed bones JSON is counted, and the frame survives",
			lines: []string{
				`2023/01/01 12:00:00.000` + "\t" + `{"session_id":"1"}` + "\t" + `{not json}`,
				`2023/01/01 12:00:01.000` + "\t" + `{"session_id":"2"}` + "\t" + `{"user_bones":[]}`,
				`2023/01/01 12:00:02.000` + "\t" + `{"session_id":"3"}` + "\t" + `{"user_bones":`,
			},
			wantFrames:  3,
			wantDropped: 2,
		},
		{
			// The reader sets DiscardUnknown (echoreplay.go:134) so the engine
			// can add fields without breaking playback. That tolerance is
			// deliberate, so an unknown field is not a dropped payload.
			name: "an unknown field is tolerated, not dropped",
			lines: []string{
				`2023/01/01 12:00:00.000` + "\t" + `{"session_id":"1"}` + "\t" + `{"user_bones":[],"nope":1}`,
			},
			wantFrames: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader, err := NewEchoReplayReader(writeBonesFixture(t, tt.lines))
			if err != nil {
				t.Fatalf("open reader: %v", err)
			}
			defer reader.Close() //nolint:errcheck // read-only assertion path

			var frames int
			for {
				_, err := reader.ReadFrame()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatalf("read frame: %v", err)
				}
				frames++
			}

			if frames != tt.wantFrames {
				t.Errorf("read %d frames, want %d", frames, tt.wantFrames)
			}
			if got := reader.SkippedFrames(); got != 0 {
				t.Errorf("SkippedFrames = %d, want 0 — the session parsed on every line", got)
			}
			if got := reader.DroppedBones(); got != tt.wantDropped {
				t.Errorf("DroppedBones = %d, want %d — a bones payload the reader could not "+
					"parse must not vanish silently", got, tt.wantDropped)
			}
		})
	}
}
