package conversion

import (
	"strings"
	"testing"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/echotools/tape/pkg/fidelity"
)

// roundtrip_v2_findings_test.go — tests of the round-trip POLICY: which
// messages are verified, how elements are paired, what a tolerance may excuse,
// and that an allowlist entry fires only against a real difference.
//
// The comparison ENGINE is tested in pkg/fidelity. What is tested here is the
// configuration this package hands it, because a perfect differ pointed at the
// wrong messages, or paired by the wrong key, proves nothing.

// TestFrameFieldsAreClassified is the self-proving half of "every field is
// compared" at the FRAME level. The round-trip compares three roots — the
// timestamp, the session and the bones — because those are the three things an
// .echoreplay line carries. Every other field of LobbySessionStateFrame must be
// explicitly recorded as not being file content.
//
// So the day someone adds a field to LobbySessionStateFrame, this test fails by
// name until a human decides which it is. Without it, "we compare the whole
// frame" silently becomes "we compare the part of the frame someone thought of
// in 2026".
func TestFrameFieldsAreClassified(t *testing.T) {
	fields := (&telemetryv1.LobbySessionStateFrame{}).ProtoReflect().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		name := string(fields.Get(i).Name())
		_, verified := VerifiedFrameFields[name]
		_, excluded := NotFileContent[name]
		switch {
		case verified && excluded:
			t.Errorf("LobbySessionStateFrame.%s is classified BOTH as verified and as not-file-content", name)
		case !verified && !excluded:
			t.Errorf("LobbySessionStateFrame.%s is a new frame field that is neither verified by the "+
				"round-trip nor recorded as not-file-content. Decide which it is: if the .echoreplay "+
				"line carries it, add a comparison root; if it is derived, record why in NotFileContent.", name)
		}
	}

	// And the verified ones must actually have a comparison plan.
	schemas, err := EchoReplaySchemas()
	if err != nil {
		t.Fatalf("EchoReplaySchemas: %v", err)
	}
	if len(schemas) != len(VerifiedFrameFields) {
		t.Fatalf("%d verified frame field(s) but %d comparison root(s)", len(VerifiedFrameFields), len(schemas))
	}
	total := 0
	for _, s := range schemas {
		total += len(s.Paths())
		t.Logf("root %-20s %d schema field(s)", s.Name(), len(s.Paths()))
	}
	t.Logf("a frame is compared across %d schema field(s) in total", total)
}

// TestAllowlistEntriesMatchTheSchema guards the allowlist against rot in the
// other direction: an entry naming a path that no longer exists is a hole that
// silently stopped covering anything, and its BUGS.md citation becomes a lie.
func TestAllowlistEntriesMatchTheSchema(t *testing.T) {
	schemas, err := EchoReplaySchemas()
	if err != nil {
		t.Fatalf("EchoReplaySchemas: %v", err)
	}
	known := map[string]bool{}
	for _, s := range schemas {
		for _, p := range s.Paths() {
			known[p] = true
		}
	}
	for entry := range KnownUnpreserved {
		path := entry
		if i := strings.IndexByte(path, '#'); i >= 0 {
			path = path[:i]
		}
		if !known[path] {
			t.Errorf("allowlist entry %q names %q, which is not a field path in any compared schema; "+
				"the entry excuses nothing and its citation is stale", entry, path)
		}
	}
}

// TestKnownUnpreservedDoesNotCoverTheHeldFields pins the fields Andrew's ruling
// keeps failing. If one of them ever lands in the allowlist by accident — or by
// a subtree entry widening over it — this test says so.
func TestKnownUnpreservedDoesNotCoverTheHeldFields(t *testing.T) {
	for _, path := range []string{
		"SessionResponse.blue_round_score",
		"SessionResponse.orange_round_score",
		"SessionResponse.possession[]",
		"SessionResponse.possession[]#count",
		"SessionResponse.client_name",
		"SessionResponse.pause.unpaused_team",
		"SessionResponse.teams[].has_possession",
	} {
		if reason, ok := KnownUnpreserved.Reason(path); ok {
			t.Errorf("%s is excused by the allowlist (%q); it must keep failing until it round-trips", path, reason)
		}
	}
}

