package conversion

import (
	"path/filepath"
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/echotools/tape/v4/pkg/codec"
)

// The v2 schema carries client_name (EchoArenaHeader) and the five payload
// fields (EchoArenaFrame.payload), and MapFrame/MapHeaderFromSession populate
// both. Neither was read back: reconstructSession never assigned them, so they
// vanished on echoreplay -> v2 -> echoreplay. The committed sample is an arena
// match, where mapping.go only emits PayloadState when a payload value is
// non-zero, so nothing exercised the payload path at all.

// payloadSession builds a SessionResponse that populates every field this test
// cares about, including combat payload values absent from any arena capture.
func payloadSession() *enginev1.SessionResponse {
	return &enginev1.SessionResponse{
		SessionId:         "1E668BB6-13D8-4000-B68C-D3D707F292B1",
		SessionIp:         "127.0.0.1",
		ClientName:        "Milkyway",
		MapName:           "mpl_combat_dyson",
		MatchType:         "Echo_Combat",
		GameStatus:        "playing",
		GameClock:         120.5,
		TotalRoundCount:   3,
		PayloadMultiplier: 1.5,
		PayloadCheckpoint: 2,
		PayloadDistance:   42.25,
		PayloadDefenders:  3,
		PayloadSpeed:      0.75,
		Teams: []*enginev1.Team{{
			Players: []*enginev1.TeamMember{{
				SlotNumber:   0,
				DisplayName:  "Milkyway",
				JerseyNumber: 7,
				Head:         &enginev1.BodyPart{Position: []float64{1, 2, 3}},
			}},
		}},
	}
}

// writeSyntheticReplay writes a one-frame echoreplay carrying session and
// returns its path.
func writeSyntheticReplay(t *testing.T, session *enginev1.SessionResponse) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "synthetic.echoreplay")
	w, err := codec.NewEchoReplayWriter(path)
	if err != nil {
		t.Fatalf("NewEchoReplayWriter: %v", err)
	}

	frame := &telemetryv1.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(time.Date(2026, 5, 1, 14, 27, 52, 0, time.UTC)),
		Session:    session,
	}
	if err := w.WriteFrame(frame); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("EchoReplay.Close: %v", err)
	}

	return path
}

// roundTripSession runs echoreplay -> v2 -> echoreplay and returns the
// reconstructed first-frame session.
func roundTripSession(t *testing.T, session *enginev1.SessionResponse) *enginev1.SessionResponse {
	t.Helper()

	src := writeSyntheticReplay(t, session)
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
	if len(frames) != 1 {
		t.Fatalf("reconstructed %d frames, want 1", len(frames))
	}

	return frames[0].GetSession()
}

// supersetSession carries every field the 2.1.0 proto additions gave a v2 home,
// with values chosen so a dropped field shows up as a zero rather than a
// coincidental match.
func supersetSession() *enginev1.SessionResponse {
	stats := func(base int32) *enginev1.PlayerStats {
		return &enginev1.PlayerStats{
			PossessionTime: float64(base) + 0.5,
			Points:         base + 1,
			Saves:          base + 2,
			Goals:          base + 3,
			Stuns:          base + 4,
			Passes:         base + 5,
			Catches:        base + 6,
			Steals:         base + 7,
			Blocks:         base + 8,
			Interceptions:  base + 9,
			Assists:        base + 10,
			ShotsTaken:     base + 11,
		}
	}
	return &enginev1.SessionResponse{
		SessionId:                "3E668BB6-13D8-4000-B68C-D3D707F292B1",
		MapName:                  "mpl_arena_a",
		MatchType:                "Echo_Arena_Tournament",
		GameStatus:               "playing",
		ClientName:               "Milkyway",
		GameClock:                662.80414,
		RulesChangedBy:           "mikey",
		RulesChangedAt:           573360097,
		ErrCode:                  7,
		BlueTeamRestartRequest:   1,
		OrangeTeamRestartRequest: 2,
		Pause: &enginev1.PauseState{
			PausedState:         "paused_requested",
			UnpausedTeam:        "blue",
			PausedRequestedTeam: "orange",
			UnpausedTimer:       1.5,
			PausedTimer:         2.5,
		},
		Teams: []*enginev1.Team{
			{
				TeamName: "Raptured and Friend",
				Stats:    &enginev1.TeamStats{},
				Players: []*enginev1.TeamMember{{
					SlotNumber: 0, DisplayName: "Milkyway", Stats: stats(0),
					Head: &enginev1.BodyPart{Position: []float64{1, 2, 3}},
				}},
			},
			{
				TeamName: "ORANGE TEAM",
				Stats:    &enginev1.TeamStats{},
				Players: []*enginev1.TeamMember{{
					SlotNumber: 1, DisplayName: "sprockee", Stats: stats(20),
					Head: &enginev1.BodyPart{Position: []float64{4, 5, 6}},
				}},
			},
		},
	}
}

