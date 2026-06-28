package conversion

import (
	"testing"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"

	"github.com/echotools/tape/pkg/events"
)

// TestRoleResolution_BothPathsAgreeOnSpectator locks the two role-resolution
// paths together. A player in team index 2 with a real jersey number (not -1)
// is a spectator. The InitialRoster path (MapHeaderFromSession -> buildInitialRoster
// -> rosterRole) already resolved this correctly, but the PlayerJoined path
// (PlayerJoinSensor.determinePlayerRole -> mapEvent) used to return ORANGE for
// any team index != 0. Both must now yield SPECTATOR so InitialRoster.Role and
// PlayerJoined.Role can never diverge for the same player.
func TestRoleResolution_BothPathsAgreeOnSpectator(t *testing.T) {
	t.Parallel()

	const specSlot = 7
	session := &enginev1.SessionResponse{
		Teams: []*enginev1.Team{
			{TeamName: "BLUE"},
			{TeamName: "ORANGE"},
			{
				TeamName: "SPECTATORS",
				Players: []*enginev1.TeamMember{
					// Team index 2 + a real jersey number (not -1): a spectator.
					{SlotNumber: specSlot, DisplayName: "Spec", AccountNumber: 9001, JerseyNumber: 3, Level: 4},
				},
			},
		},
	}

	// Path A: InitialRoster via MapHeaderFromSession.
	hdr := MapHeaderFromSession(&telemetryv1.TelemetryHeader{}, session)
	if hdr == nil {
		t.Fatal("MapHeaderFromSession returned nil")
	}
	roster := hdr.GetEchoArena().GetInitialRoster()
	if len(roster) != 1 {
		t.Fatalf("InitialRoster has %d entries, want 1", len(roster))
	}
	if got := roster[0].GetRole(); got != capturepb.Role_ROLE_SPECTATOR {
		t.Errorf("InitialRoster path: Role = %v, want ROLE_SPECTATOR", got)
	}

	// Path B: PlayerJoined via the sensor then mapEvent.
	sensor := events.NewPlayerJoinSensor()
	frame := &telemetryv1.LobbySessionStateFrame{Session: session}
	var joined *capturepb.PlayerJoined
	for {
		v1evt := sensor.AddFrame(frame)
		if v1evt == nil {
			break
		}
		v2evt := mapEvent(v1evt)
		if v2evt == nil {
			continue
		}
		if pj, ok := v2evt.GetEvent().(*capturepb.EchoEvent_PlayerJoined); ok {
			joined = pj.PlayerJoined
		}
	}
	if joined == nil {
		t.Fatal("PlayerJoined path: sensor produced no PlayerJoined event")
	}
	if joined.GetSlot() != specSlot {
		t.Fatalf("PlayerJoined path: Slot = %d, want %d", joined.GetSlot(), specSlot)
	}
	if got := joined.GetRole(); got != capturepb.Role_ROLE_SPECTATOR {
		t.Errorf("PlayerJoined path: Role = %v, want ROLE_SPECTATOR", got)
	}
}
