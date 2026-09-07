package conversion

import "strings"

// Game-type derivation: everything EchoArenaHeader.match_type / .private_match /
// .tournament_match used to carry, read out of the engine's own symbol.
//
// Those three fields are removed and reserved. The single source of truth is
// CaptureHeader.game_type, stored verbatim as the engine spells it, and this is
// the read path for it — without one, game_type is a field written and never
// read, which is the defect class AGENTS.md §4 names.
//
// WHY DERIVATION AND NOT A LOOKUP TABLE. The set of symbols is the ENGINE'S and
// it is OPEN — echo_arena_frenzy is announced and we do not control what comes
// after it. A table maps only what it has seen; an unlisted symbol becomes the
// zero value and the information is destroyed. That is not hypothetical, it is
// what the table this replaces actually did, measured on 2026-09-07 against the
// shipped converter:
//
//	FORWARD  v1 "Echo_Combat_Private" -> MATCH_TYPE_UNSPECIFIED (no map entry)
//	REVERSE  MATCH_TYPE_UNSPECIFIED   -> ""  (len 0)
//
// A real engine gametype round-tripped to the empty string. It was COUNTED —
// vocabulary.matchType recorded the miss and it reached
// ConvertResult.UnmappedValues — so the loss was reported at conversion time; it
// was simply unrecoverable and unmarked forever after. Derivation reads the axes
// out of the symbol instead, so a mode nobody has seen still yields correct
// privacy and tournament flags.
//
// The narrower failure the same run showed, worth keeping straight because the
// two are easy to merge: a symbol the table DID hold, "Echo_Arena_Private",
// round-tripped INTACT — matchTypeReverse was the literal inverse. What the enum
// destroyed there was expressiveness for a v2 consumer, which could not tell
// arena from combat behind MATCH_TYPE_PRIVATE. Only a symbol with no entry at
// all lost its value outright.

// GameTypeFacts is the decomposition of a game-type symbol.
type GameTypeFacts struct {
	// Symbol is the input, verbatim and un-folded. It is the value that must be
	// written back out; nothing here replaces it.
	Symbol string

	// Mode is the symbol case-folded with the recognised axis segments removed.
	// "echo_arena_private" and "Echo_Arena_Private" both yield "echo_arena".
	Mode string

	// Private is true when the symbol carries a "private" segment.
	Private bool

	// Tournament is true when the symbol carries a "tournament" segment.
	Tournament bool

	// KnownMode reports whether Mode is one this build has actually seen.
	//
	// IT MUST NEVER GATE BEHAVIOUR. It exists to be COUNTED, so an unseen mode
	// shows up the first time it appears in the wild instead of silently.
	// Rejecting or zeroing on KnownMode == false would reintroduce exactly the
	// failure this replaces. The pattern is vocabulary.go's: a plain map hit on
	// the common case that does work only on a miss, so counting costs nothing.
	KnownMode bool
}

// gameTypeAxes are the segments describing HOW a match is played rather than
// WHAT is played. They are stripped to leave the mode.
var gameTypeAxes = map[string]func(*GameTypeFacts){
	"private":    func(f *GameTypeFacts) { f.Private = true },
	"tournament": func(f *GameTypeFacts) { f.Tournament = true },
}

// knownGameModes is observational only — see GameTypeFacts.KnownMode. Adding to
// it changes no derivation; it changes what gets counted as novel.
var knownGameModes = map[string]bool{
	"echo_arena":        true,
	"echo_combat":       true,
	"echo_arena_ffa":    true,
	"echo_pass":         true,
	"social_2.0":        true,
	"arena":             true, // short engine spellings, e.g. "arena_tournament"
	"combat":            true,
	"echo_arena_frenzy": true, // announced, not yet observed in a capture
}

// DeriveGameType decomposes a game-type symbol. An empty symbol yields the zero
// value with Symbol == "" — it asserts nothing, and must not be read as
// "public".
func DeriveGameType(symbol string) GameTypeFacts {
	f := GameTypeFacts{Symbol: symbol}
	if symbol == "" {
		return f
	}

	// Case is the producer's, not ours: the engine and nevr-common api/v1 `mode`
	// spell it "echo_arena_private"; v1 echoreplay JSON spells it
	// "Echo_Arena_Private". Both must derive identically.
	segments := strings.Split(strings.ToLower(symbol), "_")

	kept := segments[:0:0]
	for _, seg := range segments {
		if mark, ok := gameTypeAxes[seg]; ok {
			mark(&f)
			continue
		}
		kept = append(kept, seg)
	}

	f.Mode = strings.Join(kept, "_")
	f.KnownMode = knownGameModes[f.Mode]
	return f
}
