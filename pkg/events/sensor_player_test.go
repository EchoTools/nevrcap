package events

import (
	"testing"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
)

// Test helper functions

func createFrameWithPlayers(players ...*enginev1.TeamMember) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{
				{Players: players},
			},
		},
	}
}

func createPlayer(slot int32, name string, jerseyNumber int32) *enginev1.TeamMember {
	return &enginev1.TeamMember{
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
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{{}},
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

func TestPlayerJoinSensor_MultipleJoins(t *testing.T) {
	sensor := NewPlayerJoinSensor()

	// First frame: no players
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{{}},
		},
	}
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: three players join simultaneously
	frame2 := createFrameWithPlayers(
		createPlayer(1, "Alice", 0),
		createPlayer(2, "Bob", 1),
		createPlayer(3, "Carol", 2),
	)

	// Collect all events by draining the sensor (same pattern the
	// detector loop uses: first call with the frame, subsequent
	// calls with nil to drain pending events).
	var events []*telemetry.LobbySessionEvent
	f := frame2
	for {
		event = sensor.AddFrame(f)
		if event == nil {
			break
		}
		events = append(events, event)
		f = nil // drain mode
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 PlayerJoined events, got %d", len(events))
	}

	// Collect the names we got (map iteration order is non-deterministic).
	names := make(map[string]bool)
	for _, e := range events {
		joined := e.GetPlayerJoined()
		if joined == nil {
			t.Fatalf("expected PlayerJoined, got %T", e.Event)
		}
		names[joined.Player.GetDisplayName()] = true
	}

	for _, want := range []string{"Alice", "Bob", "Carol"} {
		if !names[want] {
			t.Errorf("missing PlayerJoined event for %s", want)
		}
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
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{{}},
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
	frame2 = createFrameWithPlayers(&enginev1.TeamMember{
		SlotNumber:   5, // Changed slot to orange team range
		DisplayName:  "Player1",
		JerseyNumber: 5,
	})
	// We need to use same slot but change the role determination
	// Actually let's test with jersey number change for spectator

	// Reset and test spectator transition
	sensor = NewPlayerTeamSwitchSensor()
	frame1 = createFrameWithPlayers(&enginev1.TeamMember{
		SlotNumber:   1,
		DisplayName:  "Player1",
		JerseyNumber: 0, // Blue team
	})
	sensor.AddFrame(frame1)

	// Player becomes spectator
	frame2 = createFrameWithPlayers(&enginev1.TeamMember{
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
	frame1 := createFrameWithPlayers(&enginev1.TeamMember{
		SlotNumber:     1,
		DisplayName:    "Player1",
		IsEmotePlaying: false,
	})
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: player playing emote
	frame2 := createFrameWithPlayers(&enginev1.TeamMember{
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
	frame1 := createFrameWithPlayers(&enginev1.TeamMember{
		SlotNumber:     1,
		IsEmotePlaying: true,
	})
	sensor.AddFrame(frame1)

	// Second frame: still playing emote
	frame2 := createFrameWithPlayers(&enginev1.TeamMember{
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
	player := &enginev1.TeamMember{JerseyNumber: -1}
	role := determinePlayerRole(player, 0)
	if role != telemetry.Role_ROLE_SPECTATOR {
		t.Errorf("expected SPECTATOR, got %v", role)
	}
}

func TestDeterminePlayerRole_BlueTeam(t *testing.T) {
	player := &enginev1.TeamMember{SlotNumber: 1, JerseyNumber: 0}
	role := determinePlayerRole(player, 0)
	if role != telemetry.Role_ROLE_BLUE_TEAM {
		t.Errorf("expected BLUE_TEAM, got %v", role)
	}
}

func TestDeterminePlayerRole_OrangeTeam(t *testing.T) {
	player := &enginev1.TeamMember{SlotNumber: 5, JerseyNumber: 1}
	role := determinePlayerRole(player, 1)
	if role != telemetry.Role_ROLE_ORANGE_TEAM {
		t.Errorf("expected ORANGE_TEAM, got %v", role)
	}
}

func TestDeterminePlayerRole_NilPlayer(t *testing.T) {
	role := determinePlayerRole(nil, 0)
	if role != telemetry.Role_ROLE_UNSPECIFIED {
		t.Errorf("expected UNSPECIFIED, got %v", role)
	}
}

// A player on a third team slot (index >= 2) with a real jersey number is a
// spectator, not orange — matching the v2 InitialRoster resolution.
func TestDeterminePlayerRole_SpectatorTeamIndex(t *testing.T) {
	player := &enginev1.TeamMember{SlotNumber: 7, JerseyNumber: 3}
	role := determinePlayerRole(player, 2)
	if role != telemetry.Role_ROLE_SPECTATOR {
		t.Errorf("expected SPECTATOR, got %v", role)
	}
}

func TestExtractPlayersMap(t *testing.T) {
	session := &enginev1.SessionResponse{
		Teams: []*enginev1.Team{
			{
				Players: []*enginev1.TeamMember{
					{SlotNumber: 1, DisplayName: "Player1"},
					{SlotNumber: 2, DisplayName: "Player2"},
				},
			},
			{
				Players: []*enginev1.TeamMember{
					{SlotNumber: 5, DisplayName: "Player3"},
				},
			},
		},
	}

	players := extractPlayersMap(session)

	if len(players) != 3 {
		t.Errorf("expected 3 players, got %d", len(players))
	}

	if players[1].player.GetDisplayName() != "Player1" {
		t.Errorf("expected Player1 at slot 1")
	}
	if players[1].teamIdx != 0 {
		t.Errorf("expected Player1 to be on team 0 (blue), got %d", players[1].teamIdx)
	}

	if players[5].player.GetDisplayName() != "Player3" {
		t.Errorf("expected Player3 at slot 5")
	}
	if players[5].teamIdx != 1 {
		t.Errorf("expected Player3 to be on team 1 (orange), got %d", players[5].teamIdx)
	}
}
