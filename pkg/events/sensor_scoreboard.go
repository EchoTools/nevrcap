package events

import (
	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
)

// ScoreboardSensor detects scoreboard changes
type ScoreboardSensor struct {
	prevBluePoints       int32
	prevOrangePoints     int32
	prevBlueRoundScore   int32
	prevOrangeRoundScore int32
	initialized          bool
}

// NewScoreboardSensor creates a new ScoreboardSensor
func NewScoreboardSensor() *ScoreboardSensor {
	return &ScoreboardSensor{}
}

// AddFrame processes a frame and returns a ScoreboardUpdated event if detected
func (s *ScoreboardSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	session := frame.GetSession()
	bluePoints := session.GetBluePoints()
	orangePoints := session.GetOrangePoints()
	blueRound := session.GetBlueRoundScore()
	orangeRound := session.GetOrangeRoundScore()
	gameClock := session.GetGameClockDisplay()

	if !s.initialized {
		s.prevBluePoints = bluePoints
		s.prevOrangePoints = orangePoints
		s.prevBlueRoundScore = blueRound
		s.prevOrangeRoundScore = orangeRound
		s.initialized = true
		return nil
	}

	// Check if any score changed
	if bluePoints != s.prevBluePoints ||
		orangePoints != s.prevOrangePoints ||
		blueRound != s.prevBlueRoundScore ||
		orangeRound != s.prevOrangeRoundScore {

		s.prevBluePoints = bluePoints
		s.prevOrangePoints = orangePoints
		s.prevBlueRoundScore = blueRound
		s.prevOrangeRoundScore = orangeRound

		return &telemetry.LobbySessionEvent{
			Event: &telemetry.LobbySessionEvent_ScoreboardUpdated{
				ScoreboardUpdated: &telemetry.ScoreboardUpdated{
					BluePoints:       bluePoints,
					OrangePoints:     orangePoints,
					BlueRoundScore:   blueRound,
					OrangeRoundScore: orangeRound,
					GameClockDisplay: gameClock,
				},
			},
		}
	}

	return nil
}

// GoalScoredSensor detects when a goal is scored using LastScore data
type GoalScoredSensor struct {
	prevLastScore *apigame.LastScore
}

// NewGoalScoredSensor creates a new GoalScoredSensor
func NewGoalScoredSensor() *GoalScoredSensor {
	return &GoalScoredSensor{}
}

// AddFrame processes a frame and returns a GoalScored event if detected
func (s *GoalScoredSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	lastScore := frame.GetSession().GetLastScore()
	if lastScore == nil {
		s.prevLastScore = nil
		return nil
	}

	// Detect new goal by comparing with previous
	if s.prevLastScore == nil || !lastScoreEqual(s.prevLastScore, lastScore) {
		s.prevLastScore = lastScore
		return &telemetry.LobbySessionEvent{
			Event: &telemetry.LobbySessionEvent_GoalScored{
				GoalScored: &telemetry.GoalScored{
					ScoreDetails: lastScore,
				},
			},
		}
	}

	return nil
}

// lastScoreEqual compares two LastScore objects for equality
func lastScoreEqual(a, b *apigame.LastScore) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.GetPersonScored() == b.GetPersonScored() &&
		a.GetDiscSpeed() == b.GetDiscSpeed() &&
		a.GetDistanceThrown() == b.GetDistanceThrown() &&
		a.GetPointAmount() == b.GetPointAmount()
}