func TestReconstructPreservesSupersetFields(t *testing.T) {
	t.Parallel()

	got := roundTripSession(t, supersetSession())

	t.Run("header scalars", func(t *testing.T) {
		if got.GetRulesChangedBy() != "mikey" {
			t.Errorf("rules_changed_by = %q, want %q", got.GetRulesChangedBy(), "mikey")
		}
		if got.GetRulesChangedAt() != 573360097 {
			t.Errorf("rules_changed_at = %d, want 573360097", got.GetRulesChangedAt())
		}
	})

	t.Run("per-frame scalars", func(t *testing.T) {
		if got.GetErrCode() != 7 {
			t.Errorf("err_code = %d, want 7", got.GetErrCode())
		}
		if got.GetBlueTeamRestartRequest() != 1 {
			t.Errorf("blue_team_restart_request = %d, want 1", got.GetBlueTeamRestartRequest())
		}
		if got.GetOrangeTeamRestartRequest() != 2 {
			t.Errorf("orange_team_restart_request = %d, want 2", got.GetOrangeTeamRestartRequest())
		}
	})

	t.Run("pause sub-state", func(t *testing.T) {
		p := got.GetPause()
		if p.GetPausedState() != "paused_requested" {
			t.Errorf("paused_state = %q, want %q", p.GetPausedState(), "paused_requested")
		}
		if p.GetUnpausedTeam() != "blue" {
			t.Errorf("unpaused_team = %q, want %q", p.GetUnpausedTeam(), "blue")
		}
		if p.GetPausedRequestedTeam() != "orange" {
			t.Errorf("paused_requested_team = %q, want %q", p.GetPausedRequestedTeam(), "orange")
		}
		if p.GetUnpausedTimer() != 1.5 {
			t.Errorf("unpaused_timer = %v, want 1.5", p.GetUnpausedTimer())
		}
		if p.GetPausedTimer() != 2.5 {
			t.Errorf("paused_timer = %v, want 2.5", p.GetPausedTimer())
		}
	})

	t.Run("team names", func(t *testing.T) {
		var names []string
		for _, team := range got.GetTeams() {
			names = append(names, team.GetTeamName())
		}
		want := []string{"Raptured and Friend", "ORANGE TEAM"}
		if len(names) != len(want) {
			t.Fatalf("teams = %v, want %v", names, want)
		}
		for i := range want {
			if names[i] != want[i] {
				t.Errorf("team[%d].team_name = %q, want %q", i, names[i], want[i])
			}
		}
	})

	t.Run("player stats", func(t *testing.T) {
		for _, team := range got.GetTeams() {
			for _, m := range team.GetPlayers() {
				st := m.GetStats()
				if st == nil {
					t.Fatalf("slot %d: stats dropped", m.GetSlotNumber())
				}
				base := int32(0)
				if m.GetSlotNumber() == 1 {
					base = 20
				}
				if st.GetStuns() != base+4 {
					t.Errorf("slot %d stuns = %d, want %d", m.GetSlotNumber(), st.GetStuns(), base+4)
				}
				if st.GetShotsTaken() != base+11 {
					t.Errorf("slot %d shots_taken = %d, want %d", m.GetSlotNumber(), st.GetShotsTaken(), base+11)
				}
				if diff := st.GetPossessionTime() - (float64(base) + 0.5); diff > 1e-3 || diff < -1e-3 {
					t.Errorf("slot %d possession_time = %v, want %v", m.GetSlotNumber(), st.GetPossessionTime(), float64(base)+0.5)
				}
			}
		}
	})

	t.Run("team stats are the sum of player stats", func(t *testing.T) {
		for _, team := range got.GetTeams() {
			ts := team.GetStats()
			if ts == nil {
				t.Fatalf("team %q: stats dropped", team.GetTeamName())
			}
			var wantStuns int32
			for _, m := range team.GetPlayers() {
				wantStuns += m.GetStats().GetStuns()
			}
			if ts.GetStuns() != wantStuns {
				t.Errorf("team %q stuns = %d, want %d", team.GetTeamName(), ts.GetStuns(), wantStuns)
			}
		}
	})
}

// TestReconstructPreservesInitialRoundScores covers the frame-0 seed bug:
// ScoreboardSensor.AddFrame records the first frame's scores and
// returns nil (sensor_scoreboard.go:36-43), so a capture that starts mid-match
// with a non-zero round score has no ScoreboardUpdated to replay and
// reconstructs as 0-0 until the next goal.
func TestReconstructPreservesInitialRoundScores(t *testing.T) {
	t.Parallel()

	session := payloadSession()
	session.BlueRoundScore = 2
	session.OrangeRoundScore = 1
	session.BluePoints = 15
	session.OrangePoints = 10

	got := roundTripSession(t, session)

	if got.GetBlueRoundScore() != 2 {
		t.Errorf("blue_round_score = %d, want 2", got.GetBlueRoundScore())
	}
	if got.GetOrangeRoundScore() != 1 {
		t.Errorf("orange_round_score = %d, want 1", got.GetOrangeRoundScore())
	}
}

func TestReconstructPreservesClientName(t *testing.T) {
	t.Parallel()

	got := roundTripSession(t, payloadSession())
	if got.GetClientName() != "Milkyway" {
		t.Errorf("client_name = %q, want %q", got.GetClientName(), "Milkyway")
	}
}

func TestReconstructPreservesPayload(t *testing.T) {
	t.Parallel()

	got := roundTripSession(t, payloadSession())

	tests := []struct {
		field string
		got   float64
		want  float64
	}{
		{"payload_multiplier", got.GetPayloadMultiplier(), 1.5},
		{"payload_checkpoint", float64(got.GetPayloadCheckpoint()), 2},
		{"payload_distance", got.GetPayloadDistance(), 42.25},
		{"payload_defenders", float64(got.GetPayloadDefenders()), 3},
		{"payload_speed", got.GetPayloadSpeed(), 0.75},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %v, want %v", tt.field, tt.got, tt.want)
		}
	}
}
