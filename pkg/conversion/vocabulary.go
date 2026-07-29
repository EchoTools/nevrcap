package conversion

import (
	"cmp"
	"slices"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
)

// VOCAB-001. The string-to-enum tables in mapping.go translate engine
// vocabulary; a value no table knows falls through to *_UNSPECIFIED, and the
// reverse map renders UNSPECIFIED as "". So an unrecognised value converts
// cleanly and is simply gone. GOALTYPE-001 was an instance — six of eleven goal
// types were missing and were being discarded on real captures.
//
// The tables cannot be completed from the binary (echovr.exe carries lowercase
// symbol names such as echo_arena_private while the JSON writes
// Echo_Arena_Private, so the vocabularies differ), and one unknown string must
// not abort a 174 GB pass. So the loss is counted and surfaced per file instead
// of guessed at or fatal.

// UnmappedValue is one engine string that no conversion table recognised,
// with how many times it occurred in the capture.
type UnmappedValue struct {
	// Field is the engine JSON field the value came from, e.g. "game_status".
	Field string
	// Value is the unrecognised string as the engine wrote it.
	Value string
	// Count is how many times it occurred.
	Count uint32
}

type unmappedKey struct{ field, value string }

// vocabulary accumulates unrecognised engine strings during one conversion.
// It is not safe for concurrent use; each conversion owns its own. A nil
// *vocabulary is usable and records nothing, so the lookup helpers can serve
// the exported entry points that have nowhere to report.
type vocabulary struct {
	seen map[unmappedKey]uint32
}

// record notes one occurrence of an unrecognised value.
//
// The empty string is deliberately not recorded. An absent engine field is not
// an unknown vocabulary item: game_status is empty in 10 of 25 measured dal1
// captures and round-trips correctly (unmapped -> UNSPECIFIED -> ""), so
// counting it would swamp the signal on nearly half the corpus with a loss that
// is not real.
func (v *vocabulary) record(field, value string) {
	if v == nil || value == "" {
		return
	}
	if v.seen == nil {
		v.seen = make(map[unmappedKey]uint32, 4)
	}
	v.seen[unmappedKey{field, value}]++
}

// sorted returns the accumulated values ordered by field then value, so a
// per-file receipt on a corpus run is stable and diffable.
func (v *vocabulary) sorted() []UnmappedValue {
	if v == nil || len(v.seen) == 0 {
		return nil
	}
	out := make([]UnmappedValue, 0, len(v.seen))
	for k, n := range v.seen {
		out = append(out, UnmappedValue{Field: k.field, Value: k.value, Count: n})
	}
	slices.SortFunc(out, func(a, b UnmappedValue) int {
		if c := cmp.Compare(a.Field, b.Field); c != 0 {
			return c
		}
		return cmp.Compare(a.Value, b.Value)
	})
	return out
}

// The lookup helpers below are the only sanctioned way to consult the
// vocabulary tables on the conversion path. Each is a plain map hit on the
// common case and does work only on a miss, so counting costs nothing per
// frame.

func (v *vocabulary) gameStatus(s string) capturepb.GameStatus {
	if e, ok := gameStatusMap[s]; ok {
		return e
	}
	v.record("game_status", s)
	return capturepb.GameStatus_GAME_STATUS_UNSPECIFIED
}

func (v *vocabulary) matchType(s string) capturepb.MatchType {
	if e, ok := matchTypeMap[s]; ok {
		return e
	}
	v.record("match_type", s)
	return capturepb.MatchType_MATCH_TYPE_UNSPECIFIED
}

func (v *vocabulary) pauseState(s string) capturepb.PauseState {
	if e, ok := pauseStateMap[s]; ok {
		return e
	}
	v.record("paused_state", s)
	return capturepb.PauseState_PAUSE_STATE_UNSPECIFIED
}

func (v *vocabulary) goalType(s string) capturepb.GoalType {
	if e, ok := goalTypeStringMap[s]; ok {
		return e
	}
	v.record("goal_type", s)
	return capturepb.GoalType_GOAL_TYPE_UNSPECIFIED
}
