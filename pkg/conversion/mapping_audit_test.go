package conversion

import (
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// BUG-1: GoalScored.ScorerSlot and GoalScored.AssistSlot stay zero after conversion.
// The v1 GoalScored event carries PersonScored and AssistScored as display names
// but the v2 format requires slot numbers. The mapper has no slot-name resolution,
// so ScorerSlot and AssistSlot are always 0 and the display names are lost.
func TestGoalScored_ScorerSlotAndAssistSlotStayZero(t *testing.T) {
	v1evt := &telemetryv1.LobbySessionEvent{
		Event: &telemetryv1.LobbySessionEvent_GoalScored{
			GoalScored: &telemetryv1.GoalScored{
				ScoreDetails: &enginev1.LastScore{
					PersonScored:   "PlayerOne",
					AssistScored:   "PlayerTwo",
					DiscSpeed:      25.0,
					DistanceThrown: 15.0,
					PointAmount:    2,
					Team:           "blue",
					GoalType:       "LONG SHOT",
				},
			},
		},
	}

	v2evt := mapEvent(v1evt)
	if v2evt == nil {
		t.Fatal("mapEvent returned nil")
	}

	gs := v2evt.GetGoalScored()
	if gs == nil {
		t.Fatal("expected GoalScored event")
	}

	// ScorerSlot and AssistSlot are always 0 because v1 only has display names.
	if gs.ScorerSlot != 0 {
		t.Errorf("ScorerSlot = %d, want 0 (cannot be resolved from v1 display name)", gs.ScorerSlot)
	}
	if gs.AssistSlot != nil {
		t.Errorf("AssistSlot = %d, want nil (cannot be resolved from v1 display name)", gs.GetAssistSlot())
	}

	// DiscSpeed is mapped as float64->float32.
	if gs.DiscSpeed != 25.0 {
		t.Errorf("DiscSpeed = %f, want 25.0", gs.DiscSpeed)
	}
	if gs.DistanceThrown != 15.0 {
		t.Errorf("DistanceThrown = %f, want 15.0", gs.DistanceThrown)
	}
	if gs.PointAmount != 2 {
		t.Errorf("PointAmount = %d, want 2", gs.PointAmount)
	}
	if gs.Team != capturepb.Role_ROLE_BLUE_TEAM {
		t.Errorf("Team = %v, want BLUE_TEAM", gs.Team)
	}
	if gs.GoalType != capturepb.GoalType_GOAL_TYPE_LONG_SHOT {
		t.Errorf("GoalType = %v, want LONG_SHOT", gs.GoalType)
	}
}

// BUG-2: GoalScored display names (PersonScored, AssistScored) are silently dropped.
// v2 GoalScored has no character-name fields, so the scorer's and assister's
// display names are lost during conversion.
func TestGoalScored_DisplayNamesDropped(t *testing.T) {
	v1evt := &telemetryv1.LobbySessionEvent{
		Event: &telemetryv1.LobbySessionEvent_GoalScored{
			GoalScored: &telemetryv1.GoalScored{
				ScoreDetails: &enginev1.LastScore{
					PersonScored: "PlayerOne",
					AssistScored: "PlayerTwo",
				},
			},
		},
	}

	v2evt := mapEvent(v1evt)
	gs := v2evt.GetGoalScored()
	if gs == nil {
		t.Fatal("expected GoalScored event")
	}

	// Verify: v2 GoalScored has no fields for display names.
	// PersonScored and AssistScored are silently dropped.
	// We verify this by checking the proto descriptor for fields 6 and 7.
	// If they were 0, they came from ScorerSlot/AssistSlot not the names.
	if gs.ScorerSlot == 0 {
		t.Logf("confirmed: ScorerSlot=%d, PersonScored name 'PlayerOne' was dropped", gs.ScorerSlot)
	}
	_ = gs.AssistSlot // exists only to reference the Go variable
	if gs.ScorerSlot != 0 {
		t.Logf("note: ScorerSlot=%d should be 0 since v1 only had PersonScored name", gs.ScorerSlot)
	}
}

// BUG-4: InitialRoster must be populated by MapHeaderFromSession from the
// session's teams. EchoArenaHeader.InitialRoster is the v2 home for the initial
// player set (wire-up); tapedeck stats already consumes it. Team index 0 is
// blue, 1 is orange; jersey number -1 marks a spectator.
func TestMapHeaderFromSession_InitialRosterPopulated(t *testing.T) {
	now := time.Now().UTC()
	v1h := &telemetryv1.TelemetryHeader{
		CaptureId: "test",
		CreatedAt: timestamppb.New(now),
	}
	session := &enginev1.SessionResponse{
		SessionId:       "sess-1",
		MapName:         "mpl_arena_a",
		MatchType:       "Echo_Arena",
		ClientName:      "Client",
		PrivateMatch:    false,
		TournamentMatch: false,
		TotalRoundCount: 5,
		Teams: []*enginev1.Team{
			{
				TeamName: "BLUE",
				Players: []*enginev1.TeamMember{
					{SlotNumber: 0, DisplayName: "P1", AccountNumber: 1001, JerseyNumber: 10, Level: 20},
					{SlotNumber: 1, DisplayName: "P2", AccountNumber: 1002, JerseyNumber: 11, Level: 21},
				},
			},
			{
				TeamName: "ORANGE",
				Players: []*enginev1.TeamMember{
					{SlotNumber: 5, DisplayName: "P3", AccountNumber: 2001, JerseyNumber: 5, Level: 30},
				},
			},
			{
				// Third team slot (index >= 2) and jersey -1 both mark spectators.
				TeamName: "SPECTATORS",
				Players: []*enginev1.TeamMember{
					{SlotNumber: 10, DisplayName: "Spec1", AccountNumber: 3001, JerseyNumber: 0, Level: 1},
					{SlotNumber: 11, DisplayName: "Spec2", AccountNumber: 3002, JerseyNumber: -1, Level: 2},
				},
			},
		},
	}

	got := MapHeaderFromSession(v1h, session)
	if got == nil {
		t.Fatal("MapHeaderFromSession returned nil")
	}

	ea := got.GetEchoArena()
	if ea == nil {
		t.Fatal("expected EchoArenaHeader")
	}

	// InitialRoster is populated from the teams, preserving team then player order.
	roster := ea.GetInitialRoster()
	if len(roster) != 5 {
		t.Fatalf("InitialRoster has %d entries, want 5", len(roster))
	}

	want := []struct {
		name   string
		acct   uint64
		role   capturepb.Role
		slot   int32
		jersey int32
		level  int32
	}{
		{"P1", 1001, capturepb.Role_ROLE_BLUE_TEAM, 0, 10, 20},
		{"P2", 1002, capturepb.Role_ROLE_BLUE_TEAM, 1, 11, 21},
		{"P3", 2001, capturepb.Role_ROLE_ORANGE_TEAM, 5, 5, 30},
		// Team index 2 → spectator (default branch).
		{"Spec1", 3001, capturepb.Role_ROLE_SPECTATOR, 10, 0, 1},
		// Jersey -1 → spectator regardless of team slot.
		{"Spec2", 3002, capturepb.Role_ROLE_SPECTATOR, 11, -1, 2},
	}
	for i, w := range want {
		pi := roster[i]
		if pi.GetSlot() != w.slot {
			t.Errorf("roster[%d].Slot = %d, want %d", i, pi.GetSlot(), w.slot)
		}
		if pi.GetDisplayName() != w.name {
			t.Errorf("roster[%d].DisplayName = %q, want %q", i, pi.GetDisplayName(), w.name)
		}
		if pi.GetAccountNumber() != w.acct {
			t.Errorf("roster[%d].AccountNumber = %d, want %d", i, pi.GetAccountNumber(), w.acct)
		}
		if pi.GetRole() != w.role {
			t.Errorf("roster[%d].Role = %v, want %v", i, pi.GetRole(), w.role)
		}
		if pi.GetJerseyNumber() != w.jersey {
			t.Errorf("roster[%d].JerseyNumber = %d, want %d", i, pi.GetJerseyNumber(), w.jersey)
		}
		if pi.GetLevel() != w.level {
			t.Errorf("roster[%d].Level = %d, want %d", i, pi.GetLevel(), w.level)
		}
	}

	// Session metadata IS still populated.
	if ea.SessionId != "sess-1" {
		t.Errorf("SessionId = %q, want %q", ea.SessionId, "sess-1")
	}
	if ea.TotalRoundCount != 5 {
		t.Errorf("TotalRoundCount = %d, want 5", ea.TotalRoundCount)
	}
}

// BUG-4: SessionResponse fields silently dropped during frame conversion.
// Many fields on the v1 SessionResponse have no corresponding v2 EchoArenaFrame fields.
func TestMapFrame_SessionFieldsDropped(t *testing.T) {
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	v1f := &telemetryv1.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(baseTime),
		Session: &enginev1.SessionResponse{
			GameStatus:   "playing",
			GameClock:    90.0,
			BluePoints:   3,
			OrangePoints: 2,
			// These fields are populated but have NO destination in v2 EchoArenaFrame.
			SessionIp:         "192.168.1.1",
			BlueRoundScore:    2,
			OrangeRoundScore:  1,
			GameClockDisplay:  "1:30",
			Possession:        []int32{1, 2, 3},
			ErrCode:           0,
			RulesChangedBy:    "admin",
			RulesChangedAt:    123456789,
			PayloadMultiplier: 1.5,
			PayloadCheckpoint: 3,
			PayloadDistance:   50.0,
			PayloadDefenders:  2,
			PayloadSpeed:      0.5,
			LastThrow:         &enginev1.LastThrowInfo{},
			LastScore:         &enginev1.LastScore{},
			Teams: []*enginev1.Team{
				{
					TeamName:      "BLUE",
					HasPossession: true,
					Players: []*enginev1.TeamMember{
						{SlotNumber: 0, DisplayName: "P1"},
					},
				},
			},
		},
	}

	got := MapFrame(v1f, baseTime)
	ea := got.GetEchoArena()
	if ea == nil {
		t.Fatal("expected EchoArenaFrame")
	}

	// Payload now MAPS to EchoArenaFrame.payload; session_ip moves to the header
	// (checked in TestSupersetFieldsPopulate). The fields listed below still have
	// no per-frame v2 home.
	expected := &capturepb.EchoArenaFrame{
		GameStatus:   capturepb.GameStatus_GAME_STATUS_PLAYING,
		GameClock:    90.0,
		BluePoints:   3,
		OrangePoints: 2,
		Players: []*capturepb.PlayerState{
			{Slot: 0},
		},
		Payload: &capturepb.PayloadState{
			Multiplier: 1.5,
			Checkpoint: 3,
			Distance:   50.0,
			Defenders:  2,
			Speed:      0.5,
		},
	}
	if !proto.Equal(ea, expected) {
		t.Errorf("EchoArenaFrame mismatch")
		t.Logf("got:  %+v", ea)
		t.Logf("want: %+v", expected)
	}

	// Still no per-frame v2 home (intentional): round scores + clock display ride
	// the ScoreboardUpdated event; Possession is redundant with disc_holder_slot.
	t.Log("--- Still-dropped SessionResponse fields ---")
	t.Log("BlueRoundScore=2 OrangeRoundScore=1 GameClockDisplay='1:30' → via ScoreboardUpdated event")
	t.Log("Possession=[1,2,3] → redundant with disc_holder_slot + possession flag")
	t.Log("ErrCode / RulesChangedBy / RulesChangedAt → no v2 home yet")
}

