package conversion

import (
	"path/filepath"
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/echotools/tape/pkg/codec"
)

// The engine reports jersey_number and level as 0 for roughly the first two
// frames after a player joins. Measured on a real capture: a player joining at
// frame 243 read (0,0) on frames 243-244 and (1,1) on the remaining 12,572.
// Latching the join frame captured 0/0 and replayed it for the whole session.
//
// PlayerInfoUpdated records the correction as a delta so BOTH the glitched
// frames and the settled ones round-trip exactly. A heuristic that discarded the
// join value could not: jersey 0 is legitimate.

// glitchedJoinReplay writes a capture where a player joins with (0,0) for
// glitchFrames frames, then settles to (jersey, level) for the rest.
func glitchedJoinReplay(t *testing.T, glitchFrames, settledFrames int, jersey, level int32) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "glitched.echoreplay")
	w, err := codec.NewEchoReplayWriter(path)
	if err != nil {
		t.Fatalf("NewEchoReplayWriter: %v", err)
	}

	base := time.Date(2026, 1, 19, 22, 50, 54, 0, time.UTC)
	write := func(i int, j, l int32) {
		frame := &telemetryv1.LobbySessionStateFrame{
			FrameIndex: uint32(i), //nolint:gosec // small test constant
			Timestamp:  timestamppb.New(base.Add(time.Duration(i) * 33 * time.Millisecond)),
			Session: &enginev1.SessionResponse{
				SessionId:  "5E668BB6-13D8-4000-B68C-D3D707F292B1",
				MapName:    "mpl_arena_a",
				MatchType:  "Echo_Arena_Private",
				GameStatus: "playing",
				Teams: []*enginev1.Team{{
					TeamName: "BLUE TEAM",
					Players: []*enginev1.TeamMember{{
						SlotNumber:   3,
						DisplayName:  "iluvfemboys",
						JerseyNumber: j,
						Level:        l,
						Head:         &enginev1.BodyPart{Position: []float64{1, 2, 3}},
					}},
				}},
			},
		}
		if err := w.WriteFrame(frame); err != nil {
			t.Fatalf("WriteFrame %d: %v", i, err)
		}
	}

	n := 0
	for range glitchFrames {
		write(n, 0, 0)
		n++
	}
	for range settledFrames {
		write(n, jersey, level)
		n++
	}
	if err := w.Close(); err != nil {
		t.Fatalf("EchoReplay.Close: %v", err)
	}
	return path
}

func TestGlitchedJoinRoundTrips(t *testing.T) {
	t.Parallel()

	const (
		glitch  = 2
		settled = 20
		jersey  = int32(1)
		level   = int32(1)
	)
	src := glitchedJoinReplay(t, glitch, settled, jersey, level)

	dir := t.TempDir()
	tape := filepath.Join(dir, "out.tape")
	recon := filepath.Join(dir, "recon.echoreplay")
	if _, err := ConvertFile(src, tape); err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}
	if _, err := ReconstructFile(tape, recon); err != nil {
		t.Fatalf("ReconstructFile: %v", err)
	}

	r, err := codec.NewEchoReplayReader(recon)
	if err != nil {
		t.Fatalf("open reconstruction: %v", err)
	}
	frames, err := r.ReadFrames()
	_ = r.Close()
	if err != nil {
		t.Fatalf("read reconstruction: %v", err)
	}
	if len(frames) != glitch+settled {
		t.Fatalf("frames = %d, want %d", len(frames), glitch+settled)
	}

	for i, f := range frames {
		var got *enginev1.TeamMember
		for _, team := range f.GetSession().GetTeams() {
			for _, m := range team.GetPlayers() {
				if m.GetSlotNumber() == 3 {
					got = m
				}
			}
		}
		if got == nil {
			t.Fatalf("frame %d: slot 3 missing from reconstruction", i)
		}
		wantJ, wantL := jersey, level
		if i < glitch {
			// The engine really did report 0/0 here; reproducing it is fidelity,
			// not a bug to paper over.
			wantJ, wantL = 0, 0
		}
		if got.GetJerseyNumber() != wantJ || got.GetLevel() != wantL {
			t.Errorf("frame %d: (jersey,level) = (%d,%d), want (%d,%d)",
				i, got.GetJerseyNumber(), got.GetLevel(), wantJ, wantL)
		}
	}
}
