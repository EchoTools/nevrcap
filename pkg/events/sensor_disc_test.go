package events

import (
	"testing"

	apigame "github.com/echotools/nevr-common/v4/gen/go/apigame/v1"
	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
)

// DiscPossessionSensor Tests

func TestDiscPossessionSensor_DetectsPossessionChange(t *testing.T) {
	sensor := NewDiscPossessionSensor()

	// First frame: player 1 has possession
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{
					Players: []*apigame.TeamMember{
						{SlotNumber: 1, HasPossession: true},
						{SlotNumber: 2, HasPossession: false},
					},
				},
			},
		},
	}
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: player 2 has possession
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{
					Players: []*apigame.TeamMember{
						{SlotNumber: 1, HasPossession: false},
						{SlotNumber: 2, HasPossession: true},
					},
				},
			},
		},
	}
	event = sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected DiscPossessionChanged event")
	}

	possession := event.GetDiscPossessionChanged()
	if possession == nil {
		t.Fatalf("expected DiscPossessionChanged, got %T", event.Event)
	}

	if possession.PlayerSlot != 2 {
		t.Errorf("expected PlayerSlot=2, got %d", possession.PlayerSlot)
	}

	if possession.PreviousPlayerSlot != 1 {
		t.Errorf("expected PreviousPlayerSlot=1, got %d", possession.PreviousPlayerSlot)
	}
}

func TestDiscPossessionSensor_DetectsLostPossession(t *testing.T) {
	sensor := NewDiscPossessionSensor()

	// First frame: player 1 has possession
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{Players: []*apigame.TeamMember{{SlotNumber: 1, HasPossession: true}}},
			},
		},
	}
	sensor.AddFrame(frame1)

	// Second frame: no one has possession
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{Players: []*apigame.TeamMember{{SlotNumber: 1, HasPossession: false}}},
			},
		},
	}
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected DiscPossessionChanged event")
	}

	possession := event.GetDiscPossessionChanged()
	if possession.PlayerSlot != -1 {
		t.Errorf("expected PlayerSlot=-1 (free disc), got %d", possession.PlayerSlot)
	}
}

func TestDiscPossessionSensor_NilFrame(t *testing.T) {
	sensor := NewDiscPossessionSensor()
	event := sensor.AddFrame(nil)
	if event != nil {
		t.Fatalf("expected nil for nil frame, got %v", event)
	}
}

// DiscThrownSensor Tests

func TestDiscThrownSensor_DetectsThrow(t *testing.T) {
	sensor := NewDiscThrownSensor()

	// First frame: player 1 has possession, no throw yet
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{Players: []*apigame.TeamMember{{SlotNumber: 1, HasPossession: true}}},
			},
		},
	}
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: throw info appears
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{Players: []*apigame.TeamMember{{SlotNumber: 1, HasPossession: false}}},
			},
			LastThrow: &apigame.LastThrowInfo{
				ArmSpeed:   12.5,
				TotalSpeed: 18.0,
				RotPerSec:  5.0,
			},
		},
	}
	event = sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected DiscThrown event")
	}

	thrown := event.GetDiscThrown()
	if thrown == nil {
		t.Fatalf("expected DiscThrown, got %T", event.Event)
	}

	if thrown.PlayerSlot != 1 {
		t.Errorf("expected PlayerSlot=1, got %d", thrown.PlayerSlot)
	}

	if thrown.ThrowDetails.GetArmSpeed() != 12.5 {
		t.Errorf("expected ArmSpeed=12.5, got %f", thrown.ThrowDetails.GetArmSpeed())
	}

	if thrown.ThrowDetails.GetTotalSpeed() != 18.0 {
		t.Errorf("expected TotalSpeed=18.0, got %f", thrown.ThrowDetails.GetTotalSpeed())
	}
}

func TestDiscThrownSensor_NoEventForSameThrow(t *testing.T) {
	sensor := NewDiscThrownSensor()

	throwInfo := &apigame.LastThrowInfo{
		ArmSpeed:   12.5,
		TotalSpeed: 18.0,
		RotPerSec:  5.0,
	}

	frame := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{Players: []*apigame.TeamMember{{SlotNumber: 1, HasPossession: true}}},
			},
			LastThrow: throwInfo,
		},
	}

	// First occurrence
	event := sensor.AddFrame(frame)
	if event == nil {
		t.Fatal("expected DiscThrown event on first occurrence")
	}

	// Same throw again
	event = sensor.AddFrame(frame)
	if event != nil {
		t.Fatalf("expected no event for same throw, got %v", event)
	}
}

