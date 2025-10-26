package nevrcap

import (
	"testing"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestEventDetector_NewEventDetector(t *testing.T) {
	detector := NewEventDetector()

	if detector == nil {
		t.Fatal("NewEventDetector returned nil")
	}

	if detector.prevPlayersBySlot == nil {
		t.Error("prevPlayersBySlot map not initialized")
	}

	if len(detector.prevPlayersBySlot) != 0 {
		t.Error("prevPlayersBySlot should be empty initially")
	}
}

func TestEventDetector_Reset(t *testing.T) {
	detector := NewEventDetector()

	// Add some state
	detector.prevPlayersBySlot[1] = &apigame.TeamMember{SlotNumber: 1}
	detector.prevScoreboard = &ScoreboardState{BluePoints: 1}
	detector.prevDiscState = &DiscState{HasPossession: true}

	detector.Reset()

	if len(detector.prevPlayersBySlot) != 0 {
		t.Error("prevPlayersBySlot should be empty after reset")
	}

	if detector.prevScoreboard != nil {
		t.Error("prevScoreboard should be nil after reset")
	}

	if detector.prevDiscState != nil {
		t.Error("prevDiscState should be nil after reset")
	}
}

func TestEventDetector_PlayerJoinEvents(t *testing.T) {
	detector := NewEventDetector()

	// First frame - no players
	frame1 := createEmptyFrame()

	// Second frame - one player joins
	frame2 := createFrameWithPlayers([]*apigame.TeamMember{
		{SlotNumber: 1, DisplayName: "Player1", JerseyNumber: 0},
	})

	events := detector.DetectEvents(frame1, frame2)

	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}

	playerJoined := events[0].GetPlayerJoined()
	if playerJoined == nil {
		t.Fatal("Expected PlayerJoined event")
	}

	if playerJoined.Player.SlotNumber != 1 {
		t.Errorf("Expected slot 1, got %d", playerJoined.Player.SlotNumber)
	}

	if playerJoined.Player.DisplayName != "Player1" {
		t.Errorf("Expected Player1, got %s", playerJoined.Player.DisplayName)
	}
}

func TestEventDetector_PlayerLeaveEvents(t *testing.T) {
	detector := NewEventDetector()

	// First frame - one player
	frame1 := createFrameWithPlayers([]*apigame.TeamMember{
		{SlotNumber: 1, DisplayName: "Player1", JerseyNumber: 0},
	})

	// Second frame - player leaves
	frame2 := createEmptyFrame()

	// Process first frame to set up state
	detector.DetectEvents(nil, frame1)

	// Process second frame to detect leave event
	events := detector.DetectEvents(frame1, frame2)

	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}

	playerLeft := events[0].GetPlayerLeft()
	if playerLeft == nil {
		t.Fatal("Expected PlayerLeft event")
	}

	if playerLeft.PlayerSlot != 1 {
		t.Errorf("Expected slot 1, got %d", playerLeft.PlayerSlot)
	}

	if playerLeft.DisplayName != "Player1" {
		t.Errorf("Expected Player1, got %s", playerLeft.DisplayName)
	}
}

func TestEventDetector_ScoreboardEvents(t *testing.T) {
	detector := NewEventDetector()

	// First frame - initial score
	frame1 := createFrameWithScore(0, 0, 0, 0)

	// Second frame - blue team scores
	frame2 := createFrameWithScore(1, 0, 1, 0)

	events := detector.DetectEvents(frame1, frame2)

	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}

	scoreUpdate := events[0].GetScoreboardUpdated()
	if scoreUpdate == nil {
		t.Fatal("Expected ScoreboardUpdated event")
	}

	if scoreUpdate.BluePoints != 1 {
		t.Errorf("Expected blue points 1, got %d", scoreUpdate.BluePoints)
	}

	if scoreUpdate.OrangePoints != 0 {
		t.Errorf("Expected orange points 0, got %d", scoreUpdate.OrangePoints)
	}
}

