package conversion

import (
	"fmt"
	"os"
	"testing"

	"github.com/echotools/tape/v4/pkg/codec"
)

// TestFieldLossAudit measures, on a real echoreplay, which v1 fields that the
// v2 schema drops actually carry data in THIS recording — i.e. how much real
// signal v1->v2 loses here, vs. fields that are always empty (e.g. combat
// loadout in an arena match). Run:
//
//	TAPE_AUDIT_FILE=/path/to/file.echoreplay go test ./pkg/conversion/ \
//	  -run TestFieldLossAudit -v
func TestFieldLossAudit(t *testing.T) {
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

	var (
		framesWithSession int
		playerFrames      int
		headPresent       int
		velPresent        int
		// dropped per-player fields
		weapon, ordnance, tacMod int
		leftHold, rightHold      int
		packetLoss               int
		hasPossession            int
		// dropped session-level fields
		possessionArr int
		shoulderInput int
		payloadData   int
	)

	nonEmpty := func(s string) bool { return s != "" && s != "none" }

	for _, f := range frames {
		sess := f.GetSession()
		if sess == nil {
			continue
		}
		framesWithSession++
		if len(sess.GetPossession()) > 0 {
			possessionArr++
		}
		if sess.GetLeftShoulderPressed() != 0 || sess.GetRightShoulderPressed() != 0 ||
			sess.GetLeftShoulderPressed2() != 0 || sess.GetRightShoulderPressed2() != 0 {
			shoulderInput++
		}
		if sess.GetPayloadMultiplier() != 0 || sess.GetPayloadCheckpoint() != 0 ||
			sess.GetPayloadDistance() != 0 || sess.GetPayloadSpeed() != 0 {
			payloadData++
		}
		for _, team := range sess.GetTeams() {
			for _, p := range team.GetPlayers() {
				playerFrames++
				if p.GetHead() != nil {
					headPresent++
				}
				if len(p.GetVelocity()) >= 3 {
					velPresent++
				}
				if nonEmpty(p.GetWeapon()) {
					weapon++
				}
				if nonEmpty(p.GetOrdnance()) {
					ordnance++
				}
				if nonEmpty(p.GetTacMod()) {
					tacMod++
				}
				if nonEmpty(p.GetLeftHoldingOnto()) {
					leftHold++
				}
				if nonEmpty(p.GetRightHoldingOnto()) {
					rightHold++
				}
				if p.GetPacketLossRatio() != 0 {
					packetLoss++
				}
				if p.GetHasPossession() {
					hasPossession++
				}
			}
		}
	}

	pf := func(n int) string {
		if playerFrames == 0 {
			return "n/a"
		}
		return fmt.Sprintf("%5.1f%% (%d)", 100*float64(n)/float64(playerFrames), n)
	}
	sf := func(n int) string {
		if framesWithSession == 0 {
			return "n/a"
		}
		return fmt.Sprintf("%5.1f%% (%d)", 100*float64(n)/float64(framesWithSession), n)
	}

	t.Logf("file=%s", path)
	t.Logf("frames=%d framesWithSession=%d player-frames=%d", len(frames), framesWithSession, playerFrames)
	t.Logf("KEPT in v2:   head-pose=%s  velocity=%s", pf(headPresent), pf(velPresent))
	t.Logf("DROPPED per-player (how often it had data here):")
	t.Logf("  weapon=%s ordnance=%s tac_mod=%s", pf(weapon), pf(ordnance), pf(tacMod))
	t.Logf("  left_holding=%s right_holding=%s", pf(leftHold), pf(rightHold))
	t.Logf("  packet_loss!=0=%s has_possession=%s", pf(packetLoss), pf(hasPossession))
	t.Logf("DROPPED session-level (frames with data):")
	t.Logf("  possession[]=%s shoulder_input=%s payload=%s", sf(possessionArr), sf(shoulderInput), sf(payloadData))
}
