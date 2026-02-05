package events

import (
	"testing"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
)

// Helper to create a frame with a player that has specific stats
func createFrameWithPlayerStats(slot int32, stats *apigame.PlayerStats) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{
					Players: []*apigame.TeamMember{
						{SlotNumber: slot, Stats: stats},
					},
				},
			},
		},
	}
}

// Helper to create a frame with two players (for steal victim tracking)
func createFrameWithTwoPlayers(slot1 int32, stats1 *apigame.PlayerStats, hasPossession1 bool, slot2 int32, stats2 *apigame.PlayerStats, hasPossession2 bool) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{
					Players: []*apigame.TeamMember{
						{SlotNumber: slot1, Stats: stats1, HasPossession: hasPossession1},
						{SlotNumber: slot2, Stats: stats2, HasPossession: hasPossession2},
					},
				},
			},
		},
	}
}

// StatEventSensor Tests

func TestStatEventSensor_DetectsGoal(t *testing.T) {
	sensor := NewStatEventSensor()

	// First frame: no goals
	frame1 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Goals: 0, Points: 0})
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: player scored
	frame2 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Goals: 1, Points: 2})
	event = sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected PlayerGoal event")
	}

	goal := event.GetPlayerGoal()
	if goal == nil {
		t.Fatalf("expected PlayerGoal, got %T", event.Event)
	}

	if goal.PlayerSlot != 1 {
		t.Errorf("expected PlayerSlot=1, got %d", goal.PlayerSlot)
	}

	if goal.TotalGoals != 1 {
		t.Errorf("expected TotalGoals=1, got %d", goal.TotalGoals)
	}

	if goal.Points != 2 {
		t.Errorf("expected Points=2, got %d", goal.Points)
	}
}

func TestStatEventSensor_DetectsSave(t *testing.T) {
	sensor := NewStatEventSensor()

	frame1 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Saves: 0})
	sensor.AddFrame(frame1)

	frame2 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Saves: 1})
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected PlayerSave event")
	}

	save := event.GetPlayerSave()
	if save == nil {
		t.Fatalf("expected PlayerSave, got %T", event.Event)
	}

	if save.PlayerSlot != 1 {
		t.Errorf("expected PlayerSlot=1, got %d", save.PlayerSlot)
	}

	if save.TotalSaves != 1 {
		t.Errorf("expected TotalSaves=1, got %d", save.TotalSaves)
	}
}

func TestStatEventSensor_DetectsStun(t *testing.T) {
	sensor := NewStatEventSensor()

	frame1 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Stuns: 0})
	sensor.AddFrame(frame1)

	frame2 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Stuns: 1})
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected PlayerStun event")
	}

	stun := event.GetPlayerStun()
	if stun == nil {
		t.Fatalf("expected PlayerStun, got %T", event.Event)
	}

	if stun.TotalStuns != 1 {
		t.Errorf("expected TotalStuns=1, got %d", stun.TotalStuns)
	}
}

func TestStatEventSensor_DetectsPass(t *testing.T) {
	sensor := NewStatEventSensor()

	frame1 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Passes: 0})
	sensor.AddFrame(frame1)

	frame2 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Passes: 1})
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected PlayerPass event")
	}

	pass := event.GetPlayerPass()
	if pass == nil {
		t.Fatalf("expected PlayerPass, got %T", event.Event)
	}

	if pass.TotalPasses != 1 {
		t.Errorf("expected TotalPasses=1, got %d", pass.TotalPasses)
	}
}

func TestStatEventSensor_DetectsSteal(t *testing.T) {
	sensor := NewStatEventSensor()

	frame1 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Steals: 0})
	sensor.AddFrame(frame1)

	frame2 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Steals: 1})
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected PlayerSteal event")
	}

	steal := event.GetPlayerSteal()
	if steal == nil {
		t.Fatalf("expected PlayerSteal, got %T", event.Event)
	}

	if steal.TotalSteals != 1 {
		t.Errorf("expected TotalSteals=1, got %d", steal.TotalSteals)
	}

	// VictimPlayerSlot should be -1 since no possession was tracked
	if steal.VictimPlayerSlot != -1 {
		t.Errorf("expected VictimPlayerSlot=-1 (no possession tracked), got %d", steal.VictimPlayerSlot)
	}
}