func TestEventDetector_GoalScoredEvent(t *testing.T) {
	detector := NewEventDetector()

	frame1 := createFrameWithScore(0, 0, 0, 0)
	frame2 := createFrameWithScore(1, 0, 1, 0)

	// Add last score info to frame2
	frame2.Session.LastScore = &apigame.LastScore{
		DiscSpeed:      15.5,
		DistanceThrown: 8.2,
		PersonScored:   "Player1",
	}

	events := detector.DetectEvents(frame1, frame2)

	// Should have both scoreboard update and goal scored events
	var goalEvent *rtapi.GoalScored
	for _, event := range events {
		if gs := event.GetGoalScored(); gs != nil {
			goalEvent = gs
			break
		}
	}

	if goalEvent == nil {
		t.Fatal("Expected GoalScored event")
	}

	if goalEvent.ScoreDetails.DiscSpeed != 15.5 {
		t.Errorf("Expected disc speed 15.5, got %f", goalEvent.ScoreDetails.DiscSpeed)
	}
}

func TestEventDetector_DiscPossessionChange(t *testing.T) {
	detector := NewEventDetector()

	// First frame - player 1 has possession
	frame1 := createFrameWithPossession(1)

	// Second frame - player 2 has possession
	frame2 := createFrameWithPossession(2)

	events := detector.DetectEvents(frame1, frame2)

	var possessionEvent *rtapi.DiscPossessionChanged
	for _, event := range events {
		if pe := event.GetDiscPossessionChanged(); pe != nil {
			possessionEvent = pe
			break
		}
	}

	if possessionEvent == nil {
		t.Fatal("Expected DiscPossessionChanged event")
	}

	if possessionEvent.PlayerSlot != 2 {
		t.Errorf("Expected current slot 2, got %d", possessionEvent.PlayerSlot)
	}

	if possessionEvent.PreviousSlot != 1 {
		t.Errorf("Expected previous slot 1, got %d", possessionEvent.PreviousSlot)
	}
}

func TestEventDetector_DiscThrown(t *testing.T) {
	detector := NewEventDetector()

	frame1 := createFrameWithPossession(1)
	frame2 := createFrameWithPossession(1)

	// Add last throw info
	frame2.Session.LastThrow = &apigame.LastThrowInfo{
		ArmSpeed:   12.3,
		TotalSpeed: 15.1,
	}

	events := detector.DetectEvents(frame1, frame2)

	var throwEvent *rtapi.DiscThrown
	for _, event := range events {
		if te := event.GetDiscThrown(); te != nil {
			throwEvent = te
			break
		}
	}

	if throwEvent == nil {
		t.Fatal("Expected DiscThrown event")
	}

	if throwEvent.PlayerSlot != 1 {
		t.Errorf("Expected player slot 1, got %d", throwEvent.PlayerSlot)
	}

	if throwEvent.ThrowDetails.ArmSpeed != 12.3 {
		t.Errorf("Expected arm speed 12.3, got %f", throwEvent.ThrowDetails.ArmSpeed)
	}
}

func TestEventDetector_PlayerStatEvents(t *testing.T) {
	detector := NewEventDetector()

	// First frame - player with initial stats
	player1 := &apigame.TeamMember{
		SlotNumber:  1,
		DisplayName: "Player1",
		Stats: &apigame.PlayerStats{
			Saves:  0,
			Stuns:  0,
			Passes: 0,
		},
	}
	frame1 := createFrameWithPlayers([]*apigame.TeamMember{player1})

	// Second frame - player with increased stats
	player2 := &apigame.TeamMember{
		SlotNumber:  1,
		DisplayName: "Player1",
		Stats: &apigame.PlayerStats{
			Saves:  1,
			Stuns:  1,
			Passes: 2,
		},
	}
	frame2 := createFrameWithPlayers([]*apigame.TeamMember{player2})

	events := detector.DetectEvents(frame1, frame2)

	// Should have 3 stat events (save, stun, pass x2)
	eventTypes := make(map[string]int)
	for _, event := range events {
		switch {
		case event.GetPlayerSave() != nil:
			eventTypes["save"]++
		case event.GetPlayerStun() != nil:
			eventTypes["stun"]++
		case event.GetPlayerPass() != nil:
			eventTypes["pass"]++
		}
	}

	if eventTypes["save"] != 1 {
		t.Errorf("Expected 1 save event, got %d", eventTypes["save"])
	}

	if eventTypes["stun"] != 1 {
		t.Errorf("Expected 1 stun event, got %d", eventTypes["stun"])
	}

	if eventTypes["pass"] != 2 {
		t.Errorf("Expected 2 pass events, got %d", eventTypes["pass"])
	}
}

