package events

import (
	"testing"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
)

func TestStatEventSensor_CleansUpDepartedPlayers(t *testing.T) {
	sensor := NewStatEventSensor()

	// Frame 1: two players present
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{
				{
					Players: []*enginev1.TeamMember{
						{SlotNumber: 1, Stats: &enginev1.PlayerStats{Goals: 0}},
						{SlotNumber: 2, Stats: &enginev1.PlayerStats{Goals: 0}},
					},
				},
			},
		},
	}
	sensor.AddFrame(frame1)

	// Verify both players are tracked
	if len(sensor.prevStats) != 2 {
		t.Fatalf("expected 2 tracked players after frame 1, got %d", len(sensor.prevStats))
	}

	// Frame 2: only player 1 remains (player 2 departed)
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{
				{
					Players: []*enginev1.TeamMember{
						{SlotNumber: 1, Stats: &enginev1.PlayerStats{Goals: 0}},
					},
				},
			},
		},
	}
	// Drain all events from frame 2
	for sensor.AddFrame(frame2) != nil {
	}

	// Player 2 should be cleaned up
	if len(sensor.prevStats) != 1 {
		t.Errorf("expected 1 tracked player after frame 2, got %d", len(sensor.prevStats))
	}
	if _, exists := sensor.prevStats[2]; exists {
		t.Error("departed player (slot 2) should have been removed from prevStats")
	}
	if _, exists := sensor.prevStats[1]; !exists {
		t.Error("remaining player (slot 1) should still be in prevStats")
	}
}

func TestStatEventSensor_DepartedPlayerNoFalseEvents(t *testing.T) {
	sensor := NewStatEventSensor()

	// Frame 1: player with 5 goals
	frame1 := createFrameWithPlayerStats(1, &enginev1.PlayerStats{Goals: 5, Points: 10})
	sensor.AddFrame(frame1)

	// Frame 2: player leaves (empty frame)
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{
				{Players: []*enginev1.TeamMember{}},
			},
		},
	}
	for sensor.AddFrame(frame2) != nil {
	}

	// Frame 3: NEW player joins at the same slot with 0 goals
	frame3 := createFrameWithPlayerStats(1, &enginev1.PlayerStats{Goals: 0, Points: 0})
	event := sensor.AddFrame(frame3)

	// Without cleanup: the sensor would compare goals=0 vs old goals=5 and
	// see a "decrease" which is handled as no event (only increases trigger).
	// With cleanup: the sensor sees a fresh player with no previous state, so
	// also no event. Either way, no false goal events.
	if event != nil {
		t.Errorf("expected no event for newly joined player, got %T", event.Event)
	}
}
