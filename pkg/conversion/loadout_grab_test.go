package conversion

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/echotools/tape/v4/pkg/codec"
)

// TestLoadoutGrabReconstruct proves the LoadoutChanged/GrabChanged events the
// converter emits losslessly capture per-player loadout and grab: replaying them
// frame-by-frame reconstructs the exact value the v1 source had for every
// player on every frame. Committed sample by default; TAPE_AUDIT_FILE to switch.
func TestLoadoutGrabReconstruct(t *testing.T) {
	src := os.Getenv("TAPE_AUDIT_FILE")
	if src == "" {
		src = filepath.Join("..", "..", "testdata", "sample.echoreplay")
	}
	r, err := codec.NewEchoReplayReader(src)
	if err != nil {
		t.Skipf("no sample: %v", err)
	}
	frames, err := r.ReadFrames()
	_ = r.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	mapper := &FrameMapper{BaseTime: time.Now()}
	loadout := map[int32]loadoutState{}
	grab := map[int32]grabState{}
	var checks, loadoutEvents, grabEvents int

	for _, v1f := range frames {
		sess := v1f.GetSession()
		if sess == nil {
			continue
		}
		ea := mapper.MapFrame(v1f).GetEchoArena()

		// Replay this frame's loadout/grab events into the running state.
		for _, ev := range ea.GetEvents() {
			if lc := ev.GetLoadoutChanged(); lc != nil {
				loadout[lc.GetPlayerSlot()] = loadoutState{lc.GetWeapon(), lc.GetOrdnance(), lc.GetTacMod()}
				loadoutEvents++
			}
			if gc := ev.GetGrabChanged(); gc != nil {
				grab[gc.GetPlayerSlot()] = grabState{gc.GetLeftHolding(), gc.GetRightHolding()}
				grabEvents++
			}
		}

		// Reconstructed state must equal the v1 source for every player.
		for _, team := range sess.GetTeams() {
			for _, tm := range team.GetPlayers() {
				slot := tm.GetSlotNumber()
				wantLd := loadoutState{tm.GetWeapon(), tm.GetOrdnance(), tm.GetTacMod()}
				if loadout[slot] != wantLd {
					t.Fatalf("loadout mismatch slot %d frame %d: got %+v want %+v",
						slot, v1f.GetFrameIndex(), loadout[slot], wantLd)
				}
				wantGr := grabState{tm.GetLeftHoldingOnto(), tm.GetRightHoldingOnto()}
				if grab[slot] != wantGr {
					t.Fatalf("grab mismatch slot %d frame %d: got %+v want %+v",
						slot, v1f.GetFrameIndex(), grab[slot], wantGr)
				}
				checks++
			}
		}
	}
	t.Logf("frames=%d player-frame-checks=%d loadoutEvents=%d grabEvents=%d",
		len(frames), checks, loadoutEvents, grabEvents)
}
