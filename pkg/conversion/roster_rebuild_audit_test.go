package conversion

import (
	"fmt"
	"os"
	"testing"

	"github.com/echotools/tape/pkg/codec"
	"github.com/echotools/tape/pkg/events"
)

// TestRosterRebuildAudit answers: can the slot->identity roster be rebuilt from
// a tape's own PlayerJoined/PlayerLeft events alone (the per-frame PlayerState
// is anonymized — slot only)? It drives the real join/leave sensors over a real
// echoreplay, maintains a slot->name map exactly as a tape reader would, and
// compares it against the ground-truth name in every frame.
//
//	TAPE_AUDIT_FILE=/path go test ./pkg/conversion/ -run TestRosterRebuildAudit -v
func TestRosterRebuildAudit(t *testing.T) {
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

	joinSensor := events.NewPlayerJoinSensor()
	leaveSensor := events.NewPlayerLeaveSensor()

	slotName := map[int32]string{}
	var joins, leaves int
	distinctNames := map[string]struct{}{}

	var (
		total       int // player-frames with a non-empty ground-truth name
		matched     int // ...where replay recovered the correct name
		wrongName   int // replay had a name, but the wrong one (slot reuse miss)
		missingName int // replay had no name for an occupied slot
		emptyGT     int // player-frames whose source name is itself empty
	)

	for _, f := range frames {
		// Drain joins for this frame.
		for {
			e := joinSensor.AddFrame(f)
			if e == nil {
				break
			}
			if pj := e.GetPlayerJoined(); pj != nil {
				slot := pj.GetPlayer().GetSlotNumber()
				name := pj.GetPlayer().GetDisplayName()
				slotName[slot] = name
				joins++
				if name != "" {
					distinctNames[name] = struct{}{}
				}
			}
		}
		// Drain leaves for this frame.
		for {
			e := leaveSensor.AddFrame(f)
			if e == nil {
				break
			}
			if pl := e.GetPlayerLeft(); pl != nil {
				delete(slotName, pl.GetPlayerSlot())
				leaves++
			}
		}
		// Compare reconstructed roster to ground truth in this frame.
		sess := f.GetSession()
		if sess == nil {
			continue
		}
		for _, team := range sess.GetTeams() {
			for _, p := range team.GetPlayers() {
				gt := p.GetDisplayName()
				if gt == "" {
					emptyGT++
					continue
				}
				total++
				got, ok := slotName[p.GetSlotNumber()]
				switch {
				case !ok || got == "":
					missingName++
				case got == gt:
					matched++
				default:
					wrongName++
				}
			}
		}
	}

	pct := func(n int) string {
		if total == 0 {
			return "n/a"
		}
		return fmt.Sprintf("%6.2f%% (%d)", 100*float64(n)/float64(total), n)
	}

	t.Logf("file=%s frames=%d", path, len(frames))
	t.Logf("events: PlayerJoined=%d PlayerLeft=%d distinct names=%d", joins, leaves, len(distinctNames))
	t.Logf("player-frames with empty source name (anonymous at source)=%d", emptyGT)
	t.Logf("ROSTER REBUILD from events vs ground truth (of %d named player-frames):", total)
	t.Logf("  correctly recovered : %s", pct(matched))
	t.Logf("  wrong name (reuse?) : %s", pct(wrongName))
	t.Logf("  no name for slot    : %s", pct(missingName))
}
