package conversion

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/echotools/tape/pkg/codec"
)

// EchoReplay.ReadFrame counts a line it cannot parse and continues
// (pkg/codec/echoreplay.go:504-508). SkippedFrames() exposed the count, but
// nothing in the conversion pipeline or the CLI ever read it, so a capture whose
// lines were all rejected converted to an empty tape and reported success.
// GH #31 has real files in exactly that shape: 450 lines read, 450 rejected.

// writeReplayWithBadLines produces an uncompressed .echoreplay containing good
// well-formed frames followed by bad unparseable lines.
func writeReplayWithBadLines(t *testing.T, good, bad int) string {
	t.Helper()

	// Build a well-formed zipped replay first so the good lines are exactly what
	// the writer emits, then strip the container and append garbage.
	zipped := filepath.Join(t.TempDir(), "seed.echoreplay")
	w, err := codec.NewEchoReplayWriter(zipped)
	if err != nil {
		t.Fatalf("NewEchoReplayWriter: %v", err)
	}
	base := time.Date(2026, 5, 1, 14, 27, 52, 0, time.UTC)
	for i := range good {
		frame := &telemetryv1.LobbySessionStateFrame{
			FrameIndex: uint32(i), //nolint:gosec // loop bound is a small test constant
			Timestamp:  timestamppb.New(base.Add(time.Duration(i) * 16 * time.Millisecond)),
			Session: &enginev1.SessionResponse{
				SessionId:  "2E668BB6-13D8-4000-B68C-D3D707F292B1",
				MapName:    "mpl_arena_a",
				MatchType:  "Echo_Arena",
				GameStatus: "playing",
			},
		}
		if err := w.WriteFrame(frame); err != nil {
			t.Fatalf("WriteFrame %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("EchoReplay.Close: %v", err)
	}

	zr, err := zip.OpenReader(zipped)
	if err != nil {
		t.Fatalf("open seed zip: %v", err)
	}
	defer zr.Close() //nolint:errcheck // read-only test fixture
	if len(zr.File) == 0 {
		t.Fatal("seed zip has no members")
	}
	member, err := zr.File[0].Open()
	if err != nil {
		t.Fatalf("open seed member: %v", err)
	}
	defer member.Close() //nolint:errcheck // read-only test fixture

	dst := filepath.Join(t.TempDir(), "mixed.echoreplay")
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create mixed replay: %v", err)
	}
	defer out.Close() //nolint:errcheck // closed explicitly below

	if _, err := io.Copy(out, member); err != nil { //nolint:gosec // fixed-size test fixture
		t.Fatalf("copy good lines: %v", err)
	}
	for range bad {
		// Not a timestamp, not tab-delimited JSON: parseFrameLine rejects it.
		if _, err := out.WriteString("this is not a frame line\r\n"); err != nil {
			t.Fatalf("append bad line: %v", err)
		}
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close mixed replay: %v", err)
	}

	return dst
}

func TestConvertReportsSkippedLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		good int
		bad  int
	}{
		{"some lines rejected", 4, 3},
		{"most lines rejected (GH #31 shape)", 1, 5},
		{"nothing rejected", 4, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := writeReplayWithBadLines(t, tt.good, tt.bad)
			dst := filepath.Join(t.TempDir(), "out.tape")

			result, err := ConvertFile(src, dst)
			if err != nil {
				t.Fatalf("ConvertFile: %v", err)
			}

			if result.FrameCount != uint32(tt.good) { //nolint:gosec // small test constant
				t.Errorf("FrameCount = %d, want %d", result.FrameCount, tt.good)
			}
			if result.SkippedLines != uint32(tt.bad) { //nolint:gosec // small test constant
				t.Errorf("SkippedLines = %d, want %d — unparseable input must not be silent",
					result.SkippedLines, tt.bad)
			}
		})
	}
}
