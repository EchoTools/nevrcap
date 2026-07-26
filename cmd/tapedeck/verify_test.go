package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// verify_test.go — `tapedeck verify` is the production caller of the round-trip
// verification.
//
// Before this, VerifyEchoReplayRoundTrip was called from _test.go files only:
// the check was callable, not called. `tapedeck verify` ran a hand-written list
// of event-shape checks instead, which say nothing about whether the recording
// survived the conversion field for field — the question someone asks before
// deleting the original.

// TestVerifyCommandRunsTheRoundTripVerification pins that the CLI's verdict IS
// the fidelity verdict: it quotes the receipt (identity, frame counts, key
// scan, per-root coverage) and it fails on a file that does not round-trip.
//
// The committed sample does NOT round-trip today (BUGS.md FIDELITY-002:
// client_name and the pause sub-state are lost, awaiting a ruling), so a
// correct verify command must exit non-zero on it. That is the point: the
// command reports what is true.
func TestVerifyCommandRunsTheRoundTripVerification(t *testing.T) {
	src := filepath.Join("..", "..", "testdata", "sample.echoreplay")

	var out bytes.Buffer
	cmd := newVerifyCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{src})
	err := cmd.Execute()
	got := out.String()

	if err == nil {
		t.Errorf("verify exited 0 on a file that does not round-trip:\n%s", got)
	}
	for _, want := range []string{
		"FIDELITY FAIL",
		"sha256=",
		"keyscan=",
		"SessionResponse:",
		"PlayerBonesResponse:",
		"SessionResponse.client_name",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("verify output does not quote %q:\n%s", want, got)
		}
	}
	t.Logf("verify output:\n%s", got)
}

// TestVerifyCommandRejectsNonEchoReplay keeps the existing input contract.
func TestVerifyCommandRejectsNonEchoReplay(t *testing.T) {
	var out bytes.Buffer
	cmd := newVerifyCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{filepath.Join("..", "..", "testdata", "sample.tape.golden")})
	if err := cmd.Execute(); err == nil {
		t.Errorf("verify accepted a non-.echoreplay input:\n%s", out.String())
	}
}

// TestVerifyCommandExitsNonZero proves the process — not just the RunE return —
// fails, because that exit code is what a deletion script reads.
func TestVerifyCommandExitsNonZero(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the CLI")
	}
	src := filepath.Join("..", "..", "testdata", "sample.echoreplay")
	out, err := runTapedeck(t, "verify", src)
	if err == nil {
		t.Errorf("tapedeck verify exited 0 on a file that does not round-trip:\n%s", out)
	}
	if !strings.Contains(out, "FIDELITY FAIL") {
		t.Errorf("tapedeck verify did not print the verdict:\n%s", out)
	}
}