// DiscCaughtSensor Tests

func TestDiscCaughtSensor_DetectsCatch(t *testing.T) {
	sensor := NewDiscCaughtSensor()

	// First frame: disc is free
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{Players: []*apigame.TeamMember{
					{SlotNumber: 1, HasPossession: false},
					{SlotNumber: 2, HasPossession: false},
				}},
			},
		},
	}
	event := sensor.AddFrame(frame1)
	if event != nil {
		t.Fatalf("expected no event on first frame, got %v", event)
	}

	// Second frame: player 2 catches
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{Players: []*apigame.TeamMember{
					{SlotNumber: 1, HasPossession: false},
					{SlotNumber: 2, HasPossession: true},
				}},
			},
		},
	}
	event = sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected DiscCaught event")
	}

	caught := event.GetDiscCaught()
	if caught == nil {
		t.Fatalf("expected DiscCaught, got %T", event.Event)
	}

	if caught.PlayerSlot != 2 {
		t.Errorf("expected PlayerSlot=2, got %d", caught.PlayerSlot)
	}
}

func TestDiscCaughtSensor_DetectsInterception(t *testing.T) {
	sensor := NewDiscCaughtSensor()

	// First frame: player 1 has possession
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{Players: []*apigame.TeamMember{
					{SlotNumber: 1, HasPossession: true},
					{SlotNumber: 5, HasPossession: false},
				}},
			},
		},
	}
	sensor.AddFrame(frame1)

	// Second frame: player 5 catches (interception)
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &apigame.SessionResponse{
			Teams: []*apigame.Team{
				{Players: []*apigame.TeamMember{
					{SlotNumber: 1, HasPossession: false},
					{SlotNumber: 5, HasPossession: true},
				}},
			},
		},
	}
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected DiscCaught event for interception")
	}

	caught := event.GetDiscCaught()
	if caught.PlayerSlot != 5 {
		t.Errorf("expected PlayerSlot=5, got %d", caught.PlayerSlot)
	}
}

// findPossessorSlot Tests

func TestFindPossessorSlot_NoOne(t *testing.T) {
	session := &apigame.SessionResponse{
		Teams: []*apigame.Team{
			{Players: []*apigame.TeamMember{
				{SlotNumber: 1, HasPossession: false},
				{SlotNumber: 2, HasPossession: false},
			}},
		},
	}

	slot := findPossessorSlot(session)
	if slot != -1 {
		t.Errorf("expected -1 when no one has possession, got %d", slot)
	}
}

func TestFindPossessorSlot_Found(t *testing.T) {
	session := &apigame.SessionResponse{
		Teams: []*apigame.Team{
			{Players: []*apigame.TeamMember{
				{SlotNumber: 1, HasPossession: false},
				{SlotNumber: 2, HasPossession: true},
			}},
		},
	}

	slot := findPossessorSlot(session)
	if slot != 2 {
		t.Errorf("expected slot 2 to have possession, got %d", slot)
	}
}

// lastThrowEqual Tests

func TestLastThrowEqual_BothNil(t *testing.T) {
	if !lastThrowEqual(nil, nil) {
		t.Error("expected true for both nil")
	}
}

func TestLastThrowEqual_OneNil(t *testing.T) {
	throw := &apigame.LastThrowInfo{ArmSpeed: 10.0}
	if lastThrowEqual(nil, throw) {
		t.Error("expected false when one is nil")
	}
	if lastThrowEqual(throw, nil) {
		t.Error("expected false when one is nil")
	}
}

func TestLastThrowEqual_Equal(t *testing.T) {
	a := &apigame.LastThrowInfo{ArmSpeed: 10.0, TotalSpeed: 15.0, RotPerSec: 5.0}
	b := &apigame.LastThrowInfo{ArmSpeed: 10.0, TotalSpeed: 15.0, RotPerSec: 5.0}
	if !lastThrowEqual(a, b) {
		t.Error("expected true for equal throws")
	}
}

func TestLastThrowEqual_NotEqual(t *testing.T) {
	a := &apigame.LastThrowInfo{ArmSpeed: 10.0, TotalSpeed: 15.0}
	b := &apigame.LastThrowInfo{ArmSpeed: 12.0, TotalSpeed: 15.0}
	if lastThrowEqual(a, b) {
		t.Error("expected false for different throws")
	}
}