func TestStatEventSensor_DetectsStealWithVictim(t *testing.T) {
	sensor := NewStatEventSensor()

	// Frame 1: Player 2 (slot 2) has possession, player 1 (slot 1) has 0 steals
	frame1 := createFrameWithTwoPlayers(1, &apigame.PlayerStats{Steals: 0}, false, 2, &apigame.PlayerStats{Steals: 0}, true)
	sensor.AddFrame(frame1)

	// Frame 2: Player 1 now has possession (stole it), steals stat incremented
	frame2 := createFrameWithTwoPlayers(1, &apigame.PlayerStats{Steals: 1}, true, 2, &apigame.PlayerStats{Steals: 0}, false)
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected PlayerSteal event")
	}

	steal := event.GetPlayerSteal()
	if steal == nil {
		t.Fatalf("expected PlayerSteal, got %T", event.Event)
	}

	if steal.PlayerSlot != 1 {
		t.Errorf("expected PlayerSlot=1, got %d", steal.PlayerSlot)
	}

	if steal.TotalSteals != 1 {
		t.Errorf("expected TotalSteals=1, got %d", steal.TotalSteals)
	}

	// VictimPlayerSlot should be 2 since player 2 had possession before the steal
	if steal.VictimPlayerSlot != 2 {
		t.Errorf("expected VictimPlayerSlot=2, got %d", steal.VictimPlayerSlot)
	}
}

func TestStatEventSensor_DetectsBlock(t *testing.T) {
	sensor := NewStatEventSensor()

	frame1 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Blocks: 0})
	sensor.AddFrame(frame1)

	frame2 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Blocks: 1})
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected PlayerBlock event")
	}

	block := event.GetPlayerBlock()
	if block == nil {
		t.Fatalf("expected PlayerBlock, got %T", event.Event)
	}

	if block.TotalBlocks != 1 {
		t.Errorf("expected TotalBlocks=1, got %d", block.TotalBlocks)
	}
}

func TestStatEventSensor_DetectsInterception(t *testing.T) {
	sensor := NewStatEventSensor()

	frame1 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Interceptions: 0})
	sensor.AddFrame(frame1)

	frame2 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Interceptions: 1})
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected PlayerInterception event")
	}

	interception := event.GetPlayerInterception()
	if interception == nil {
		t.Fatalf("expected PlayerInterception, got %T", event.Event)
	}

	if interception.TotalInterceptions != 1 {
		t.Errorf("expected TotalInterceptions=1, got %d", interception.TotalInterceptions)
	}
}

func TestStatEventSensor_DetectsAssist(t *testing.T) {
	sensor := NewStatEventSensor()

	frame1 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Assists: 0})
	sensor.AddFrame(frame1)

	frame2 := createFrameWithPlayerStats(1, &apigame.PlayerStats{Assists: 1})
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected PlayerAssist event")
	}

	assist := event.GetPlayerAssist()
	if assist == nil {
		t.Fatalf("expected PlayerAssist, got %T", event.Event)
	}

	if assist.TotalAssists != 1 {
		t.Errorf("expected TotalAssists=1, got %d", assist.TotalAssists)
	}
}

func TestStatEventSensor_DetectsShotTaken(t *testing.T) {
	sensor := NewStatEventSensor()

	frame1 := createFrameWithPlayerStats(1, &apigame.PlayerStats{ShotsTaken: 0})
	sensor.AddFrame(frame1)

	frame2 := createFrameWithPlayerStats(1, &apigame.PlayerStats{ShotsTaken: 1})
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected PlayerShotTaken event")
	}

	shot := event.GetPlayerShotTaken()
	if shot == nil {
		t.Fatalf("expected PlayerShotTaken, got %T", event.Event)
	}

	if shot.TotalShots != 1 {
		t.Errorf("expected TotalShots=1, got %d", shot.TotalShots)
	}
}