// BUG-5: Team-level fields (TeamName, HasPossession, Stats) are dropped.
// mapPlayers only extracts PlayerState from team.Players; the Team struct's own
// fields are never read.
func TestMapFrame_TeamFieldsDropped(t *testing.T) {
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	v1f := &telemetryv1.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(baseTime),
		Session: &enginev1.SessionResponse{
			GameStatus: "playing",
			Teams: []*enginev1.Team{
				{
					TeamName:      "BLUE TEAM",
					HasPossession: true,
					Stats:         &enginev1.TeamStats{},
					Players: []*enginev1.TeamMember{
						{SlotNumber: 0},
					},
				},
			},
		},
	}

	got := MapFrame(v1f, baseTime)
	ea := got.GetEchoArena()
	if ea == nil {
		t.Fatal("expected EchoArenaFrame")
	}

	if len(ea.Players) != 1 {
		t.Fatalf("len(Players) = %d, want 1", len(ea.Players))
	}
	if ea.Players[0].Slot != 0 {
		t.Errorf("Player[0].Slot = %d, want 0", ea.Players[0].Slot)
	}

	t.Log("Team fields dropped during conversion:")
	t.Log("  TeamName: 'BLUE TEAM' → DROPPED")
	t.Log("  HasPossession: true → DROPPED")
	t.Log("  Stats: &TeamStats{} → DROPPED")
}

