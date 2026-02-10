package events

import (
	"testing"

	apigame "github.com/echotools/nevr-common/v4/gen/go/apigame/v1"
	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
)

// ScoreboardSensor Tests

func TestScoreboardSensor_DetectsScoreChange(t *testing.T) {
	sensor := NewScoreboardSensor()

	// First frame: initial score
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			BluePoints:       0,
			OrangePoints:     0,
			BlueRoundScore:   0,
			OrangeRoundScore: 0,
			GameClockDisplay: "5:00",
		},
	}
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: blue scores
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			BluePoints:       2,
			OrangePoints:     0,
			BlueRoundScore:   0,
			OrangeRoundScore: 0,
			GameClockDisplay: "4:45",
		},
	}
	event = sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected ScoreboardUpdated event")
	}

	scoreboard := event.GetScoreboardUpdated()
	if scoreboard == nil {
		t.Fatalf("expected ScoreboardUpdated, got %T", event.Event)
	}

	if scoreboard.BluePoints != 2 {
		t.Errorf("expected BluePoints=2, got %d", scoreboard.BluePoints)
	}

	if scoreboard.OrangePoints != 0 {
		t.Errorf("expected OrangePoints=0, got %d", scoreboard.OrangePoints)
	}

	if scoreboard.GameClockDisplay != "4:45" {
		t.Errorf("expected GameClockDisplay=4:45, got %s", scoreboard.GameClockDisplay)
	}
}

func TestScoreboardSensor_DetectsRoundScoreChange(t *testing.T) {
	sensor := NewScoreboardSensor()

	// First frame
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			BlueRoundScore:   0,
			OrangeRoundScore: 0,
		},
	}
	sensor.AddFrame(frame1)

	// Second frame: round score changes
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			BlueRoundScore:   1,
			OrangeRoundScore: 0,
		},
	}
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected ScoreboardUpdated event")
	}

	scoreboard := event.GetScoreboardUpdated()
	if scoreboard.BlueRoundScore != 1 {
		t.Errorf("expected BlueRoundScore=1, got %d", scoreboard.BlueRoundScore)
	}
}

func TestScoreboardSensor_NoEventWhenUnchanged(t *testing.T) {
	sensor := NewScoreboardSensor()

	frame := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			BluePoints:       2,
			OrangePoints:     2,
			BlueRoundScore:   1,
			OrangeRoundScore: 1,
		},
	}

	sensor.AddFrame(frame)
	event := sensor.AddFrame(frame)

	if event != nil {
		t.Fatalf("expected no event when score unchanged, got %v", event)
	}
}

func TestScoreboardSensor_NilFrame(t *testing.T) {
	sensor := NewScoreboardSensor()
	event := sensor.AddFrame(nil)
	if event != nil {
		t.Fatalf("expected nil for nil frame, got %v", event)
	}
}

// GoalScoredSensor Tests

func TestGoalScoredSensor_DetectsGoal(t *testing.T) {
	sensor := NewGoalScoredSensor()

	// First frame: no last score
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{},
	}
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: goal scored
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			LastScore: &apigame.LastScore{
				PersonScored:   "Player1",
				DiscSpeed:      15.5,
				DistanceThrown: 8.2,
				PointAmount:    2,
				Team:           "blue",
				GoalType:       "normal",
			},
		},
	}
	event = sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected GoalScored event")
	}

	goal := event.GetGoalScored()
	if goal == nil {
		t.Fatalf("expected GoalScored, got %T", event.Event)
	}

	if goal.ScoreDetails.GetPersonScored() != "Player1" {
		t.Errorf("expected PersonScored=Player1, got %s", goal.ScoreDetails.GetPersonScored())
	}

	if goal.ScoreDetails.GetDiscSpeed() != 15.5 {
		t.Errorf("expected DiscSpeed=15.5, got %f", goal.ScoreDetails.GetDiscSpeed())
	}

	if goal.ScoreDetails.GetPointAmount() != 2 {
		t.Errorf("expected PointAmount=2, got %d", goal.ScoreDetails.GetPointAmount())
	}
}

func TestGoalScoredSensor_NoEventForSameGoal(t *testing.T) {
	sensor := NewGoalScoredSensor()

	lastScore := &apigame.LastScore{
		PersonScored:   "Player1",
		DiscSpeed:      15.5,
		DistanceThrown: 8.2,
		PointAmount:    2,
	}

	frame := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			LastScore: lastScore,
		},
	}

	// First time seeing this score
	event := sensor.AddFrame(frame)
	if event == nil {
		t.Fatal("expected GoalScored event on first occurrence")
	}

	// Same score again
	event = sensor.AddFrame(frame)
	if event != nil {
		t.Fatalf("expected no event for same goal, got %v", event)
	}
}

func TestGoalScoredSensor_DetectsNewGoal(t *testing.T) {
	sensor := NewGoalScoredSensor()

	// First goal
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			LastScore: &apigame.LastScore{
				PersonScored: "Player1",
				DiscSpeed:    15.5,
			},
		},
	}
	sensor.AddFrame(frame1)

	// Second goal (different person)
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			LastScore: &apigame.LastScore{
				PersonScored: "Player2",
				DiscSpeed:    18.0,
			},
		},
	}
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected GoalScored event for new goal")
	}

	goal := event.GetGoalScored()
	if goal.ScoreDetails.GetPersonScored() != "Player2" {
		t.Errorf("expected PersonScored=Player2, got %s", goal.ScoreDetails.GetPersonScored())
	}
}

// lastScoreEqual Tests

func TestLastScoreEqual_BothNil(t *testing.T) {
	if !lastScoreEqual(nil, nil) {
		t.Error("expected true for both nil")
	}
}

func TestLastScoreEqual_OneNil(t *testing.T) {
	score := &apigame.LastScore{PersonScored: "Player1"}
	if lastScoreEqual(nil, score) {
		t.Error("expected false when one is nil")
	}
	if lastScoreEqual(score, nil) {
		t.Error("expected false when one is nil")
	}
}

func TestLastScoreEqual_Equal(t *testing.T) {
	a := &apigame.LastScore{
		PersonScored:   "Player1",
		DiscSpeed:      15.5,
		DistanceThrown: 8.2,
		PointAmount:    2,
	}
	b := &apigame.LastScore{
		PersonScored:   "Player1",
		DiscSpeed:      15.5,
		DistanceThrown: 8.2,
		PointAmount:    2,
	}
	if !lastScoreEqual(a, b) {
		t.Error("expected true for equal scores")
	}
}

func TestLastScoreEqual_NotEqual(t *testing.T) {
	a := &apigame.LastScore{PersonScored: "Player1", DiscSpeed: 15.5}
	b := &apigame.LastScore{PersonScored: "Player2", DiscSpeed: 15.5}
	if lastScoreEqual(a, b) {
		t.Error("expected false for different scores")
	}
}
