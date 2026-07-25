package conversion

import "testing"

// TestAuditStatusClassifiesBothLanes guards the failure lane that
// TestEchoReplayRoundTripFidelityAudit is built to catch.
//
// WHY IT EXISTS: the audit compares each round-tripped frame in two lanes — the
// SessionResponse lane and the whole-frame lane — but only the session lane was
// ever asserted. A recording that round-tripped with every PlayerBones dropped
// scored session-mismatch=0, whole-frame-mismatch=197 and was printed
// "LOSSLESS", and the test PASSED. That audit is the floor under the decision to
// delete irreplaceable recordings, so a total bone loss labelled lossless is the
// worst possible defect in it.
//
// BAC: a non-zero mismatch in EITHER lane is lossy, is never called LOSSLESS,
// and names the lane that failed.
func TestAuditStatusClassifiesBothLanes(t *testing.T) {
	cases := []struct {
		name           string
		session, frame int
		wantStatus     string
		wantLossy      bool
	}{
		{"clean", 0, 0, "LOSSLESS", false},
		// The dangerous case: SessionResponse survived, everything outside it
		// (player bones) did not.
		{"whole_frame_only", 0, 197, "LOSSY-FRAME", true},
		{"session_fields", 12, 12, "LOSSY-SESSION", true},
		{"both_lanes", 5, 197, "LOSSY-SESSION", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, lossy := auditStatus(tc.session, tc.frame)
			if lossy != tc.wantLossy {
				t.Errorf("auditStatus(session=%d, frame=%d) lossy=%v, want %v",
					tc.session, tc.frame, lossy, tc.wantLossy)
			}
			if status != tc.wantStatus {
				t.Errorf("auditStatus(session=%d, frame=%d) status=%q, want %q",
					tc.session, tc.frame, status, tc.wantStatus)
			}
			if tc.frame > 0 && status == "LOSSLESS" {
				t.Errorf("auditStatus(session=%d, frame=%d) reported LOSSLESS with a whole-frame mismatch",
					tc.session, tc.frame)
			}
		})
	}
}
