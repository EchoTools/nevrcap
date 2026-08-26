package codec

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// FuzzTapeContainer exercises the full .tape read path (zstd container) with
// arbitrary bytes. The reader must never panic. SEC-001/002 limits are applied
// so a highly-compressible input cannot force unbounded decoded output (GH #42).
func FuzzTapeContainer(f *testing.F) {
	// Seed: a minimal valid .tape (header + footer).
	f.Add(validTapeBytes(f))

	// Seed: empty file.
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "fuzz.tape")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Skipf("write seed: %v", err)
		}

		r, err := NewReader(path)
		if err != nil {
			// Unreadable files (bad zstd, empty, etc.) are non-panics.
			return
		}
		defer r.Close() //nolint:errcheck // best-effort cleanup

		if _, err := r.ReadHeader(); err != nil {
			return
		}
		// Drain frames until EOF/error.
		for {
			if _, err := r.ReadFrame(); err != nil {
				break
			}
		}
	})
}

// FuzzEchoReplayContainer exercises the full .echoreplay read path (zip
// container) with arbitrary bytes. The reader must never panic. (GH #42)
func FuzzEchoReplayContainer(f *testing.F) {
	// Seed: a minimal valid .echoreplay (zip with one line).
	f.Add(validEchoReplayBytes(f))

	// Seed: empty file.
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "fuzz.echoreplay")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Skipf("write seed: %v", err)
		}

		r, err := NewEchoReplayReader(path)
		if err != nil {
			return
		}
		defer r.Close() //nolint:errcheck // best-effort cleanup

		// Drain frames until EOF/error.
		for {
			if _, err := r.ReadFrame(); err != nil {
				break
			}
		}
	})
}

// validTapeBytes returns a minimal valid .tape file (zstd-compressed envelope
// stream: header + footer) as a seed for the container fuzzer.
func validTapeBytes(f *testing.F) []byte {
	path := filepath.Join(f.TempDir(), "seed.tape")
	w, err := NewWriter(path)
	if err != nil {
		f.Fatalf("seed writer: %v", err)
	}
	if err := w.WriteHeader(&capturepb.CaptureHeader{}); err != nil {
		f.Fatalf("seed header: %v", err)
	}
	if err := w.Close(); err != nil {
		f.Fatalf("seed close: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		f.Fatalf("seed read: %v", err)
	}
	return data
}

// validEchoReplayBytes returns a minimal valid .echoreplay file (zip containing
// one frame line) as a seed for the container fuzzer.
func validEchoReplayBytes(f *testing.F) []byte {
	path := filepath.Join(f.TempDir(), "seed.echoreplay")
	w, err := NewEchoReplayWriter(path)
	if err != nil {
		f.Fatalf("seed writer: %v", err)
	}
	if err := w.WriteFrame(&telemetryv1.LobbySessionStateFrame{
		Timestamp: timestamppb.New(time.Date(2025, 3, 15, 14, 30, 0, 0, time.UTC)),
		Session: &enginev1.SessionResponse{
			SessionId:  "seed",
			GameStatus: "playing",
			GameClock:  1.0,
			MapName:    "mpl_arena_a",
			MatchType:  "Echo_Arena",
			Teams: []*enginev1.Team{{
				Players: []*enginev1.TeamMember{{
					SlotNumber:  0,
					DisplayName: "TestPlayer",
					Head:        &enginev1.BodyPart{Position: []float64{1, 2, 3}},
				}},
			}},
		},
	}); err != nil {
		f.Fatalf("seed frame: %v", err)
	}
	if err := w.Finalize(); err != nil {
		f.Fatalf("seed close: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		f.Fatalf("seed read: %v", err)
	}
	return data
}
