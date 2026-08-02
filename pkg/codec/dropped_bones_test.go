package codec

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
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

// parityResult is what one entry point observed over a fixture: the session IDs
// of the frames it returned (in order), and the reader's loss counters.
type parityResult struct {
	sessions []string
	skipped  uint32
	dropped  uint32
}

// readViaEntryPoint drains a fixture through ReadFrame (reuse=false) or the
// zero-alloc ReadFrameTo (reuse=true), recording the sessions returned and the
// SkippedFrames/DroppedBones the reader counted.
func readViaEntryPoint(t *testing.T, path string, reuse bool) parityResult {
	t.Helper()

	reader, err := NewEchoReplayReader(path)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	defer reader.Close() //nolint:errcheck // read-only assertion path

	res := parityResult{}
	if reuse {
		buf := &telemetry.LobbySessionStateFrame{}
		for {
			ok, err := reader.ReadFrameTo(buf)
			if !ok {
				if errors.Is(err, io.EOF) || err == nil {
					break
				}
				t.Fatalf("ReadFrameTo: %v", err)
			}
			res.sessions = append(res.sessions, buf.GetSession().GetSessionId())
		}
	} else {
		for {
			frame, err := reader.ReadFrame()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}
			res.sessions = append(res.sessions, frame.GetSession().GetSessionId())
		}
	}
	res.skipped = reader.SkippedFrames()
	res.dropped = reader.DroppedBones()
	return res
}

// TestReadFrameToReadFrameParity is the release-audit v4.0.0 R4 guard. The
// reuse API ReadFrameTo used to return a bones error for a malformed bones
// payload, so its caller incremented SkippedFrames and discarded the whole
// line — losing a valid session that ReadFrame retained (counting the bones
// loss via DroppedBones instead). The two entry points must agree line for
// line: the same frames returned, the same lines skipped, the same bones
// payloads counted as dropped.
func TestReadFrameToReadFrameParity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		lines        []string
		wantSessions []string
		wantSkipped  uint32
		wantDropped  uint32
	}{
		{
			name: "valid lines agree",
			lines: []string{
				`2023/01/01 12:00:00.000` + "\t" + `{"session_id":"1"}` + "\t" + `{"user_bones":[]}`,
				`2023/01/01 12:00:01.000` + "\t" + `{"session_id":"2"}`,
			},
			wantSessions: []string{"1", "2"},
		},
		{
			name: "malformed bones are retained, not skipped",
			lines: []string{
				`2023/01/01 12:00:00.000` + "\t" + `{"session_id":"1"}` + "\t" + `{not json}`,
				`2023/01/01 12:00:01.000` + "\t" + `{"session_id":"2"}` + "\t" + `{"user_bones":`,
			},
			wantSessions: []string{"1", "2"},
			wantDropped:  2,
		},
		{
			name: "skip lines are skipped by both entry points",
			lines: []string{
				`2023/01/01 12:00:00.000` + "\t" + `{"session_id":"1"}`,
				`not a timestamp` + "\t" + `{"session_id":"2"}`,
				`2023/01/01 12:00:02.000` + "\t" + `{"session_id":"3"}` + "\t" + `{bad}`,
			},
			wantSessions: []string{"1", "3"},
			wantSkipped:  1,
			wantDropped:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := writeBonesFixture(t, tt.lines)
			gotRead := readViaEntryPoint(t, path, false)
			gotReuse := readViaEntryPoint(t, path, true)

			// The two entry points must agree line for line.
			if !slices.Equal(gotRead.sessions, gotReuse.sessions) {
				t.Errorf("ReadFrame sessions %v != ReadFrameTo sessions %v",
					gotRead.sessions, gotReuse.sessions)
			}
			if gotRead.skipped != gotReuse.skipped {
				t.Errorf("ReadFrame SkippedFrames=%d != ReadFrameTo SkippedFrames=%d",
					gotRead.skipped, gotReuse.skipped)
			}
			if gotRead.dropped != gotReuse.dropped {
				t.Errorf("ReadFrame DroppedBones=%d != ReadFrameTo DroppedBones=%d",
					gotRead.dropped, gotReuse.dropped)
			}

			// Pin the absolute behavior, not just agreement.
			if !slices.Equal(tt.wantSessions, gotReuse.sessions) {
				t.Errorf("ReadFrameTo sessions = %v, want %v", gotReuse.sessions, tt.wantSessions)
			}
			if gotReuse.skipped != tt.wantSkipped {
				t.Errorf("ReadFrameTo SkippedFrames = %d, want %d", gotReuse.skipped, tt.wantSkipped)
			}
			if gotReuse.dropped != tt.wantDropped {
				t.Errorf("ReadFrameTo DroppedBones = %d, want %d", gotReuse.dropped, tt.wantDropped)
			}
		})
	}
}