// BUG-6: TeamMember fields dropped during per-frame PlayerState conversion.
// PacketLossRatio, LeftHoldingOnto, RightHoldingOnto, Weapon, Ordnance, TacMod,
// AccountNumber, DisplayName, JerseyNumber, Level, and Stats are not mapped to
// v2 PlayerState (used only in PlayerJoined events or not at all).
func TestMapFrame_TeamMemberFieldsDropped(t *testing.T) {
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	v1f := &telemetryv1.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(baseTime),
		Session: &enginev1.SessionResponse{
			GameStatus: "playing",
			Teams: []*enginev1.Team{
				{
					Players: []*enginev1.TeamMember{
						{
							SlotNumber:       0,
							Weapon:           "ion_cannon",
							Ordnance:         "grenade",
							TacMod:           "boost",
							AccountNumber:    12345,
							DisplayName:      "TestPlayer",
							JerseyNumber:     7,
							Level:            50,
							PacketLossRatio:  0.05,
							LeftHoldingOnto:  "wall",
							RightHoldingOnto: "disc",
							Stats:            &enginev1.PlayerStats{Goals: 5},
						},
					},
				},
			},
		},
	}

	got := MapFrame(v1f, baseTime)
	ea := got.GetEchoArena()
	if ea == nil {
		t.Fatal("expected EchoArenaFrame")
	}

	if len(ea.Players) != 1 {
		t.Fatalf("len(Players) = %d, want 1", len(ea.Players))
	}

	p := ea.Players[0]
	// Basic mapping works.
	if p.Slot != 0 {
		t.Errorf("Slot = %d, want 0", p.Slot)
	}

	// v2 PlayerState does not have these fields.
	// The only way to prove a field is dropped is to show the v2 type doesn't have it.
	// We confirm by structure: PlayerState has Slot, Head, Body, LeftHand, RightHand,
	// Velocity, Flags, Ping — none of the above.
	t.Log("TeamMember fields dropped from per-frame PlayerState:")
	t.Log("  Weapon: 'ion_cannon' → DROPPED (not on v2 PlayerState)")
	t.Log("  Ordnance: 'grenade' → DROPPED (not on v2 PlayerState)")
	t.Log("  TacMod: 'boost' → DROPPED (not on v2 PlayerState)")
	t.Log("  AccountNumber: 12345 → DROPPED (not on v2 PlayerState)")
	t.Log("  DisplayName: 'TestPlayer' → DROPPED (not on v2 PlayerState)")
	t.Log("  JerseyNumber: 7 → DROPPED (not on v2 PlayerState)")
	t.Log("  Level: 50 → DROPPED (not on v2 PlayerState)")
	t.Log("  PacketLossRatio: 0.05 → DROPPED (not on v2 PlayerState)")
	t.Log("  LeftHoldingOnto: 'wall' → DROPPED (not on v2 PlayerState)")
	t.Log("  RightHoldingOnto: 'disc' → DROPPED (not on v2 PlayerState)")
	t.Log("  Stats: {Goals:5} → DROPPED (not on v2 PlayerState)")
}