func TestEventDetector_DeterminePlayerRole(t *testing.T) {
	detector := NewEventDetector()

	spectator := &apigame.TeamMember{JerseyNumber: -1}
	bluePlayer := &apigame.TeamMember{SlotNumber: 2, JerseyNumber: 0}
	orangePlayer := &apigame.TeamMember{SlotNumber: 3, JerseyNumber: 1}

	if detector.determinePlayerRole(spectator) != rtapi.Role_SPECTATOR {
		t.Error("Expected SPECTATOR role for jersey -1")
	}

	if detector.determinePlayerRole(bluePlayer) != rtapi.Role_BLUE_TEAM {
		t.Error("Expected BLUE_TEAM role for even slot")
	}

	if detector.determinePlayerRole(orangePlayer) != rtapi.Role_ORANGE_TEAM {
		t.Error("Expected ORANGE_TEAM role for odd slot")
	}
}

func TestEventDetector_GetDiscState(t *testing.T) {
	detector := NewEventDetector()

	// Session with no possession
	session1 := &apigame.SessionResponse{
		Teams: []*apigame.Team{
			{
				Players: []*apigame.TeamMember{
					{SlotNumber: 1, HasPossession: false},
				},
			},
		},
	}

	state1 := detector.getDiscState(session1)
	if state1.HasPossession {
		t.Error("Expected no possession")
	}
	if state1.PlayerSlot != -1 {
		t.Errorf("Expected player slot -1, got %d", state1.PlayerSlot)
	}

	// Session with possession
	session2 := &apigame.SessionResponse{
		Teams: []*apigame.Team{
			{
				Players: []*apigame.TeamMember{
					{SlotNumber: 1, HasPossession: true},
				},
			},
		},
	}

	state2 := detector.getDiscState(session2)
	if !state2.HasPossession {
		t.Error("Expected possession")
	}
	if state2.PlayerSlot != 1 {
		t.Errorf("Expected player slot 1, got %d", state2.PlayerSlot)
	}
}

// Helper functions for creating test data

func createEmptyFrame() *rtapi.LobbySessionStateFrame {
	return &rtapi.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(time.Now()),
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{},
		},
	}
}

func createFrameWithPlayers(players []*apigame.TeamMember) *rtapi.LobbySessionStateFrame {
	return &rtapi.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(time.Now()),
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{Players: players},
			},
		},
	}
}

func createFrameWithScore(bluePoints, orangePoints, blueRound, orangeRound int32) *rtapi.LobbySessionStateFrame {
	return &rtapi.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(time.Now()),
		Session: &apigame.SessionResponse{
			BluePoints:       bluePoints,
			OrangePoints:     orangePoints,
			BlueRoundScore:   blueRound,
			OrangeRoundScore: orangeRound,
			GameClockDisplay: "10:00",
			Teams:            []*apigame.Team{},
		},
	}
}

func createFrameWithPossession(playerSlot int32) *rtapi.LobbySessionStateFrame {
	players := []*apigame.TeamMember{
		{SlotNumber: playerSlot, HasPossession: true},
	}

	return &rtapi.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(time.Now()),
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{Players: players},
			},
		},
	}
}
