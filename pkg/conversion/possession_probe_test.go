package conversion

import (
	"os"
	"testing"

	"github.com/echotools/tape/pkg/codec"
)

// TestPossessionProbe characterizes the session-level possession[] array on a
// real recording: its length, what index possession[1] actually refers to
// (slot vs global player index), and whether it carries a possessor that the
// per-player has_possession bool (and thus v2's disc_holder_slot) does NOT.
func TestPossessionProbe(t *testing.T) {
	path := os.Getenv("TAPE_AUDIT_FILE")
	if path == "" {
		t.Skip("set TAPE_AUDIT_FILE=/path/to.echoreplay to run this audit")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no audit file: %v", err)
	}
	r, err := codec.NewEchoReplayReader(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	frames, err := r.ReadFrames()
	_ = r.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	lenDist := map[int]int{}
	team0vals := map[int32]int{} // distribution of possession[0]
	var (
		withSession                                          int
		matchSlot, matchGlobal, matchTeamIdx                 int
		possHasSlotButNoBool, boolButPossNone, bothAgreeSlot int
		bothNone                                             int
	)
	samples := 0

	for _, f := range frames {
		sess := f.GetSession()
		if sess == nil {
			continue
		}
		withSession++
		poss := sess.GetPossession()
		lenDist[len(poss)]++

		// Find the has_possession player.
		hpSlot, hpTeam, hpIdxInTeam, hpGlobal := int32(-1), int32(-1), int32(-1), int32(-1)
		gi := int32(0)
		for ti, team := range sess.GetTeams() {
			for pi, p := range team.GetPlayers() {
				if p.GetHasPossession() {
					hpSlot = p.GetSlotNumber()
					hpTeam = int32(ti)
					hpIdxInTeam = int32(pi)
					hpGlobal = gi
				}
				gi++
			}
		}

		if len(poss) < 2 {
			continue
		}
		p0, p1 := poss[0], poss[1]
		team0vals[p0]++

		if p1 == hpSlot && hpSlot >= 0 {
			matchSlot++
		}
		if p1 == hpGlobal && hpGlobal >= 0 {
			matchGlobal++
		}
		if p1 == hpIdxInTeam && hpIdxInTeam >= 0 {
			matchTeamIdx++
		}

		possSet := p1 >= 0
		boolSet := hpSlot >= 0
		switch {
		case possSet && boolSet && p1 == hpSlot:
			bothAgreeSlot++
		case possSet && !boolSet:
			possHasSlotButNoBool++
		case !possSet && boolSet:
			boolButPossNone++
		case !possSet && !boolSet:
			bothNone++
		}

		if samples < 10 && (possSet || boolSet) {
			samples++
			t.Logf("sample: possession=%v  has_possession{slot=%d team=%d idxInTeam=%d global=%d}",
				poss, hpSlot, hpTeam, hpIdxInTeam, hpGlobal)
		}
	}

	t.Logf("frames=%d withSession=%d", len(frames), withSession)
	t.Logf("possession[] length distribution: %v", lenDist)
	t.Logf("possession[0] value distribution (team?): %v", team0vals)
	t.Logf("possession[1] == has_possession player's: slot=%d  global-idx=%d  team-idx=%d",
		matchSlot, matchGlobal, matchTeamIdx)
	t.Logf("agreement: bothAgreeSlot=%d  poss-has-slot-but-no-bool=%d  bool-but-poss-none=%d  bothNone=%d",
		bothAgreeSlot, possHasSlotButNoBool, boolButPossNone, bothNone)
}
