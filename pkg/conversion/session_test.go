package conversion_test

import (
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/echotools/tape/v4/pkg/conversion"
)

// frameWithEvents builds a v2 Frame carrying the given EchoArena events.
func frameWithEvents(events ...*capturepb.EchoEvent) *capturepb.Frame {
	ea := &capturepb.EchoArenaFrame{Events: events}
	return &capturepb.Frame{Payload: &capturepb.Frame_EchoArena{EchoArena: ea}}
}

func joinedEvent(slot int32, account uint64, name string, role capturepb.Role, jersey, level int32) *capturepb.EchoEvent {
	return &capturepb.EchoEvent{Event: &capturepb.EchoEvent_PlayerJoined{
		PlayerJoined: &capturepb.PlayerJoined{
			Slot: slot, AccountNumber: account, DisplayName: name,
			Role: role, JerseyNumber: jersey, Level: level,
		},
	}}
}

func leftEvent(slot int32, name string) *capturepb.EchoEvent {
	return &capturepb.EchoEvent{Event: &capturepb.EchoEvent_PlayerLeft{
		PlayerLeft: &capturepb.PlayerLeft{PlayerSlot: slot, DisplayName: name},
	}}
}

func loadoutEvent(slot int32, weapon, ordnance, tacMod string) *capturepb.EchoEvent {
	return &capturepb.EchoEvent{Event: &capturepb.EchoEvent_LoadoutChanged{
		LoadoutChanged: &capturepb.LoadoutChanged{
			PlayerSlot: slot, Weapon: weapon, Ordnance: ordnance, TacMod: tacMod,
		},
	}}
}

func grabEvent(slot int32, left, right string) *capturepb.EchoEvent {
	return &capturepb.EchoEvent{Event: &capturepb.EchoEvent_GrabChanged{
		GrabChanged: &capturepb.GrabChanged{
			PlayerSlot: slot, LeftHolding: left, RightHolding: right,
		},
	}}
}

func scoreboardEvent(blue, orange, blueRound, orangeRound int32, clock string) *capturepb.EchoEvent {
	return &capturepb.EchoEvent{Event: &capturepb.EchoEvent_ScoreboardUpdated{
		ScoreboardUpdated: &capturepb.ScoreboardUpdated{
			BluePoints: blue, OrangePoints: orange,
			BlueRoundScore: blueRound, OrangeRoundScore: orangeRound,
			GameClockDisplay: clock,
		},
	}}
}

func TestSessionRosterAt(t *testing.T) {
	t.Parallel()

	header := &capturepb.EchoArenaHeader{
		InitialRoster: []*capturepb.PlayerInfo{
			{Slot: 0, AccountNumber: 100, DisplayName: "Alice", Role: capturepb.Role_ROLE_BLUE_TEAM, JerseyNumber: 1, Level: 50},
			{Slot: 1, AccountNumber: 200, DisplayName: "Bob", Role: capturepb.Role_ROLE_ORANGE_TEAM, JerseyNumber: 2, Level: 40},
		},
	}
	frames := []*capturepb.Frame{
		// frame 0: the two seeded players re-emit PlayerJoined (idempotent).
		frameWithEvents(
			joinedEvent(0, 100, "Alice", capturepb.Role_ROLE_BLUE_TEAM, 1, 50),
			joinedEvent(1, 200, "Bob", capturepb.Role_ROLE_ORANGE_TEAM, 2, 40),
		),
		// frame 1: nothing changes.
		frameWithEvents(),
		// frame 2: Carol joins slot 2; Bob leaves slot 1.
		frameWithEvents(
			joinedEvent(2, 300, "Carol", capturepb.Role_ROLE_BLUE_TEAM, 3, 30),
			leftEvent(1, "Bob"),
		),
		// frame 3: slot 1 reused by Dave.
		frameWithEvents(
			joinedEvent(1, 400, "Dave", capturepb.Role_ROLE_ORANGE_TEAM, 9, 10),
		),
	}

	s := conversion.NewSession(header, frames)

	if got := s.FrameCount(); got != 4 {
		t.Fatalf("FrameCount = %d, want 4", got)
	}

	// Frame 0: Alice + Bob present.
	r0 := s.RosterAt(0)
	if len(r0) != 2 {
		t.Fatalf("RosterAt(0) len = %d, want 2", len(r0))
	}
	if r0[0].GetDisplayName() != "Alice" || r0[0].GetAccountNumber() != 100 ||
		r0[0].GetJerseyNumber() != 1 || r0[0].GetLevel() != 50 ||
		r0[0].GetRole() != capturepb.Role_ROLE_BLUE_TEAM {
		t.Errorf("RosterAt(0)[0] = %+v, want Alice identity", r0[0])
	}
	if r0[1].GetDisplayName() != "Bob" {
		t.Errorf("RosterAt(0)[1] name = %q, want Bob", r0[1].GetDisplayName())
	}

	// Frame 1: unchanged from frame 0.
	if r1 := s.RosterAt(1); len(r1) != 2 || r1[1].GetDisplayName() != "Bob" {
		t.Errorf("RosterAt(1) = %v, want Alice+Bob", r1)
	}

	// Frame 2: Carol added, Bob (slot 1) removed.
	r2 := s.RosterAt(2)
	if len(r2) != 2 {
		t.Fatalf("RosterAt(2) len = %d, want 2 (Alice+Carol)", len(r2))
	}
	if _, ok := r2[1]; ok {
		t.Errorf("RosterAt(2) still has slot 1 after Bob left")
	}
	if r2[2].GetDisplayName() != "Carol" {
		t.Errorf("RosterAt(2)[2] name = %q, want Carol", r2[2].GetDisplayName())
	}

	// Frame 3: slot 1 reused by Dave.
	r3 := s.RosterAt(3)
	if r3[1].GetDisplayName() != "Dave" || r3[1].GetAccountNumber() != 400 {
		t.Errorf("RosterAt(3)[1] = %+v, want Dave", r3[1])
	}

	// Out of range returns nil.
	if s.RosterAt(-1) != nil || s.RosterAt(4) != nil {
		t.Errorf("out-of-range RosterAt should be nil")
	}
}

