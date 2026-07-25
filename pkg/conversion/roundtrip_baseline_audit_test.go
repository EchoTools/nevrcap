package conversion

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"github.com/echotools/tape/pkg/codec"
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
// BAC: for every audited recording, echoreplay -> v1 codec -> echoreplay
// preserves the complete SessionResponse of every frame, field for field.
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

	for _, f := range files {
		frames, sessionMismatch, frameMismatch, err := roundTripEchoReplay(t, f)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		totalFrames += frames
		status := "LOSSLESS"
		if sessionMismatch > 0 {
			status = "LOSSY"
			lossy++
		}
		t.Logf("%-8s %-70s frames=%-6d session-mismatch=%d whole-frame-mismatch=%d",
			status, filepath.Base(f), frames, sessionMismatch, frameMismatch)
	}

	t.Logf("AUDIT SUMMARY: %d file(s), %d frames, %d lossy", len(files), totalFrames, lossy)
	if lossy > 0 {
		t.Fatalf("ECHOREPLAY ROUND-TRIP IS LOSSY on %d/%d audited file(s)", lossy, len(files))
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
func roundTripEchoReplay(t *testing.T, src string) (frames, sessionMismatch, frameMismatch int, err error) {
	t.Helper()

	r1, err := codec.NewEchoReplayReader(src)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("open original: %w", err)
	}
	orig, err := r1.ReadFrames()
	_ = r1.Close()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read original: %w", err)
	}
	if len(orig) == 0 {
		return 0, 0, 0, fmt.Errorf("original had zero frames")
	}

	tmp := filepath.Join(t.TempDir(), filepath.Base(src))
	w, err := codec.NewEchoReplayWriter(tmp)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("open writer: %w", err)
	}
	for _, f := range orig {
		if err := w.WriteFrame(f); err != nil {
			_ = w.Close()
			return 0, 0, 0, fmt.Errorf("write frame: %w", err)
		}
	}
	if err := w.Close(); err != nil {
		return 0, 0, 0, fmt.Errorf("close writer: %w", err)
	}

	r2, err := codec.NewEchoReplayReader(tmp)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("open round-tripped: %w", err)
	}
	rt, err := r2.ReadFrames()
	_ = r2.Close()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("read round-tripped: %w", err)
	}
	if len(rt) != len(orig) {
		return 0, 0, 0, fmt.Errorf("frame count changed: %d -> %d", len(orig), len(rt))
	}

	for i := range orig {
		if !proto.Equal(orig[i].GetSession(), rt[i].GetSession()) {
			sessionMismatch++
		}
		if !proto.Equal(orig[i], rt[i]) {
			frameMismatch++
		}
	}
	return len(orig), sessionMismatch, frameMismatch, nil
}
