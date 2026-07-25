package conversion

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/echotools/tape/pkg/codec"
	"github.com/echotools/tape/pkg/fidelity"
	"google.golang.org/protobuf/proto"
)

// TestEchoReplayRoundTripFidelityAudit is the parameterized twin of
// TestEchoReplayRoundTripFidelity.
//
// WHY IT EXISTS: the baseline test is hardcoded to testdata/sample.echoreplay,
// so it can only ever prove fidelity for one committed file. Before deleting
// real recordings in favour of a converted archive, the lossless claim has to
// hold on THOSE recordings, not on a sample. docs/format-design.md §8 states
// every audit tool takes TAPE_AUDIT_FILE; this one actually does.
//
// TAPE_AUDIT_FILE may be a single .echoreplay OR a directory, in which case
// every .echoreplay inside is checked (bounded by TAPE_AUDIT_LIMIT, default 25).
//
// The key-set guard is exhaustive: every frame of every file. It used to take a
// frame budget (TAPE_AUDIT_KEYSCAN) and sample past it, because parsing each
// payload into map[string]any cost ~20 minutes and 13 GB on a 102,892-frame
// recording. The scanner no longer materializes anything, so the budget — and
// with it the possibility of a guard that quietly did not look — is gone.
//
// BAC: for every audited recording, echoreplay -> v1 codec -> echoreplay
// preserves the complete SessionResponse of every frame, field for field, AND
// every JSON key present in the original file's session objects is a key the
// proto can represent — nothing was dropped before the comparison began.
func TestEchoReplayRoundTripFidelityAudit(t *testing.T) {
	target := os.Getenv("TAPE_AUDIT_FILE")
	if target == "" {
		t.Skip("set TAPE_AUDIT_FILE to a .echoreplay file or a directory of them")
	}

	files, err := collectEchoReplays(target, auditLimit())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no .echoreplay files found under %s", target)
	}

	totalFrames := 0
	lossy := 0
	lossySession := 0
	lossyFrame := 0
	lossyKeys := 0
	lossyDropped := 0

	for _, f := range files {
		frames, skipped, sessionMismatch, frameMismatch, err := roundTripEchoReplay(t, f)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		// The key-set completeness guard reads the ORIGINAL FILE'S BYTES. The
		// two mismatch counters above cannot: both sides of that comparison
		// come from the same reader, which discards unknown keys, so content
		// dropped at read time is invisible to it. It is exhaustive — every
		// frame of every file. See pkg/fidelity/keyscan.go.
		keys, err := fidelity.ScanEchoReplayKeys(f)
		if err != nil {
			t.Errorf("%s: key-set guard: %v", filepath.Base(f), err)
			continue
		}
		totalFrames += frames
		dropped := skipped + keys.ParseErrors
		status, isLossy := auditStatus(sessionMismatch, frameMismatch, len(keys.Findings), dropped)
		if isLossy {
			lossy++
			switch {
			case dropped > 0:
				lossyDropped++
			case len(keys.Findings) > 0:
				lossyKeys++
			case sessionMismatch > 0:
				lossySession++
			default:
				lossyFrame++
			}
		}
		t.Logf("%-14s %-70s frames=%-6d codec-skipped=%d session-mismatch=%d whole-frame-mismatch=%d %s",
			status, filepath.Base(f), frames, skipped, sessionMismatch, frameMismatch, keys.Summary())
	}

	t.Logf("AUDIT SUMMARY: %d file(s), %d frames, %d lossy (%d dropped-at-read, %d unrepresentable-keys, %d SessionResponse, %d whole-frame-only)",
		len(files), totalFrames, lossy, lossyDropped, lossyKeys, lossySession, lossyFrame)
	if lossy > 0 {
		t.Fatalf("ECHOREPLAY ROUND-TRIP IS LOSSY on %d/%d audited file(s): "+
			"%d dropped whole frames at read time (unparseable payloads, absent from both sides "+
			"of the comparison), %d carry JSON keys the proto cannot represent (discarded at read "+
			"time, so the round-trip comparison never saw them), %d lost SessionResponse fields, "+
			"%d lost whole-frame data outside the SessionResponse (player bones) while their "+
			"SessionResponse survived",
			lossy, len(files), lossyDropped, lossyKeys, lossySession, lossyFrame)
	}
}