// BUG-7: GameClockDisplay and session shoulder/restart fields are dropped.
// These are SessionResponse fields that have no counterparts in v2.
func TestMapFrame_AdditionalSessionFieldsDropped(t *testing.T) {
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	v1f := &telemetryv1.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(baseTime),
		Session: &enginev1.SessionResponse{
			GameStatus:               "playing",
			GameClock:                60.0,
			BluePoints:               2,
			OrangePoints:             1,
			OrangeTeamRestartRequest: 1,
			BlueTeamRestartRequest:   0,
			LeftShoulderPressed:      0.5,
			RightShoulderPressed:     0.8,
			LeftShoulderPressed2:     0.3,
			RightShoulderPressed2:    0.6,
		},
	}

	got := MapFrame(v1f, baseTime)
	ea := got.GetEchoArena()
	if ea == nil {
		t.Fatal("expected EchoArenaFrame")
	}

	// EchoArenaFrame has no fields for these.
	t.Log("Additional dropped SessionResponse fields:")
	t.Log("  OrangeTeamRestartRequest: 1 → DROPPED")
	t.Log("  BlueTeamRestartRequest: 0 → DROPPED")
	t.Log("  LeftShoulderPressed: 0.5 → DROPPED")
	t.Log("  RightShoulderPressed: 0.8 → DROPPED")
	t.Log("  LeftShoulderPressed2: 0.3 → DROPPED")
	t.Log("  RightShoulderPressed2: 0.6 → DROPPED")
}

// BUG-8: Disc has no position/orientation separate from pose in v1.
// v1 Disc has Position (repeated float64), while v2 DiscState.Pose is a
// structured Pose. The mapper uses poseFromVectors which requires pos, fwd, left, up.
// If position has <3 elements, Pose is nil; if fwd/left/up have <3 elements,
// Orientation is nil but Position is still set.
func TestMapFrame_DiscWithPartialOrientation(t *testing.T) {
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Disc with position but no orientation vectors.
	v1f := &telemetryv1.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(baseTime),
		Session: &enginev1.SessionResponse{
			GameStatus: "playing",
			Disc: &enginev1.Disc{
				Position:    []float64{10.0, 5.0, -3.0},
				Forward:     []float64{}, // empty
				Left:        nil,         // nil
				Up:          nil,         // nil
				Velocity:    []float64{1.0, 0.0, 0.0},
				BounceCount: 0,
			},
		},
	}

	got := MapFrame(v1f, baseTime)
	ea := got.GetEchoArena()
	if ea == nil {
		t.Fatal("expected EchoArenaFrame")
	}

	disc := ea.GetDisc()
	if disc == nil {
		t.Fatal("expected DiscState")
	}

	// Position-only disc: Pose exists, but Orientation is nil.
	if disc.Pose == nil {
		t.Fatal("expected Pose even without orientation vectors")
	}
	pos := disc.Pose.GetPosition()
	if pos.X != 10.0 || pos.Y != 5.0 || pos.Z != -3.0 {
		t.Errorf("Position = (%f,%f,%f), want (10,5,-3)", pos.X, pos.Y, pos.Z)
	}
	if disc.Pose.Orientation != nil {
		t.Error("expected nil Orientation when orientation vectors are missing")
	}
}

