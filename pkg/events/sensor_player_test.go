package events

import (
	"testing"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
)

// Test helper functions

func createFrameWithPlayers(players ...*apigame.TeamMember) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{Players: players},
			},
		},
	}
}

func createPlayer(slot int32, name string, jerseyNumber int32) *apigame.TeamMember {
	return &apigame.TeamMember{
		SlotNumber:   slot,
		DisplayName:  name,
		JerseyNumber: jerseyNumber,
	}
}

// PlayerJoinSensor Tests

func TestPlayerJoinSensor_DetectsNewPlayer(t *testing.T) {
	sensor := NewPlayerJoinSensor()

	// First frame: no players
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{{}},
		},
	}
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: one player joins
	frame2 := createFrameWithPlayers(createPlayer(1, "Player1", 0))
	event = sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected PlayerJoined event")
	}

	joined := event.GetPlayerJoined()
	if joined == nil {
		t.Fatalf("expected PlayerJoined, got %T", event.Event)
	}

	if joined.Player.GetDisplayName() != "Player1" {
		t.Errorf("expected Player1, got %s", joined.Player.GetDisplayName())
	}

	if joined.Player.GetSlotNumber() != 1 {
		t.Errorf("expected slot 1, got %d", joined.Player.GetSlotNumber())
	}
}

func TestPlayerJoinSensor_NilFrame(t *testing.T) {
	sensor := NewPlayerJoinSensor()
	event := sensor.AddFrame(nil)
	if event != nil {
		t.Fatalf("expected nil for nil frame, got %v", event)
	}
}

func TestPlayerJoinSensor_NilSession(t *testing.T) {
	sensor := NewPlayerJoinSensor()
	event := sensor.AddFrame(&telemetry.LobbySessionStateFrame{})
	if event != nil {
		t.Fatalf("expected nil for nil session, got %v", event)
	}
}

// PlayerLeaveSensor Tests

func TestPlayerLeaveSensor_DetectsPlayerLeaving(t *testing.T) {
	sensor := NewPlayerLeaveSensor()

	// First frame: one player
	frame1 := createFrameWithPlayers(createPlayer(1, "Player1", 0))
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: no players
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{{}},
		},
	}
	event = sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected PlayerLeft event")
	}

	left := event.GetPlayerLeft()
	if left == nil {
		t.Fatalf("expected PlayerLeft, got %T", event.Event)
	}

	if left.DisplayName != "Player1" {
		t.Errorf("expected Player1, got %s", left.DisplayName)
	}

	if left.PlayerSlot != 1 {
		t.Errorf("expected slot 1, got %d", left.PlayerSlot)
	}
}

func TestPlayerLeaveSensor_NilFrame(t *testing.T) {
	sensor := NewPlayerLeaveSensor()
	event := sensor.AddFrame(nil)
	if event != nil {
		t.Fatalf("expected nil for nil frame, got %v", event)
	}
}

// PlayerTeamSwitchSensor Tests

func TestPlayerTeamSwitchSensor_DetectsTeamSwitch(t *testing.T) {
	sensor := NewPlayerTeamSwitchSensor()

	// First frame: player on blue team (slot 0-3)
	frame1 := createFrameWithPlayers(createPlayer(1, "Player1", 0))
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: same player, different slot (indicating team switch)
	frame2 := createFrameWithPlayers(createPlayer(1, "Player1", 5)) // jersey 5 = orange
	// Actually team is determined by slot in our implementation
	frame2 = createFrameWithPlayers(&apigame.TeamMember{
		SlotNumber:   5, // Changed slot to orange team range
		DisplayName:  "Player1",
		JerseyNumber: 5,
	})
	// We need to use same slot but change the role determination
	// Actually let's test with jersey number change for spectator

	// Reset and test spectator transition
	sensor = NewPlayerTeamSwitchSensor()
	frame1 = createFrameWithPlayers(&apigame.TeamMember{
		SlotNumber:   1,
		DisplayName:  "Player1",
		JerseyNumber: 0, // Blue team
	})
	sensor.AddFrame(frame1)

	// Player becomes spectator
	frame2 = createFrameWithPlayers(&apigame.TeamMember{
		SlotNumber:   1,
		DisplayName:  "Player1",
		JerseyNumber: -1, // Spectator
	})
	event = sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected PlayerSwitchedTeam event")
	}

	switched := event.GetPlayerSwitchedTeam()
	if switched == nil {
		t.Fatalf("expected PlayerSwitchedTeam, got %T", event.Event)
	}

	if switched.PlayerSlot != 1 {
		t.Errorf("expected slot 1, got %d", switched.PlayerSlot)
	}

	if switched.NewRole != telemetry.Role_ROLE_SPECTATOR {
		t.Errorf("expected SPECTATOR role, got %v", switched.NewRole)
	}
}

