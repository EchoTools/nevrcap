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
// A third lane was added after the same defect was found one level lower: the
// two mismatch lanes both count PARSED frames, so content the reader discarded
// (protojson DiscardUnknown) is absent from both sides and scores zero in each.
// unknownKeys is the key-set completeness guard's count — keys present in the
// original file's bytes that the proto cannot represent. It outranks the other
// two, because a non-zero count means the comparison itself was blind.
//
// A fourth lane was added for the same reason one step further out:
// droppedFrames counts WHOLE FRAMES the reader refused (unparseable session
// JSON; pkg/codec/echoreplay.go increments skippedFrames and continues). Such a
// frame is missing from both sides AND from both frame counts, so it is the
// most invisible loss of all — and the largest. It outranks every other lane.
//
// BAC: a non-zero count in ANY lane is lossy, is never called LOSSLESS, and
// names the lane that failed.
func TestAuditStatusClassifiesBothLanes(t *testing.T) {
	cases := []struct {
		name                                       string
		session, frame, unknownKeys, droppedFrames int
		wantStatus                                 string
		wantLossy                                  bool
	}{
		{"clean", 0, 0, 0, 0, "LOSSLESS", false},
		// The dangerous case: SessionResponse survived, everything outside it
		// (player bones) did not.
		{"whole_frame_only", 0, 197, 0, 0, "LOSSY-FRAME", true},
		{"session_fields", 12, 12, 0, 0, "LOSSY-SESSION", true},
		{"both_lanes", 5, 197, 0, 0, "LOSSY-SESSION", true},
		// The blind case: both mismatch lanes are clean precisely BECAUSE the
		// content never survived the read. Verified on a real recording
		// rebuilt with an unknown key in all 288 session objects: LOSSLESS,
		// session-mismatch=0, whole-frame-mismatch=0, test PASSED.
		{"dropped_at_read", 0, 0, 1, 0, "LOSSY-KEYS", true},
		{"dropped_and_mismatched", 3, 3, 2, 0, "LOSSY-KEYS", true},
		// The blindest case: the frame never existed as far as any counter
		// downstream of the reader is concerned.
		{"frame_dropped", 0, 0, 0, 1, "LOSSY-DROPPED", true},
		{"frame_dropped_outranks_keys", 4, 4, 9, 1, "LOSSY-DROPPED", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, lossy := auditStatus(tc.session, tc.frame, tc.unknownKeys, tc.droppedFrames)
			if lossy != tc.wantLossy {
				t.Errorf("auditStatus(session=%d, frame=%d, keys=%d, dropped=%d) lossy=%v, want %v",
					tc.session, tc.frame, tc.unknownKeys, tc.droppedFrames, lossy, tc.wantLossy)
			}
			if status != tc.wantStatus {
				t.Errorf("auditStatus(session=%d, frame=%d, keys=%d, dropped=%d) status=%q, want %q",
					tc.session, tc.frame, tc.unknownKeys, tc.droppedFrames, status, tc.wantStatus)
			}
			if tc.frame > 0 && status == "LOSSLESS" {
				t.Errorf("auditStatus(session=%d, frame=%d) reported LOSSLESS with a whole-frame mismatch",
					tc.session, tc.frame)
			}
		})
	}
}