// TestFindingsFireOnlyAgainstTheReconstruction is the property the previous
// hand-written probes broke: team_name, team.stats and player.stats used to
// fire on `orig.GetX() != ""` / `!= nil` alone — they never looked at the
// reconstruction. On today's recordings that coincides with real loss, so the
// gate failed for the right reason by accident. But KnownUnpreserved's own
// documented workflow is "when a field gains a v2 home and round-trips, DELETE
// its entry", and with presence-only probes that step would fail the test on
// CORRECT code: a trap for whoever fixes the field next.
//
// A descriptor-driven differ cannot regress to a presence check — it only ever
// compares two values — and this test holds that against the real policy.
func TestFindingsFireOnlyAgainstTheReconstruction(t *testing.T) {
	sessionS, _, _, err := echoSchemas()
	if err != nil {
		t.Fatalf("schemas: %v", err)
	}

	full := func() *enginev1.SessionResponse {
		stats := &enginev1.TeamStats{}
		stats.SetPoints(7)
		stats.SetGoals(2)
		pstats := &enginev1.PlayerStats{}
		pstats.SetPoints(3)
		member := &enginev1.TeamMember{}
		member.SetSlotNumber(1)
		member.SetStats(pstats)
		team := &enginev1.Team{}
		team.SetTeamName("BLUE TEAM")
		team.SetStats(stats)
		team.SetPlayers([]*enginev1.TeamMember{member})
		s := &enginev1.SessionResponse{}
		s.SetTeams([]*enginev1.Team{team})
		return s
	}

	t.Run("preserved_fields_do_not_fire", func(t *testing.T) {
		c := sessionS.NewComparator()
		c.Compare(full(), full(), "frame 0")
		if d := c.Diffs(); len(d) != 0 {
			t.Errorf("the differ reported %d difference(s) on a reconstruction that preserves everything: %v; "+
				"it is testing presence in the original, not fidelity", len(d), d)
		}
	})

	t.Run("dropped_fields_fire", func(t *testing.T) {
		// Same roster and team count; only the three fields are dropped, which
		// is exactly what v2 does to them today.
		member := &enginev1.TeamMember{}
		member.SetSlotNumber(1)
		team := &enginev1.Team{}
		team.SetPlayers([]*enginev1.TeamMember{member})
		stripped := &enginev1.SessionResponse{}
		stripped.SetTeams([]*enginev1.Team{team})

		c := sessionS.NewComparator()
		c.Compare(full(), stripped, "frame 0")
		got := map[string]bool{}
		for _, d := range c.Diffs() {
			got[d.Path] = true
		}
		for _, want := range []string{
			"SessionResponse.teams[].team_name",
			"SessionResponse.teams[].stats#presence",
			"SessionResponse.teams[].stats.points",
			"SessionResponse.teams[].players[].stats#presence",
			"SessionResponse.teams[].players[].stats.points",
		} {
			if !got[want] {
				t.Errorf("%s did not fire on a reconstruction that dropped it; got %v", want, keysOf(got))
			}
		}
	})
}

// TestTeamPairingSurvivesReordering pins the pairing policy: teams are matched
// by their slot set, not by array index, because reconstruction omits empty
// teams. If this regressed to index pairing, every player field would report as
// different and the real losses would be buried.
func TestTeamPairingSurvivesReordering(t *testing.T) {
	sessionS, _, _, err := echoSchemas()
	if err != nil {
		t.Fatalf("schemas: %v", err)
	}
	mk := func(slots ...int32) *enginev1.Team {
		team := &enginev1.Team{}
		var ms []*enginev1.TeamMember
		for _, s := range slots {
			m := &enginev1.TeamMember{}
			m.SetSlotNumber(s)
			m.SetDisplayName("p")
			ms = append(ms, m)
		}
		team.SetPlayers(ms)
		return team
	}
	orig := &enginev1.SessionResponse{}
	orig.SetTeams([]*enginev1.Team{mk(0, 1), mk(2, 3)})
	// Reconstruction: same teams, opposite order, and players within a team
	// reordered too.
	recon := &enginev1.SessionResponse{}
	recon.SetTeams([]*enginev1.Team{mk(3, 2), mk(1, 0)})

	c := sessionS.NewComparator()
	c.Compare(orig, recon, "frame 0")
	if d := c.Diffs(); len(d) != 0 {
		t.Fatalf("reordered but identical rosters reported %d difference(s): %v", len(d), d)
	}
}

// TestToleranceDoesNotExcuseIntegersOrFloat32 holds the line "write better
// tests, not dumber tests" against the one knob that could quietly weaken the
// whole gate.
func TestToleranceDoesNotExcuseIntegersOrFloat32(t *testing.T) {
	sessionS, bonesS, _, err := echoSchemas()
	if err != nil {
		t.Fatalf("schemas: %v", err)
	}

	orig := &enginev1.SessionResponse{}
	orig.SetBluePoints(3)
	orig.SetGameClock(90)
	recon := &enginev1.SessionResponse{}
	recon.SetBluePoints(4) // an integer is never "close enough"
	recon.SetGameClock(90.0000004)

	c := sessionS.NewComparator()
	c.Compare(orig, recon, "frame 0")
	paths := map[string]bool{}
	for _, d := range c.Diffs() {
		paths[d.Path] = true
	}
	if !paths["SessionResponse.blue_points"] {
		t.Errorf("an integer difference was excused: %v", keysOf(paths))
	}
	if paths["SessionResponse.game_clock"] {
		t.Errorf("a float64 narrowing well inside tolerance was reported: %v", keysOf(paths))
	}

	// float32 bone data does not narrow, so it gets no tolerance at all.
	ob, rb := &enginev1.PlayerBonesResponse{}, &enginev1.PlayerBonesResponse{}
	oub, rub := &enginev1.UserBones{}, &enginev1.UserBones{}
	oub.SetBoneT([]float32{1.0})
	rub.SetBoneT([]float32{1.0000001})
	ob.SetUserBones([]*enginev1.UserBones{oub})
	rb.SetUserBones([]*enginev1.UserBones{rub})
	bc := bonesS.NewComparator()
	bc.Compare(ob, rb, "frame 0")
	if len(bc.Diffs()) == 0 {
		t.Errorf("a float32 bone difference was excused by a tolerance; bones do not narrow and must be exact")
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

var _ = fidelity.Diff{}
