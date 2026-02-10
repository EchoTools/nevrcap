package events

import (
	"testing"

	apigame "github.com/echotools/nevr-common/v4/gen/go/apigame/v1"
	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
)

// Helper to create a frame with game status and scores
func createGameStateFrame(status string, blueRound, orangeRound int32) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			GameStatus:       status,
			BlueRoundScore:   blueRound,
			OrangeRoundScore: orangeRound,
		},
	}
}

// RoundStartSensor Tests

func TestRoundStartSensor_DetectsRoundStart(t *testing.T) {
	sensor := NewRoundStartSensor()

	// First frame: in lobby or score state
	frame1 := createGameStateFrame("score", 0, 0)
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: round starts
	frame2 := createGameStateFrame(GameStatusPlaying, 0, 0)
	event = sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected RoundStarted event")
	}

	roundStarted := event.GetRoundStarted()
	if roundStarted == nil {
		t.Fatalf("expected RoundStarted, got %T", event.Event)
	}

	if roundStarted.RoundNumber != 1 {
		t.Errorf("expected RoundNumber=1, got %d", roundStarted.RoundNumber)
	}
}

func TestRoundStartSensor_CalculatesRoundNumber(t *testing.T) {
	sensor := NewRoundStartSensor()

	// After some rounds have been played
	frame1 := createGameStateFrame("score", 1, 1) // 2 rounds completed
	sensor.AddFrame(frame1)

	// New round starts
	frame2 := createGameStateFrame(GameStatusPlaying, 1, 1)
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected RoundStarted event")
	}

	roundStarted := event.GetRoundStarted()
	if roundStarted.RoundNumber != 3 {
		t.Errorf("expected RoundNumber=3, got %d", roundStarted.RoundNumber)
	}
}

func TestRoundStartSensor_NoEventIfAlreadyPlaying(t *testing.T) {
	sensor := NewRoundStartSensor()

	frame1 := createGameStateFrame(GameStatusPlaying, 0, 0)
	sensor.AddFrame(frame1)

	frame2 := createGameStateFrame(GameStatusPlaying, 0, 0)
	event := sensor.AddFrame(frame2)

	if event != nil {
		t.Fatalf("expected no event when already playing, got %v", event)
	}
}

// PauseSensor Tests

func TestPauseSensor_DetectsPause(t *testing.T) {
	sensor := NewPauseSensor()

	// First frame: not paused
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Pause: &apigame.PauseState{PausedState: "none"},
		},
	}
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: paused
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Pause: &apigame.PauseState{
				PausedState:         "paused",
				PausedRequestedTeam: "blue",
			},
		},
	}
	event = sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected RoundPaused event")
	}

	paused := event.GetRoundPaused()
	if paused == nil {
		t.Fatalf("expected RoundPaused, got %T", event.Event)
	}
}

func TestPauseSensor_DetectsUnpause(t *testing.T) {
	sensor := NewPauseSensor()

	// First frame: paused
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Pause: &apigame.PauseState{PausedState: "paused"},
		},
	}
	sensor.AddFrame(frame1)

	// Second frame: unpaused
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Pause: &apigame.PauseState{PausedState: "none"},
		},
	}
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected RoundUnpaused event")
	}

	unpaused := event.GetRoundUnpaused()
	if unpaused == nil {
		t.Fatalf("expected RoundUnpaused, got %T", event.Event)
	}
}

func TestPauseSensor_NilPauseState(t *testing.T) {
	sensor := NewPauseSensor()

	frame := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Pause: nil,
		},
	}
	event := sensor.AddFrame(frame)

	// Should handle nil pause gracefully
	if event != nil {
		t.Fatalf("expected no event for nil pause, got %v", event)
	}
}

// RoundEndSensor Tests

func TestRoundEndSensor_DetectsRoundEnd(t *testing.T) {
	sensor := NewRoundEndSensor()

	// First frame: playing
	frame1 := createGameStateFrame(GameStatusPlaying, 0, 0)
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: round over
	frame2 := createGameStateFrame(GameStatusRoundOver, 1, 0)
	event = sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected RoundEnded event")
	}

	roundEnded := event.GetRoundEnded()
	if roundEnded == nil {
		t.Fatalf("expected RoundEnded, got %T", event.Event)
	}

	if roundEnded.WinningTeam != telemetry.Role_ROLE_BLUE_TEAM {
		t.Errorf("expected BLUE_TEAM winner, got %v", roundEnded.WinningTeam)
	}
}

