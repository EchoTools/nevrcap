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

// TestReconstructPreservesInitialRoundScores covers the frame-0 seed bug named
// in BUGS.md: ScoreboardSensor.AddFrame records the first frame's scores and
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
