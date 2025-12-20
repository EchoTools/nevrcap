package events

import (
	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
)

// Game status constants
const (
	GameStatusPlaying    = "playing"
	GameStatusRoundStart = "round_start"
	GameStatusScore      = "score"
	GameStatusPaused     = "paused"
	GameStatusUnpausing  = "unpausing"
)

// RoundStartSensor detects when a round starts
type RoundStartSensor struct {
	prevGameStatus string
	roundNumber    int32
}

// NewRoundStartSensor creates a new RoundStartSensor
func NewRoundStartSensor() *RoundStartSensor {
	return &RoundStartSensor{}
}

// AddFrame processes a frame and returns a RoundStarted event if detected
func (s *RoundStartSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	currentStatus := frame.GetSession().GetGameStatus()

	// Detect transition to round_start or playing from a non-playing state
	if (currentStatus == GameStatusRoundStart || currentStatus == GameStatusPlaying) &&
		s.prevGameStatus != GameStatusPlaying && s.prevGameStatus != GameStatusRoundStart &&
		s.prevGameStatus != "" {

		// Calculate round number from round scores
		session := frame.GetSession()
		s.roundNumber = session.GetBlueRoundScore() + session.GetOrangeRoundScore() + 1

		s.prevGameStatus = currentStatus
		return &telemetry.LobbySessionEvent{
			Event: &telemetry.LobbySessionEvent_RoundStarted{
				RoundStarted: &telemetry.RoundStarted{
					RoundNumber: s.roundNumber,
				},
			},
		}
	}

	s.prevGameStatus = currentStatus
	return nil
}

// PauseSensor detects pause/unpause events
type PauseSensor struct {
	prevPauseState string
}

// NewPauseSensor creates a new PauseSensor
func NewPauseSensor() *PauseSensor {
	return &PauseSensor{}
}

// AddFrame processes a frame and returns pause-related events
func (s *PauseSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	pause := frame.GetSession().GetPause()
	if pause == nil {
		s.prevPauseState = ""
		return nil
	}

	currentState := pause.GetPausedState()

	// Detect transitions
	if currentState != s.prevPauseState {
		defer func() { s.prevPauseState = currentState }()

		// Transition to paused state
		if isPausedState(currentState) && !isPausedState(s.prevPauseState) {
			return &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_RoundPaused{
					RoundPaused: &telemetry.RoundPaused{
						PauseState: pause,
					},
				},
			}
		}

		// Transition from paused to unpaused
		if !isPausedState(currentState) && isPausedState(s.prevPauseState) {
			return &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_RoundUnpaused{
					RoundUnpaused: &telemetry.RoundUnpaused{
						PauseState: pause,
					},
				},
			}
		}
	}

	s.prevPauseState = currentState
	return nil
}

// isPausedState checks if the given state represents a paused game
func isPausedState(state string) bool {
	return state == GameStatusPaused || state == "paused" || state == "paused_requested"
}

// RoundEndSensor detects when a round ends (separate from match end)
type RoundEndSensor struct {
	prevGameStatus       string
	prevBlueRoundScore   int32
	prevOrangeRoundScore int32
	initialized          bool
}

// NewRoundEndSensor creates a new RoundEndSensor
func NewRoundEndSensor() *RoundEndSensor {
	return &RoundEndSensor{}
}

// AddFrame processes a frame and returns a RoundEnded event if detected
func (s *RoundEndSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	session := frame.GetSession()
	currentStatus := session.GetGameStatus()
	blueRound := session.GetBlueRoundScore()
	orangeRound := session.GetOrangeRoundScore()

	if !s.initialized {
		s.prevGameStatus = currentStatus
		s.prevBlueRoundScore = blueRound
		s.prevOrangeRoundScore = orangeRound
		s.initialized = true
		return nil
	}

	// Detect round end by transition to "round_over" or "score" status,
	// OR by a change in round scores
	roundScoreChanged := blueRound != s.prevBlueRoundScore || orangeRound != s.prevOrangeRoundScore
	statusTransitionToRoundOver := currentStatus == GameStatusRoundOver && s.prevGameStatus != GameStatusRoundOver

	if statusTransitionToRoundOver || (roundScoreChanged && s.prevGameStatus == GameStatusPlaying) {
		roundNumber := s.prevBlueRoundScore + s.prevOrangeRoundScore + 1
		var winningTeam telemetry.Role

		if blueRound > s.prevBlueRoundScore {
			winningTeam = telemetry.Role_ROLE_BLUE_TEAM
		} else if orangeRound > s.prevOrangeRoundScore {
			winningTeam = telemetry.Role_ROLE_ORANGE_TEAM
		}

		s.prevGameStatus = currentStatus
		s.prevBlueRoundScore = blueRound
		s.prevOrangeRoundScore = orangeRound

		return &telemetry.LobbySessionEvent{
			Event: &telemetry.LobbySessionEvent_RoundEnded{
				RoundEnded: &telemetry.RoundEnded{
					RoundNumber: roundNumber,
					WinningTeam: winningTeam,
				},
			},
		}
	}

	s.prevGameStatus = currentStatus
	s.prevBlueRoundScore = blueRound
	s.prevOrangeRoundScore = orangeRound
	return nil
}

// MatchEndSensor detects when a match ends
type MatchEndSensor struct {
	prevGameStatus string
}

// NewMatchEndSensor creates a new MatchEndSensor
func NewMatchEndSensor() *MatchEndSensor {
	return &MatchEndSensor{}
}

// AddFrame processes a frame and returns a MatchEnded event if detected
func (s *MatchEndSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	session := frame.GetSession()
	currentStatus := session.GetGameStatus()

	// Detect transition to post_match
	if currentStatus == GameStatusPostMatch && s.prevGameStatus != GameStatusPostMatch {
		var winningTeam telemetry.Role
		if session.GetBluePoints() > session.GetOrangePoints() {
			winningTeam = telemetry.Role_ROLE_BLUE_TEAM
		} else if session.GetOrangePoints() > session.GetBluePoints() {
			winningTeam = telemetry.Role_ROLE_ORANGE_TEAM
		}
		// If tied, leave as ROLE_UNSPECIFIED

		s.prevGameStatus = currentStatus
		return &telemetry.LobbySessionEvent{
			Event: &telemetry.LobbySessionEvent_MatchEnded{
				MatchEnded: &telemetry.MatchEnded{
					WinningTeam: winningTeam,
				},
			},
		}
	}

	s.prevGameStatus = currentStatus
	return nil
}