func TestSessionLoadoutAt(t *testing.T) {
	t.Parallel()

	frames := []*capturepb.Frame{
		frameWithEvents(loadoutEvent(0, "rocket", "det", "sniper")),
		frameWithEvents(),
		frameWithEvents(loadoutEvent(0, "pistol", "det", "sniper")),
	}
	s := conversion.NewSession(&capturepb.EchoArenaHeader{}, frames)

	if l := s.LoadoutAt(0)[0]; l.Weapon != "rocket" || l.Ordnance != "det" || l.TacMod != "sniper" {
		t.Errorf("LoadoutAt(0)[0] = %+v, want rocket/det/sniper", l)
	}
	if l := s.LoadoutAt(1)[0]; l.Weapon != "rocket" {
		t.Errorf("LoadoutAt(1)[0].Weapon = %q, want rocket (carry-forward)", l.Weapon)
	}
	if l := s.LoadoutAt(2)[0]; l.Weapon != "pistol" {
		t.Errorf("LoadoutAt(2)[0].Weapon = %q, want pistol", l.Weapon)
	}
}

func TestSessionGrabAt(t *testing.T) {
	t.Parallel()

	frames := []*capturepb.Frame{
		frameWithEvents(grabEvent(0, "disc", "")),
		frameWithEvents(),
		frameWithEvents(grabEvent(0, "", "wall")),
	}
	s := conversion.NewSession(&capturepb.EchoArenaHeader{}, frames)

	if g := s.GrabAt(0)[0]; g.Left != "disc" || g.Right != "" {
		t.Errorf("GrabAt(0)[0] = %+v, want disc/empty", g)
	}
	if g := s.GrabAt(1)[0]; g.Left != "disc" {
		t.Errorf("GrabAt(1)[0].Left = %q, want disc (carry-forward)", g.Left)
	}
	if g := s.GrabAt(2)[0]; g.Left != "" || g.Right != "wall" {
		t.Errorf("GrabAt(2)[0] = %+v, want empty/wall", g)
	}
}

// TestSessionLoadoutGrabPersistAcrossLeave proves loadout/grab baselines are NOT
// cleared on PlayerLeft, mirroring the forward mapper (mapping.go) which keeps
// prevLoadout/prevGrab across leaves. A slot reused by a player whose value
// equals the departed player's emits no change event, so the carried-forward
// value must remain queryable (regression guard for grab "none" loss).
func TestSessionLoadoutGrabPersistAcrossLeave(t *testing.T) {
	t.Parallel()

	frames := []*capturepb.Frame{
		frameWithEvents(
			joinedEvent(0, 100, "Alice", capturepb.Role_ROLE_BLUE_TEAM, 1, 50),
			loadoutEvent(0, "a", "b", "c"),
			grabEvent(0, "none", "none"),
		),
		frameWithEvents(leftEvent(0, "Alice")), // slot 0 leaves; baselines persist.
		frameWithEvents(joinedEvent(0, 200, "Bob", capturepb.Role_ROLE_BLUE_TEAM, 2, 40)),
	}
	s := conversion.NewSession(&capturepb.EchoArenaHeader{}, frames)

	if l := s.LoadoutAt(2)[0]; l.Weapon != "a" || l.Ordnance != "b" || l.TacMod != "c" {
		t.Errorf("LoadoutAt(2)[0] = %+v, want carried-forward a/b/c across leave", l)
	}
	if g := s.GrabAt(2)[0]; g.Left != "none" || g.Right != "none" {
		t.Errorf("GrabAt(2)[0] = %+v, want carried-forward none/none across leave", g)
	}
	// Identity is cleared on leave and replaced on the new join.
	if r := s.RosterAt(2); r[0].GetDisplayName() != "Bob" {
		t.Errorf("RosterAt(2)[0] = %q, want Bob", r[0].GetDisplayName())
	}
	if r := s.RosterAt(1); len(r) != 0 {
		t.Errorf("RosterAt(1) len = %d, want 0 after Alice left", len(r))
	}
}

func TestSessionScoreAt(t *testing.T) {
	t.Parallel()

	frames := []*capturepb.Frame{
		frameWithEvents(scoreboardEvent(0, 0, 0, 0, "10:00.00")),
		frameWithEvents(),
		frameWithEvents(scoreboardEvent(2, 0, 1, 0, "09:30.50")),
	}
	s := conversion.NewSession(&capturepb.EchoArenaHeader{}, frames)

	if sc := s.ScoreAt(0); sc.BluePoints != 0 || sc.GameClockDisplay != "10:00.00" {
		t.Errorf("ScoreAt(0) = %+v, want clock 10:00.00", sc)
	}
	if sc := s.ScoreAt(1); sc.GameClockDisplay != "10:00.00" {
		t.Errorf("ScoreAt(1).GameClockDisplay = %q, want carry-forward 10:00.00", sc.GameClockDisplay)
	}
	sc := s.ScoreAt(2)
	if sc.BluePoints != 2 || sc.BlueRoundScore != 1 || sc.GameClockDisplay != "09:30.50" {
		t.Errorf("ScoreAt(2) = %+v, want blue=2 blueRound=1 clock=09:30.50", sc)
	}
}