func TestRoundEndSensor_DetectsRoundEndByScoreChange(t *testing.T) {
	sensor := NewRoundEndSensor()

	// First frame: playing
	frame1 := createGameStateFrame(GameStatusPlaying, 0, 0)
	sensor.AddFrame(frame1)

	// Second frame: still playing but score changed (blue won round)
	frame2 := createGameStateFrame(GameStatusPlaying, 1, 0)
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected RoundEnded event on score change")
	}

	roundEnded := event.GetRoundEnded()
	if roundEnded.WinningTeam != telemetry.Role_ROLE_BLUE_TEAM {
		t.Errorf("expected BLUE_TEAM winner, got %v", roundEnded.WinningTeam)
	}

	if roundEnded.RoundNumber != 1 {
		t.Errorf("expected RoundNumber=1, got %d", roundEnded.RoundNumber)
	}
}

func TestRoundEndSensor_OrangeWins(t *testing.T) {
	sensor := NewRoundEndSensor()

	frame1 := createGameStateFrame(GameStatusPlaying, 0, 0)
	sensor.AddFrame(frame1)

	frame2 := createGameStateFrame(GameStatusRoundOver, 0, 1)
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected RoundEnded event")
	}

	roundEnded := event.GetRoundEnded()
	if roundEnded.WinningTeam != telemetry.Role_ROLE_ORANGE_TEAM {
		t.Errorf("expected ORANGE_TEAM winner, got %v", roundEnded.WinningTeam)
	}
}

// MatchEndSensor Tests

func TestMatchEndSensor_DetectsMatchEnd(t *testing.T) {
	sensor := NewMatchEndSensor()

	// First frame: playing
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			GameStatus:   GameStatusPlaying,
			BluePoints:   10,
			OrangePoints: 8,
		},
	}
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: post match
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			GameStatus:   GameStatusPostMatch,
			BluePoints:   10,
			OrangePoints: 8,
		},
	}
	event = sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected MatchEnded event")
	}

	matchEnded := event.GetMatchEnded()
	if matchEnded == nil {
		t.Fatalf("expected MatchEnded, got %T", event.Event)
	}

	if matchEnded.WinningTeam != telemetry.Role_ROLE_BLUE_TEAM {
		t.Errorf("expected BLUE_TEAM winner, got %v", matchEnded.WinningTeam)
	}
}

func TestMatchEndSensor_OrangeWins(t *testing.T) {
	sensor := NewMatchEndSensor()

	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			GameStatus:   GameStatusPlaying,
			BluePoints:   5,
			OrangePoints: 12,
		},
	}
	sensor.AddFrame(frame1)

	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			GameStatus:   GameStatusPostMatch,
			BluePoints:   5,
			OrangePoints: 12,
		},
	}
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected MatchEnded event")
	}

	matchEnded := event.GetMatchEnded()
	if matchEnded.WinningTeam != telemetry.Role_ROLE_ORANGE_TEAM {
		t.Errorf("expected ORANGE_TEAM winner, got %v", matchEnded.WinningTeam)
	}
}

func TestMatchEndSensor_TiedMatch(t *testing.T) {
	sensor := NewMatchEndSensor()

	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			GameStatus:   GameStatusPlaying,
			BluePoints:   8,
			OrangePoints: 8,
		},
	}
	sensor.AddFrame(frame1)

	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			GameStatus:   GameStatusPostMatch,
			BluePoints:   8,
			OrangePoints: 8,
		},
	}
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected MatchEnded event")
	}

	matchEnded := event.GetMatchEnded()
	if matchEnded.WinningTeam != telemetry.Role_ROLE_UNSPECIFIED {
		t.Errorf("expected UNSPECIFIED for tie, got %v", matchEnded.WinningTeam)
	}
}

func TestMatchEndSensor_NoEventIfNotPostMatch(t *testing.T) {
	sensor := NewMatchEndSensor()

	frame1 := createGameStateFrame(GameStatusPlaying, 0, 0)
	sensor.AddFrame(frame1)

	frame2 := createGameStateFrame(GameStatusRoundOver, 0, 0)
	event := sensor.AddFrame(frame2)

	if event != nil {
		t.Fatalf("expected no event for non-post_match status, got %v", event)
	}
}

// isPausedState Tests

func TestIsPausedState(t *testing.T) {
	tests := []struct {
		state    string
		expected bool
	}{
		{"paused", true},
		{GameStatusPaused, true},
		{"paused_requested", true},
		{"none", false},
		{"", false},
		{"playing", false},
		{"unpausing", false},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			result := isPausedState(tt.state)
			if result != tt.expected {
				t.Errorf("isPausedState(%q) = %v, want %v", tt.state, result, tt.expected)
			}
		})
	}
}