// EmoteSensor Tests

func TestEmoteSensor_DetectsEmotePlayed(t *testing.T) {
	sensor := NewEmoteSensor()

	// First frame: player not playing emote
	frame1 := createFrameWithPlayers(&apigame.TeamMember{
		SlotNumber:     1,
		DisplayName:    "Player1",
		IsEmotePlaying: false,
	})
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: player playing emote
	frame2 := createFrameWithPlayers(&apigame.TeamMember{
		SlotNumber:     1,
		DisplayName:    "Player1",
		IsEmotePlaying: true,
	})
	event = sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected EmotePlayed event")
	}

	emote := event.GetEmotePlayed()
	if emote == nil {
		t.Fatalf("expected EmotePlayed, got %T", event.Event)
	}

	if emote.PlayerSlot != 1 {
		t.Errorf("expected slot 1, got %d", emote.PlayerSlot)
	}
}

func TestEmoteSensor_NoEventWhenAlreadyPlaying(t *testing.T) {
	sensor := NewEmoteSensor()

	// First frame: player playing emote
	frame1 := createFrameWithPlayers(&apigame.TeamMember{
		SlotNumber:     1,
		IsEmotePlaying: true,
	})
	sensor.AddFrame(frame1)

	// Second frame: still playing emote
	frame2 := createFrameWithPlayers(&apigame.TeamMember{
		SlotNumber:     1,
		IsEmotePlaying: true,
	})
	event := sensor.AddFrame(frame2)

	if event != nil {
		t.Fatalf("expected no event when emote continues, got %v", event)
	}
}

// Helper function tests

func TestDeterminePlayerRole_Spectator(t *testing.T) {
	player := &apigame.TeamMember{JerseyNumber: -1}
	role := determinePlayerRole(player)
	if role != telemetry.Role_ROLE_SPECTATOR {
		t.Errorf("expected SPECTATOR, got %v", role)
	}
}

func TestDeterminePlayerRole_BlueTeam(t *testing.T) {
	player := &apigame.TeamMember{SlotNumber: 1, JerseyNumber: 0}
	role := determinePlayerRole(player)
	if role != telemetry.Role_ROLE_BLUE_TEAM {
		t.Errorf("expected BLUE_TEAM, got %v", role)
	}
}

func TestDeterminePlayerRole_OrangeTeam(t *testing.T) {
	player := &apigame.TeamMember{SlotNumber: 5, JerseyNumber: 1}
	role := determinePlayerRole(player)
	if role != telemetry.Role_ROLE_ORANGE_TEAM {
		t.Errorf("expected ORANGE_TEAM, got %v", role)
	}
}

func TestDeterminePlayerRole_NilPlayer(t *testing.T) {
	role := determinePlayerRole(nil)
	if role != telemetry.Role_ROLE_UNSPECIFIED {
		t.Errorf("expected UNSPECIFIED, got %v", role)
	}
}

func TestExtractPlayersMap(t *testing.T) {
	session := &apigame.SessionResponse{
		Teams: []*apigame.Team{
			{
				Players: []*apigame.TeamMember{
					{SlotNumber: 1, DisplayName: "Player1"},
					{SlotNumber: 2, DisplayName: "Player2"},
				},
			},
			{
				Players: []*apigame.TeamMember{
					{SlotNumber: 5, DisplayName: "Player3"},
				},
			},
		},
	}

	players := extractPlayersMap(session)

	if len(players) != 3 {
		t.Errorf("expected 3 players, got %d", len(players))
	}

	if players[1].GetDisplayName() != "Player1" {
		t.Errorf("expected Player1 at slot 1")
	}

	if players[5].GetDisplayName() != "Player3" {
		t.Errorf("expected Player3 at slot 5")
	}
}
