package events

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
)

// frameSequence is a generated sequence of frames that simulates a realistic
// EchoVR match with varying players, possession changes, stats, and round
// transitions.
type frameSequence struct {
	Frames []*telemetry.LobbySessionStateFrame
}

func (frameSequence) Generate(rand *rand.Rand, size int) reflect.Value {
	return reflect.ValueOf(generateFrameSequence(rand, size))
}

// generateFrameSequence builds a plausible match timeline.
func generateFrameSequence(rng *rand.Rand, size int) frameSequence {
	numFrames := 5 + rng.Intn(max(1, size*3))
	if numFrames > 200 {
		numFrames = 200
	}
	numPlayers := 2 + rng.Intn(7) // 2..8

	// Build a stable roster.
	type rosterEntry struct {
		slot    int32
		name    string
		teamIdx int
		jersey  int32
	}
	roster := make([]rosterEntry, numPlayers)
	usedSlots := make(map[int32]bool)
	for i := range roster {
		slot := int32(rng.Intn(16))
		for usedSlots[slot] {
			slot = int32(rng.Intn(16))
		}
		usedSlots[slot] = true
		teamIdx := 0
		if i >= numPlayers/2 {
			teamIdx = 1
		}
		roster[i] = rosterEntry{
			slot:    slot,
			name:    fmt.Sprintf("Player%d", slot),
			teamIdx: teamIdx,
			jersey:  int32(i),
		}
	}

	// Track cumulative stats per player slot.
	type accum struct {
		goals, saves, steals, stuns, assists, blocks, shots, points int32
	}
	stats := make(map[int32]*accum)
	for _, r := range roster {
		stats[r.slot] = &accum{}
	}

	possessorSlot := int32(-1)
	blueRoundScore := int32(0)
	orangeRoundScore := int32(0)
	roundActive := false

	frames := make([]*telemetry.LobbySessionStateFrame, numFrames)
	activeRoster := roster // players currently in the match

	for fi := range frames {
		// Decide game status for this frame.
		status := GameStatusPlaying
		if !roundActive && fi > 0 {
			status = GameStatusRoundStart
		}
		if rng.Intn(30) == 0 && fi > 2 {
			status = GameStatusRoundOver
			roundActive = false
		} else {
			roundActive = true
		}

		// Occasional player leave/join (at most one per frame to keep it simple).
		if rng.Intn(15) == 0 && len(activeRoster) > 2 {
			// Remove a random player.
			idx := rng.Intn(len(activeRoster))
			activeRoster = append(activeRoster[:idx], activeRoster[idx+1:]...)
		}
		if rng.Intn(15) == 0 && len(activeRoster) < len(roster) {
			// Re-add a previously removed player.
			for _, r := range roster {
				found := false
				for _, a := range activeRoster {
					if a.slot == r.slot {
						found = true
						break
					}
				}
				if !found {
					activeRoster = append(activeRoster, r)
					break
				}
			}
		}

		// Randomize possession.
		if len(activeRoster) > 0 && rng.Intn(3) == 0 {
			possessorSlot = activeRoster[rng.Intn(len(activeRoster))].slot
		}
		if rng.Intn(5) == 0 {
			possessorSlot = -1
		}

		// Randomly bump stats (only increase, never decrease).
		for _, r := range activeRoster {
			a := stats[r.slot]
			if a == nil {
				a = &accum{}
				stats[r.slot] = a
			}
			if rng.Intn(10) == 0 {
				a.goals++
				a.points += 2
			}
			if rng.Intn(8) == 0 {
				a.saves++
			}
			if rng.Intn(8) == 0 {
				a.steals++
			}
			if rng.Intn(6) == 0 {
				a.stuns++
			}
			if rng.Intn(10) == 0 {
				a.assists++
			}
			if rng.Intn(8) == 0 {
				a.blocks++
			}
			if rng.Intn(6) == 0 {
				a.shots++
			}
		}

		// Occasionally bump round scores (when a round ends).
		if status == GameStatusRoundOver {
			if rng.Intn(2) == 0 {
				blueRoundScore++
			} else {
				orangeRoundScore++
			}
		}

		// Build teams.
		blueTeam := &enginev1.Team{}
		orangeTeam := &enginev1.Team{}
		for _, r := range activeRoster {
			a := stats[r.slot]
			member := &enginev1.TeamMember{
				SlotNumber:    r.slot,
				DisplayName:   r.name,
				JerseyNumber:  r.jersey,
				HasPossession: r.slot == possessorSlot,
				Stats: &enginev1.PlayerStats{
					Goals:      a.goals,
					Saves:      a.saves,
					Steals:     a.steals,
					Stuns:      a.stuns,
					Assists:    a.assists,
					Blocks:     a.blocks,
					ShotsTaken: a.shots,
					Points:     a.points,
				},
			}
			if r.teamIdx == 0 {
				blueTeam.Players = append(blueTeam.Players, member)
			} else {
				orangeTeam.Players = append(orangeTeam.Players, member)
			}
		}

		frames[fi] = &telemetry.LobbySessionStateFrame{
			FrameIndex: uint32(fi),
			Session: &enginev1.SessionResponse{
				GameStatus:       status,
				BlueRoundScore:   blueRoundScore,
				OrangeRoundScore: orangeRoundScore,
				Teams:            []*enginev1.Team{blueTeam, orangeTeam},
			},
		}
	}

	return frameSequence{Frames: frames}
}

