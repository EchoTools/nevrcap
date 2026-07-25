package fidelity_test

import (
	"testing"

	"github.com/echotools/tape/pkg/fidelity"
)

// TestAllowlistSubtreeMatching pins the one thing the allowlist must get right:
// an entry covers the field it names and everything beneath it — because an
// entry like "last_throw has no v2 home" is a statement about the whole message
// — and it must NOT bleed onto a field whose name merely starts the same way.
// A bleeding prefix would silently excuse a loss nobody ruled on.
func TestAllowlistSubtreeMatching(t *testing.T) {
	a := fidelity.Allowlist{
		"SessionResponse.last_throw":    "documented",
		"SessionResponse.teams[]#count": "documented",
	}
	covered := []string{
		"SessionResponse.last_throw",
		"SessionResponse.last_throw#presence",
		"SessionResponse.last_throw.arm_speed",
		"SessionResponse.teams[]#count",
	}
	for _, p := range covered {
		if _, ok := a.Reason(p); !ok {
			t.Errorf("%s should be covered by the allowlist", p)
		}
	}
	notCovered := []string{
		"SessionResponse.last_throw_extra",
		"SessionResponse.last_score",
		"SessionResponse.teams[]",
		"SessionResponse.teams[].team_name",
		"SessionResponse.teams[]#presence",
	}
	for _, p := range notCovered {
		if r, ok := a.Reason(p); ok {
			t.Errorf("%s must NOT be excused by the allowlist (matched %q)", p, r)
		}
	}
}

func TestNilAllowlistExcusesNothing(t *testing.T) {
	var a fidelity.Allowlist
	if _, ok := a.Reason("SessionResponse.anything"); ok {
		t.Fatal("a nil allowlist excused a difference")
	}
}
