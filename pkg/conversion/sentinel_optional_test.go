package conversion

import (
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
)

// TestMapEvent_DiscPossessionChanged_FreeDiscMapsToAbsentOptional proves F-4:
// when the v1 DiscPossessionChanged event carries the -1 sentinel meaning "disc
// is free" (findPossessorSlot returns -1), the v2 DiscPossessionChanged must
// store nil (absent) for the optional fields, not a present -1 pointer. The
// proto contract at echo_arena.proto:477-478 says "Absent if disc is free."
//
// Currently RED: mapEvent always takes the address of the local variable,
// setting the pointer even when the value is -1.
func TestMapEvent_DiscPossessionChanged_FreeDiscMapsToAbsentOptional(t *testing.T) {
	t.Parallel()

	v1e := &telemetryv1.LobbySessionEvent{
		Event: &telemetryv1.LobbySessionEvent_DiscPossessionChanged{
			DiscPossessionChanged: &telemetryv1.DiscPossessionChanged{
				PlayerSlot:         -1, // sentinel: disc is free
				PreviousPlayerSlot: -1, // sentinel: disc was free
			},
		},
	}

	dp := mapEvent(v1e, nil).GetDiscPossessionChanged()
	if dp == nil {
		t.Fatal("expected DiscPossessionChanged")
	}

	if dp.HasPlayerSlot() {
		t.Errorf("HasPlayerSlot() = true, want false — -1 sentinel should map to absent (nil) optional")
	}
	if got := dp.GetPlayerSlot(); got != 0 {
		t.Errorf("GetPlayerSlot() = %d, want 0 — absent optional has zero default", got)
	}

	if dp.HasPreviousPlayerSlot() {
		t.Errorf("HasPreviousPlayerSlot() = true, want false — -1 sentinel should map to absent (nil) optional")
	}
	if got := dp.GetPreviousPlayerSlot(); got != 0 {
		t.Errorf("GetPreviousPlayerSlot() = %d, want 0 — absent optional has zero default", got)
	}
}

// TestMapEvent_DiscPossessionChanged_OccupiedDiscKeepsSlot proves the companion
// case to the free-disc test: when the v1 event carries a real player slot
// (non-negative), the v2 optional MUST be present.
func TestMapEvent_DiscPossessionChanged_OccupiedDiscKeepsSlot(t *testing.T) {
	t.Parallel()

	v1e := &telemetryv1.LobbySessionEvent{
		Event: &telemetryv1.LobbySessionEvent_DiscPossessionChanged{
			DiscPossessionChanged: &telemetryv1.DiscPossessionChanged{
				PlayerSlot:         3,
				PreviousPlayerSlot: 7,
			},
		},
	}

	dp := mapEvent(v1e, nil).GetDiscPossessionChanged()
	if dp == nil {
		t.Fatal("expected DiscPossessionChanged")
	}

	if !dp.HasPlayerSlot() {
		t.Error("HasPlayerSlot() = false, want true — real slot must be present")
	}
	if got := dp.GetPlayerSlot(); got != 3 {
		t.Errorf("GetPlayerSlot() = %d, want 3", got)
	}

	if !dp.HasPreviousPlayerSlot() {
		t.Error("HasPreviousPlayerSlot() = false, want true — real slot must be present")
	}
	if got := dp.GetPreviousPlayerSlot(); got != 7 {
		t.Errorf("GetPreviousPlayerSlot() = %d, want 7", got)
	}
}

