package conversion_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/echotools/tape/pkg/conversion"
	"github.com/echotools/tape/pkg/fidelity"
)

// roundtrip_v2_test.go — the acceptance gate for echoreplay -> v2 -> echoreplay.
//
// These tests are deliberately THIN. Everything they assert is computed by
// conversion.VerifyEchoReplayRoundTrip, which is ordinary library code any
// caller can run on any file: the same check that gates this test can gate a
// conversion or an archive deletion. The previous version of this file WAS the
// check — 400 lines of hand-written field comparison that only existed inside
// `go test`, that missed 6 SessionResponse fields entirely, and that had a
// strict=false mode allowing a real recording's losses to be logged instead of
// failed.
//
// What the gate now asserts, per Andrew's ruling (2026-07-25):
//   - EVERY FIELD COMPARED — a descriptor-driven differ, no hand list.
//   - ANY DIFFERENCE IS AN ERROR — unless the path is in KnownUnpreserved.
//   - ANYTHING NOT REACHED IS AN ERROR — coverage is asserted, not assumed.
//   - Content dropped at READ time (unparseable frames, unrepresentable JSON
//     keys in either payload) is fatal, because it is absent from both sides of
//     the comparison and would otherwise read as "lossless".

// TestRoundTripBAC is the committed-sample gate: the sample must survive
// echoreplay -> v2 -> echoreplay completely, except on paths KnownUnpreserved
// documents.
func TestRoundTripBAC(t *testing.T) {
	src := filepath.Join("..", "..", "testdata", "sample.echoreplay")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no sample: %v", err)
	}
	verifyRoundTrip(t, src)
}

// TestRoundTripBACAudit runs the identical gate on a real external recording
// (TAPE_AUDIT_FILE). It is identical on purpose: the old audit reported its
// recoverable-lane mismatches instead of failing on them, which is how
// possession losing every frame stayed green for weeks. There is no
// report-only mode any more.
func TestRoundTripBACAudit(t *testing.T) {
	src := os.Getenv("TAPE_AUDIT_FILE")
	if src == "" {
		t.Skip("set TAPE_AUDIT_FILE=/path/to.echoreplay to run the audit round-trip")
	}
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no audit file: %v", err)
	}
	verifyRoundTrip(t, src)
}

func verifyRoundTrip(t *testing.T, src string) *fidelity.Verdict {
	t.Helper()
	v, err := conversion.VerifyEchoReplayRoundTrip(src, conversion.VerifyOptions{
		Allowlist: conversion.KnownUnpreserved,
	})
	if err != nil {
		t.Fatalf("verify %s: %v", src, err)
	}
	t.Log("\n" + v.String())
	for _, why := range v.FailureReasons() {
		t.Errorf("%s: %s", filepath.Base(src), why)
	}
	if v.Pass() {
		t.Logf("%s round-trips completely (allowlisted holes aside): %d frames, %d schema fields compared",
			filepath.Base(src), v.FramesOriginal, totalSchemaPaths(v))
	}
	return v
}

func totalSchemaPaths(v *fidelity.Verdict) int {
	n := 0
	for _, r := range v.Roots {
		n += r.SchemaPaths
	}
	return n
}