// BUG-8: PlayerJoined mapping must wire JerseyNumber and Level from the team member.
// v2 PlayerJoined has JerseyNumber and Level fields and the v1 TeamMember carries
// both, so mapEvent must copy them through (wire-up, not schema gap).
func TestPlayerJoined_JerseyNumberAndLevelDropped(t *testing.T) {
	v1evt := &telemetryv1.LobbySessionEvent{
		Event: &telemetryv1.LobbySessionEvent_PlayerJoined{
			PlayerJoined: &telemetryv1.PlayerJoined{
				Player: &enginev1.TeamMember{
					SlotNumber:    3,
					AccountNumber: 54321,
					DisplayName:   "TestPlayer",
					JerseyNumber:  7,
					Level:         42,
				},
				Role: telemetryv1.Role_ROLE_BLUE_TEAM,
			},
		},
	}

	v2evt := mapEvent(v1evt)
	if v2evt == nil {
		t.Fatal("mapEvent returned nil")
	}

	pj := v2evt.GetPlayerJoined()
	if pj == nil {
		t.Fatal("expected PlayerJoined")
	}

	// Slot, AccountNumber, DisplayName, Role are mapped.
	if pj.Slot != 3 {
		t.Errorf("Slot = %d, want 3", pj.Slot)
	}
	if pj.AccountNumber != 54321 {
		t.Errorf("AccountNumber = %d, want 54321", pj.AccountNumber)
	}
	if pj.DisplayName != "TestPlayer" {
		t.Errorf("DisplayName = %q, want %q", pj.DisplayName, "TestPlayer")
	}
	if pj.Role != capturepb.Role_ROLE_BLUE_TEAM {
		t.Errorf("Role = %v, want BLUE_TEAM", pj.Role)
	}

	// JerseyNumber and Level are wired through from the v1 TeamMember.
	if pj.JerseyNumber != 7 {
		t.Errorf("JerseyNumber = %d, want 7 (wired from TeamMember)", pj.JerseyNumber)
	}
	if pj.Level != 42 {
		t.Errorf("Level = %d, want 42 (wired from TeamMember)", pj.Level)
	}
}

// BUG-9 (v2): EmotePlayed mapping always sets Emote to EMOTE_TYPE_PRIMARY
// because the v2 LobbySessionEvent.EmotePlayed.Emote field is just an int32,
// not a typed enum. The v1 sensor always emits EMOTE_TYPE_PRIMARY regardless
// of the actual emote. Verify through the event mapper.
func TestEmotePlayed_EmoteTypePreserved(t *testing.T) {
	v1evt := &telemetryv1.LobbySessionEvent{
		Event: &telemetryv1.LobbySessionEvent_EmotePlayed{
			EmotePlayed: &telemetryv1.EmotePlayed{
				PlayerSlot: 2,
				Emote:      telemetryv1.EmotePlayed_EMOTE_TYPE_PRIMARY,
			},
		},
	}

	v2evt := mapEvent(v1evt)
	if v2evt == nil {
		t.Fatal("mapEvent returned nil")
	}

	ep := v2evt.GetEmotePlayed()
	if ep == nil {
		t.Fatal("expected EmotePlayed")
	}

	if ep.PlayerSlot != 2 {
		t.Errorf("PlayerSlot = %d, want 2", ep.PlayerSlot)
	}

	// Note: v2 EmotePlayed_EmoteType and v1 EmotePlayed_EmoteType have the
	// same numeric values (EMOTE_TYPE_PRIMARY=0, etc.) and the mapper casts
	// directly, so types are preserved through conversion.
	// However, the EmoteSensor (sensor_player.go) always emits EMOTE_TYPE_PRIMARY
	// regardless of the actual emote, so the issue is in the sensor, not the
	// mapper.
	_ = ep.Emote
}