func TestStatEventSensor_MultipleEventsInOneFrame(t *testing.T) {
	sensor := NewStatEventSensor()

	// First frame: no stats
	frame1 := createFrameWithPlayerStats(1, &apigame.PlayerStats{})
	sensor.AddFrame(frame1)

	// Second frame: multiple stat increases
	frame2 := createFrameWithPlayerStats(1, &apigame.PlayerStats{
		Stuns:  2,
		Passes: 1,
	})

	// Should get all events one at a time
	events := make([]*telemetry.LobbySessionEvent, 0)
	for {
		event := sensor.AddFrame(frame2)
		if event == nil {
			break
		}
		events = append(events, event)
	}

	// Should have 3 events: 2 stuns + 1 pass
	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}

	// Count event types
	stunCount := 0
	passCount := 0
	for _, e := range events {
		if e.GetPlayerStun() != nil {
			stunCount++
		}
		if e.GetPlayerPass() != nil {
			passCount++
		}
	}

	if stunCount != 2 {
		t.Errorf("expected 2 stun events, got %d", stunCount)
	}
	if passCount != 1 {
		t.Errorf("expected 1 pass event, got %d", passCount)
	}
}

func TestStatEventSensor_NilFrame(t *testing.T) {
	sensor := NewStatEventSensor()
	event := sensor.AddFrame(nil)
	if event != nil {
		t.Fatalf("expected nil for nil frame, got %v", event)
	}
}

func TestStatEventSensor_NilStats(t *testing.T) {
	sensor := NewStatEventSensor()

	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{Players: []*apigame.TeamMember{{SlotNumber: 1, Stats: nil}}},
			},
		},
	}
	sensor.AddFrame(frame1)

	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{Players: []*apigame.TeamMember{{SlotNumber: 1, Stats: nil}}},
			},
		},
	}
	event := sensor.AddFrame(frame2)

	// Should handle nil stats gracefully
	if event != nil {
		t.Fatalf("expected no event for nil stats, got %v", event)
	}
}

// snapshotFromStats Tests

func TestSnapshotFromStats_NilStats(t *testing.T) {
	snapshot := snapshotFromStats(nil)
	if snapshot.goals != 0 || snapshot.saves != 0 || snapshot.stuns != 0 {
		t.Error("expected all zeros for nil stats")
	}
}

func TestSnapshotFromStats_WithStats(t *testing.T) {
	stats := &apigame.PlayerStats{
		Goals:         3,
		Saves:         2,
		Stuns:         5,
		Passes:        10,
		Steals:        1,
		Blocks:        4,
		Interceptions: 2,
		Assists:       3,
		ShotsTaken:    15,
		Points:        8,
	}

	snapshot := snapshotFromStats(stats)

	if snapshot.goals != 3 {
		t.Errorf("expected goals=3, got %d", snapshot.goals)
	}
	if snapshot.saves != 2 {
		t.Errorf("expected saves=2, got %d", snapshot.saves)
	}
	if snapshot.stuns != 5 {
		t.Errorf("expected stuns=5, got %d", snapshot.stuns)
	}
	if snapshot.passes != 10 {
		t.Errorf("expected passes=10, got %d", snapshot.passes)
	}
	if snapshot.steals != 1 {
		t.Errorf("expected steals=1, got %d", snapshot.steals)
	}
	if snapshot.blocks != 4 {
		t.Errorf("expected blocks=4, got %d", snapshot.blocks)
	}
	if snapshot.interceptions != 2 {
		t.Errorf("expected interceptions=2, got %d", snapshot.interceptions)
	}
	if snapshot.assists != 3 {
		t.Errorf("expected assists=3, got %d", snapshot.assists)
	}
	if snapshot.shotsTaken != 15 {
		t.Errorf("expected shotsTaken=15, got %d", snapshot.shotsTaken)
	}
	if snapshot.points != 8 {
		t.Errorf("expected points=8, got %d", snapshot.points)
	}
}
