package conversion_test

import (
	"testing"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
)

// TestMeasureFindingsComparesAgainstReconstruction guards the three team-level
// probes against regressing to presence-in-original checks.
//
// WHY IT EXISTS: team_name, team.stats and player.stats used to fire on
// `orig.GetX() != ""` / `!= nil` alone — they never looked at the
// reconstruction. On today's recordings that coincides with real loss, so the
// gate fails for the right reason by accident. But knownUnpreserved's own
// documented workflow is "when a field gains a v2 home and round-trips, DELETE
// its entry", and with presence-only probes that step would fail the test on
// CORRECT code: a trap for whoever fixes the field next.
//
// BAC: a findings probe fires when and only when the reconstruction differs
// from the original.
func TestMeasureFindingsComparesAgainstReconstruction(t *testing.T) {
	teamStats := func() *enginev1.TeamStats { return &enginev1.TeamStats{Points: 7, Goals: 2} }
	playerStats := func() *enginev1.PlayerStats { return &enginev1.PlayerStats{Points: 3} }

	full := func() *enginev1.SessionResponse {
		return &enginev1.SessionResponse{
			Teams: []*enginev1.Team{{
				TeamName: "BLUE TEAM",
				Stats:    teamStats(),
				Players: []*enginev1.TeamMember{{
					SlotNumber: 1,
					Stats:      playerStats(),
				}},
			}},
		}
	}

	t.Run("preserved_fields_do_not_fire", func(t *testing.T) {
		ta := newTally()
		measureFindings(ta, full(), full())
		for _, f := range []string{"team_name", "team.stats", "player.stats"} {
			if n := ta.finding[f]; n != 0 {
				t.Errorf("%s fired %d time(s) on a reconstruction that preserves it (e.g. %s); "+
					"the probe is testing presence in the original, not fidelity", f, n, ta.findingExample[f])
			}
		}
	})

	t.Run("dropped_fields_fire", func(t *testing.T) {
		// Same roster and team count; only the three fields are dropped, which
		// is exactly what v2 does to them today.
		stripped := &enginev1.SessionResponse{
			Teams: []*enginev1.Team{{
				Players: []*enginev1.TeamMember{{SlotNumber: 1}},
			}},
		}
		ta := newTally()
		measureFindings(ta, full(), stripped)
		for _, f := range []string{"team_name", "team.stats", "player.stats"} {
			if ta.finding[f] != 1 {
				t.Errorf("%s fired %d time(s) on a reconstruction that dropped it, want 1", f, ta.finding[f])
			}
		}
	})
}