// TestMapEvent_PlayerSteal_NoVictimMapsToAbsentOptional proves F-4 for the
// PlayerSteal path: when the v1 PlayerSteal event carries victim_player_slot=-1
// (the sentinel meaning "no victim / unknown"), the v2 VictimPlayerSlot must be
// absent (nil), not a present -1 pointer. The proto contract at
// echo_arena.proto:553-554 says "Absent if unknown."
//
// Currently RED: mapEvent always takes the address of victimSlot, setting the
// pointer even when the value is -1.
func TestMapEvent_PlayerSteal_NoVictimMapsToAbsentOptional(t *testing.T) {
	t.Parallel()

	v1e := &telemetryv1.LobbySessionEvent{
		Event: &telemetryv1.LobbySessionEvent_PlayerSteal{
			PlayerSteal: &telemetryv1.PlayerSteal{
				PlayerSlot:       1,
				TotalSteals:      5,
				VictimPlayerSlot: -1, // sentinel: no victim / unknown
			},
		},
	}

	ps := mapEvent(v1e, nil).GetPlayerSteal()
	if ps == nil {
		t.Fatal("expected PlayerSteal")
	}

	if ps.HasVictimPlayerSlot() {
		t.Errorf("HasVictimPlayerSlot() = true, want false — -1 sentinel should map to absent (nil) optional")
	}
	if got := ps.GetVictimPlayerSlot(); got != 0 {
		t.Errorf("GetVictimPlayerSlot() = %d, want 0 — absent optional has zero default", got)
	}
}

// TestMapEvent_PlayerSteal_WithVictimKeepsSlot proves the companion case: a
// real victim slot (non-negative) must remain present in v2.
func TestMapEvent_PlayerSteal_WithVictimKeepsSlot(t *testing.T) {
	t.Parallel()

	v1e := &telemetryv1.LobbySessionEvent{
		Event: &telemetryv1.LobbySessionEvent_PlayerSteal{
			PlayerSteal: &telemetryv1.PlayerSteal{
				PlayerSlot:       1,
				TotalSteals:      5,
				VictimPlayerSlot: 3,
			},
		},
	}

	ps := mapEvent(v1e, nil).GetPlayerSteal()
	if ps == nil {
		t.Fatal("expected PlayerSteal")
	}

	if !ps.HasVictimPlayerSlot() {
		t.Error("HasVictimPlayerSlot() = false, want true — real victim slot must be present")
	}
	if got := ps.GetVictimPlayerSlot(); got != 3 {
		t.Errorf("GetVictimPlayerSlot() = %d, want 3", got)
	}
}

// TestMapEvent_DiscPossessionChanged_PartialFreeDisc tests the mixed case: one
// slot is free (-1), the other is occupied. Each optional independently follows
// the sentinel rule.
func TestMapEvent_DiscPossessionChanged_PartialFreeDisc(t *testing.T) {
	t.Parallel()

	v1e := &telemetryv1.LobbySessionEvent{
		Event: &telemetryv1.LobbySessionEvent_DiscPossessionChanged{
			DiscPossessionChanged: &telemetryv1.DiscPossessionChanged{
				PlayerSlot:         4,  // someone grabbed it
				PreviousPlayerSlot: -1, // from free disc
			},
		},
	}

	dp := mapEvent(v1e, nil).GetDiscPossessionChanged()
	if dp == nil {
		t.Fatal("expected DiscPossessionChanged")
	}

	if !dp.HasPlayerSlot() {
		t.Error("HasPlayerSlot() = false, want true — real slot must be present")
	}
	if got := dp.GetPlayerSlot(); got != 4 {
		t.Errorf("GetPlayerSlot() = %d, want 4", got)
	}

	if dp.HasPreviousPlayerSlot() {
		t.Errorf("HasPreviousPlayerSlot() = true, want false — -1 sentinel should map to absent")
	}
}

// TestMapFrame_DiscHolderFirstPossessorWins proves the F-4 consistency fix:
// when more than one player carries has_possession in a single frame (never
// observed from the engine, but possible in crafted input), the frame's
// disc_holder_slot must be the FIRST possessor, matching findPossessorSlot
// (pkg/events/sensor_disc.go:173-182), so the frame and the
// DiscPossessionChanged events cannot disagree within a frame.
func TestMapFrame_DiscHolderFirstPossessorWins(t *testing.T) {
	t.Parallel()

	v1f := &telemetryv1.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{
				{Players: []*enginev1.TeamMember{
					{SlotNumber: 3, HasPossession: true},
					{SlotNumber: 5, HasPossession: true},
				}},
			},
		},
	}

	ea := mapFrame(v1f, time.Time{}, 0, nil).GetEchoArena()
	if ea == nil {
		t.Fatal("expected EchoArena frame")
	}
	if got := ea.GetDiscHolderSlot(); got != 3 {
		t.Fatalf("DiscHolderSlot = %d, want 3 (first possessor, not the last)", got)
	}
}
