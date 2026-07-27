package events

import (
	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"google.golang.org/protobuf/proto"
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

	// Seed on the first frame. Without this the opening scoreboard is never
	// emitted, so a capture that starts mid-match reconstructs as 0-0 until the
	// next goal — the state before the first change is otherwise unrecoverable.
	seeding := !s.initialized

	// Check if any score changed
	if seeding ||
		bluePoints != s.prevBluePoints ||
		orangePoints != s.prevOrangePoints ||
		blueRound != s.prevBlueRoundScore ||
		orangeRound != s.prevOrangeRoundScore {

		s.initialized = true
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

// Reset clears internal state for a new session.
func (s *ScoreboardSensor) Reset() {
	s.prevBluePoints = 0
	s.prevOrangePoints = 0
	s.prevBlueRoundScore = 0
	s.prevOrangeRoundScore = 0
	s.initialized = false
}

// GoalScoredSensor detects when a goal is scored using LastScore data
type GoalScoredSensor struct {
	prevLastScore *enginev1.LastScore
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
		s.prevLastScore = proto.Clone(lastScore).(*enginev1.LastScore)
		return &telemetry.LobbySessionEvent{
			Event: &telemetry.LobbySessionEvent_GoalScored{
				GoalScored: &telemetry.GoalScored{
					ScoreDetails: proto.Clone(lastScore).(*enginev1.LastScore),
				},
			},
		}
	}

	return nil
}

// Reset clears internal state for a new session.
func (s *GoalScoredSensor) Reset() {
	s.prevLastScore = nil
}

// lastScoreEqual compares two LastScore objects for equality using
// proto.Equal to ensure all 7 fields are compared.
func lastScoreEqual(a, b *enginev1.LastScore) bool {
	return proto.Equal(a, b)
}
