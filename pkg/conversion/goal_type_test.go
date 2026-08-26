package conversion

import (
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
)

// engineGoalTypes is the complete goal-type table read from echovr.exe, where
// the strings sit contiguously at 0x1416e22b0-0x1416e2360. Anything the mapper
// does not know collapses to GOAL_TYPE_UNSPECIFIED and the value is lost.
//
// Measured presence in a 30-file dal1 sample (files containing each):
// SLAM DUNK 7, INSIDE SHOT 6, LONG SHOT 6, [NO GOAL] 6, SELF GOAL 5,
// BOUNCE SHOT 3, LONG BOUNCE SHOT 2. LONG HEADBUTT also appears in a January
// 2026 client capture (729 frames).
var engineGoalTypes = []string{
	"[NO GOAL]",        // 0x1416e22b0
	"SLAM DUNK",        // 0x1416e22c0
	"INSIDE SHOT",      // 0x1416e22d0
	"LONG SHOT",        // 0x1416e22e0
	"BOUNCE SHOT",      // 0x1416e22f0
	"LONG BOUNCE SHOT", // 0x1416e2300
	"HEADBUTT",         // 0x1416e2318
	"LONG HEADBUTT",    // 0x1416e2328
	"BUMPER SHOT",      // 0x1416e2338
	"LONG BUMPER SHOT", // 0x1416e2348
	"SELF GOAL",        // 0x1416e2360
}

func TestGoalTypeMapCoversEveryEngineValue(t *testing.T) {
	t.Parallel()

	for _, s := range engineGoalTypes {
		t.Run(s, func(t *testing.T) {
			t.Parallel()
			if got := goalTypeStringToEnum(s); got == capturepb.GoalType_GOAL_TYPE_UNSPECIFIED {
				t.Errorf("goalTypeStringToEnum(%q) = UNSPECIFIED — the value is lost on conversion", s)
			}
		})
	}
}

// TestGoalTypeRoundTrips pins the reverse direction too: a value that maps in
// but cannot map back is still lost, just later.
func TestGoalTypeRoundTrips(t *testing.T) {
	t.Parallel()

	for _, s := range engineGoalTypes {
		enum := goalTypeStringToEnum(s)
		if back := goalTypeReverse[enum]; back != s {
			t.Errorf("%q -> %v -> %q, want round-trip", s, enum, back)
		}
	}
}