// collectAllEvents feeds the full frame sequence through a synchronous
// AsyncDetector with DefaultSensors and returns every emitted event.
func collectAllEvents(seq frameSequence) []*telemetry.LobbySessionEvent {
	det := New(
		WithSynchronousProcessing(),
		WithEventsChannelSize(1024),
		WithSensors(DefaultSensors()...),
	)
	defer det.Stop()

	var all []*telemetry.LobbySessionEvent
	for _, f := range seq.Frames {
		det.ProcessFrame(f)
		// Drain all available event batches.
		for {
			select {
			case batch := <-det.EventsChan():
				all = append(all, batch...)
			default:
				goto done
			}
		done:
			break
		}
	}
	return all
}

// ---------------------------------------------------------------------------
// Property 1: No duplicate consecutive events with identical data
// ---------------------------------------------------------------------------

func TestProperty_NoDuplicateConsecutiveEvents(t *testing.T) {
	f := func(seq frameSequence) bool {
		events := collectAllEvents(seq)
		for i := 1; i < len(events); i++ {
			prev := events[i-1]
			curr := events[i]
			if sameEventTypeAndData(prev, curr) {
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Errorf("found duplicate consecutive events: %v", err)
	}
}

// sameEventTypeAndData returns true when two events have the same oneof case
// and identical payload. Uses the protobuf reflect API to compare the oneof
// variant names and then proto.Equal on the inner messages.
func sameEventTypeAndData(a, b *telemetry.LobbySessionEvent) bool {
	if a == nil || b == nil {
		return false
	}
	aRef := a.ProtoReflect()
	bRef := b.ProtoReflect()
	eventField := aRef.Descriptor().Oneofs().ByName("event")
	if eventField == nil {
		return false
	}
	aWhich := aRef.WhichOneof(eventField)
	bWhich := bRef.WhichOneof(eventField)
	if aWhich == nil || bWhich == nil {
		return false
	}
	if aWhich.Name() != bWhich.Name() {
		return false
	}
	// Same variant -- compare inner message values.
	aVal := aRef.Get(aWhich)
	bVal := bRef.Get(bWhich)
	// For message fields the Value wraps a protoreflect.Message.
	aMsg := aVal.Message().Interface()
	bMsg := bVal.Message().Interface()
	return fmt.Sprintf("%v", aMsg) == fmt.Sprintf("%v", bMsg)
}

// ---------------------------------------------------------------------------
// Property 2: Event counts match stat deltas
// ---------------------------------------------------------------------------

func TestProperty_EventCountsMatchStatDeltas(t *testing.T) {
	f := func(seq frameSequence) bool {
		if len(seq.Frames) < 2 {
			return true
		}
		events := collectAllEvents(seq)

		// Count events per slot.
		eventCounts := make(map[int32]*statCounts)
		for _, e := range events {
			switch v := e.Event.(type) {
			case *telemetry.LobbySessionEvent_PlayerGoal:
				s := v.PlayerGoal.PlayerSlot
				ensureStatCounts(eventCounts, s)
				eventCounts[s].goals++
			case *telemetry.LobbySessionEvent_PlayerSave:
				s := v.PlayerSave.PlayerSlot
				ensureStatCounts(eventCounts, s)
				eventCounts[s].saves++
			case *telemetry.LobbySessionEvent_PlayerSteal:
				s := v.PlayerSteal.PlayerSlot
				ensureStatCounts(eventCounts, s)
				eventCounts[s].steals++
			case *telemetry.LobbySessionEvent_PlayerStun:
				s := v.PlayerStun.PlayerSlot
				ensureStatCounts(eventCounts, s)
				eventCounts[s].stuns++
			case *telemetry.LobbySessionEvent_PlayerAssist:
				s := v.PlayerAssist.PlayerSlot
				ensureStatCounts(eventCounts, s)
				eventCounts[s].assists++
			case *telemetry.LobbySessionEvent_PlayerBlock:
				s := v.PlayerBlock.PlayerSlot
				ensureStatCounts(eventCounts, s)
				eventCounts[s].blocks++
			case *telemetry.LobbySessionEvent_PlayerShotTaken:
				s := v.PlayerShotTaken.PlayerSlot
				ensureStatCounts(eventCounts, s)
				eventCounts[s].shots++
			}
		}

		// Compute stat deltas from first to last frame appearance of each slot.
		firstStats := make(map[int32]playerStatSnapshot)
		lastStats := make(map[int32]playerStatSnapshot)
		for _, frame := range seq.Frames {
			for _, team := range frame.GetSession().GetTeams() {
				for _, p := range team.GetPlayers() {
					slot := p.GetSlotNumber()
					snap := snapshotFromStats(p.GetStats())
					if _, ok := firstStats[slot]; !ok {
						firstStats[slot] = snap
					}
					lastStats[slot] = snap
				}
			}
		}

		for slot, last := range lastStats {
			first := firstStats[slot]
			ec := eventCounts[slot]
			if ec == nil {
				ec = &statCounts{}
			}
			delta := func(a, b int32) int32 { return b - a }
			if delta(first.goals, last.goals) != ec.goals {
				return false
			}
			if delta(first.saves, last.saves) != ec.saves {
				return false
			}
			if delta(first.steals, last.steals) != ec.steals {
				return false
			}
			if delta(first.stuns, last.stuns) != ec.stuns {
				return false
			}
			if delta(first.assists, last.assists) != ec.assists {
				return false
			}
			if delta(first.blocks, last.blocks) != ec.blocks {
				return false
			}
			if delta(first.shotsTaken, last.shotsTaken) != ec.shots {
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Errorf("event counts do not match stat deltas: %v", err)
	}
}

type statCounts struct {
	goals, saves, steals, stuns, assists, blocks, shots int32
}

func ensureStatCounts(m map[int32]*statCounts, slot int32) {
	if m[slot] == nil {
		m[slot] = &statCounts{}
	}
}

// ---------------------------------------------------------------------------
// Property 3: Possession changes are consistent (no self-possession)
// ---------------------------------------------------------------------------

func TestProperty_PossessionChangesConsistent(t *testing.T) {
	f := func(seq frameSequence) bool {
		events := collectAllEvents(seq)
		for _, e := range events {
			if pc := e.GetDiscPossessionChanged(); pc != nil {
				if pc.PlayerSlot == pc.PreviousPlayerSlot {
					return false
				}
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Errorf("self-possession change detected: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 4: Round numbers only increase
// ---------------------------------------------------------------------------

func TestProperty_RoundNumbersMonotonic(t *testing.T) {
	f := func(seq frameSequence) bool {
		events := collectAllEvents(seq)
		lastRound := int32(0)
		for _, e := range events {
			if rs := e.GetRoundStarted(); rs != nil {
				if rs.RoundNumber < lastRound {
					return false
				}
				lastRound = rs.RoundNumber
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Errorf("round numbers are not monotonically increasing: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Property 5: Join/Leave consistency -- no phantom leaves
// ---------------------------------------------------------------------------

func TestProperty_JoinLeaveConsistency(t *testing.T) {
	f := func(seq frameSequence) bool {
		events := collectAllEvents(seq)
		joined := make(map[int32]bool) // slots currently joined
		for _, e := range events {
			if pj := e.GetPlayerJoined(); pj != nil {
				joined[pj.Player.GetSlotNumber()] = true
			}
			if pl := e.GetPlayerLeft(); pl != nil {
				if !joined[pl.PlayerSlot] {
					return false // phantom leave
				}
				delete(joined, pl.PlayerSlot)
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Errorf("phantom player leave detected: %v", err)
	}
}