// auditStatus classifies one audited file from its loss counters. The lanes are
// kept distinct — and ALL are fatal:
//
//   - sessionMismatch: the frame's SessionResponse did not survive the
//     round-trip. A session field was lost.
//   - frameMismatch: the whole LobbySessionStateFrame did not survive. The
//     SessionResponse is part of the frame, so a session loss always shows up
//     here as well; a frame mismatch with NO session mismatch means the loss is
//     OUTSIDE the SessionResponse — in practice the player bones.
//
// The bones case is why the frame lane must fail the audit: a recording that
// round-tripped with every PlayerBones dropped scores sessionMismatch=0, and
// calling that LOSSLESS is exactly the wrong answer to the only question this
// audit exists to answer — whether the originals can be deleted.
//
// The third lane, unknownKeys, comes from the key-set completeness guard
// (pkg/fidelity/keyscan.go) and outranks both mismatch lanes. Both mismatch
// counters are computed from PARSED frames, so anything the reader discarded
// is missing from both sides of the comparison and scores zero in each: the
// reader is built with protojson DiscardUnknown, and a real recording rebuilt
// with an unknown key in all 288 session objects audited LOSSLESS, 0/0, PASS.
// A non-zero unknownKeys count means the file carries content the codec never
// admitted — so the comparison was blind and its zeroes prove nothing.
//
// The fourth lane, droppedFrames, outranks even that, and is the same defect
// one step further: a frame whose session JSON does not parse is not partially
// read, it is SKIPPED (pkg/codec/echoreplay.go increments skippedFrames and
// continues). A skipped frame is absent from both sides of every comparison and
// from the frame counts on both sides, so nothing downstream can notice it.
// Whole frames vanishing is the largest possible loss, so it is the highest
// lane.
func auditStatus(sessionMismatch, frameMismatch, unknownKeys, droppedFrames int) (status string, lossy bool) {
	switch {
	case droppedFrames > 0:
		return "LOSSY-DROPPED", true
	case unknownKeys > 0:
		return "LOSSY-KEYS", true
	case sessionMismatch > 0:
		return "LOSSY-SESSION", true
	case frameMismatch > 0:
		return "LOSSY-FRAME", true
	default:
		return "LOSSLESS", false
	}
}

func auditLimit() int {
	if v := os.Getenv("TAPE_AUDIT_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 25
}

func collectEchoReplays(target string, limit int) ([]string, error) {
	fi, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return []string{target}, nil
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".echoreplay" {
			continue
		}
		out = append(out, filepath.Join(target, e.Name()))
	}
	sort.Strings(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// roundTripEchoReplay reads a recording, writes it back out through the
// echoreplay codec, re-reads it, and compares every frame's SessionResponse.
//
// skipped is what the reader itself refused: lines whose session JSON did not
// parse. The codec counts them and continues, so they never appear in `orig` at
// all — which is precisely why the caller must treat a non-zero count as loss
// rather than as an absence of evidence.
func roundTripEchoReplay(t *testing.T, src string) (frames, skipped, sessionMismatch, frameMismatch int, err error) {
	t.Helper()

	r1, err := codec.NewEchoReplayReader(src)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("open original: %w", err)
	}
	orig, err := r1.ReadFrames()
	skipped = int(r1.SkippedFrames())
	_ = r1.Close()
	if err != nil {
		return 0, skipped, 0, 0, fmt.Errorf("read original: %w", err)
	}
	if len(orig) == 0 {
		return 0, skipped, 0, 0, fmt.Errorf("original had zero frames")
	}

	tmp := filepath.Join(t.TempDir(), filepath.Base(src))
	w, err := codec.NewEchoReplayWriter(tmp)
	if err != nil {
		return 0, skipped, 0, 0, fmt.Errorf("open writer: %w", err)
	}
	for _, f := range orig {
		if err := w.WriteFrame(f); err != nil {
			_ = w.Close()
			return 0, skipped, 0, 0, fmt.Errorf("write frame: %w", err)
		}
	}
	if err := w.Close(); err != nil {
		return 0, skipped, 0, 0, fmt.Errorf("close writer: %w", err)
	}

	r2, err := codec.NewEchoReplayReader(tmp)
	if err != nil {
		return 0, skipped, 0, 0, fmt.Errorf("open round-tripped: %w", err)
	}
	rt, err := r2.ReadFrames()
	skipped += int(r2.SkippedFrames())
	_ = r2.Close()
	if err != nil {
		return 0, skipped, 0, 0, fmt.Errorf("read round-tripped: %w", err)
	}
	if len(rt) != len(orig) {
		return 0, skipped, 0, 0, fmt.Errorf("frame count changed: %d -> %d", len(orig), len(rt))
	}

	for i := range orig {
		if !proto.Equal(orig[i].GetSession(), rt[i].GetSession()) {
			sessionMismatch++
		}
		if !proto.Equal(orig[i], rt[i]) {
			frameMismatch++
		}
	}
	return len(orig), skipped, sessionMismatch, frameMismatch, nil
}
