package events

import (
	"slices"
	"testing"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
)

// scrambledSlots lists eight player slots in a deliberately non-ascending
// order. The player sensors key their state by a Go map, whose iteration order
// is randomized; emitting in that order (or in this input order) would not
// match the slot-sorted order these tests assert. With eight simultaneous
// events the odds of a randomized order coinciding with ascending order are
// 1/8! (~0.0000248), so these tests reliably catch non-deterministic emission.
var scrambledSlots = []int32{7, 3, 5, 1, 6, 2, 4, 0}

// ascendingSlots is scrambledSlots sorted: the deterministic order the sensors
// must emit, keyed on player slot.
var ascendingSlots = []int32{0, 1, 2, 3, 4, 5, 6, 7}

// spectatorFrame builds a frame with players at the given slots, all marked as
// spectators (jersey -1), so a role switch fires for every slot that was
// previously on a team.
func spectatorFrame(slots ...int32) *telemetry.LobbySessionStateFrame {
	players := make([]*enginev1.TeamMember, 0, len(slots))
	for _, slot := range slots {
		players = append(players, &enginev1.TeamMember{
			SlotNumber:   slot,
			DisplayName:  "P",
			JerseyNumber: -1,
		})
	}
	return &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{{Players: players}},
		},
	}
}

func TestPlayerJoinSensor_EmitsEventsInSlotOrder(t *testing.T) {
	sensor := NewPlayerJoinSensor()

	if ev := sensor.AddFrame(frameWithPlayers()); ev != nil {
		t.Fatalf("expected no event on init frame, got %v", ev)
	}

	events := drainSensor(sensor, frameWithPlayers(scrambledSlots...))

	got := make([]int32, len(events))
	for i, e := range events {
		pj := e.GetPlayerJoined()
		if pj == nil {
			t.Fatalf("event %d: expected PlayerJoined, got %T", i, e.Event)
		}
		got[i] = pj.GetPlayer().GetSlotNumber()
	}

	if !slices.Equal(got, ascendingSlots) {
		t.Fatalf("PlayerJoined events not emitted in ascending slot order:\n  got  %v\n  want %v", got, ascendingSlots)
	}
}

func TestPlayerLeaveSensor_EmitsEventsInSlotOrder(t *testing.T) {
	sensor := NewPlayerLeaveSensor()

	if ev := sensor.AddFrame(frameWithPlayers(scrambledSlots...)); ev != nil {
		t.Fatalf("expected no event on init frame, got %v", ev)
	}

	events := drainSensor(sensor, frameWithPlayers())

	got := make([]int32, len(events))
	for i, e := range events {
		pl := e.GetPlayerLeft()
		if pl == nil {
			t.Fatalf("event %d: expected PlayerLeft, got %T", i, e.Event)
		}
		got[i] = pl.GetPlayerSlot()
	}

	if !slices.Equal(got, ascendingSlots) {
		t.Fatalf("PlayerLeft events not emitted in ascending slot order:\n  got  %v\n  want %v", got, ascendingSlots)
	}
}

func TestPlayerTeamSwitchSensor_EmitsEventsInSlotOrder(t *testing.T) {
	sensor := NewPlayerTeamSwitchSensor()

	// Init: eight blue-team players (frameWithPlayers uses jersey 0 => blue).
	if ev := sensor.AddFrame(frameWithPlayers(scrambledSlots...)); ev != nil {
		t.Fatalf("expected no event on init frame, got %v", ev)
	}

	// Same slots become spectators (jersey -1 => role change for every slot).
	events := drainSensor(sensor, spectatorFrame(scrambledSlots...))

	got := make([]int32, len(events))
	for i, e := range events {
		sw := e.GetPlayerSwitchedTeam()
		if sw == nil {
			t.Fatalf("event %d: expected PlayerSwitchedTeam, got %T", i, e.Event)
		}
		got[i] = sw.GetPlayerSlot()
	}

	if !slices.Equal(got, ascendingSlots) {
		t.Fatalf("PlayerSwitchedTeam events not emitted in ascending slot order:\n  got  %v\n  want %v", got, ascendingSlots)
	}
}
