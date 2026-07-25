package fidelity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Allowlist is the ONLY escape hatch from "any difference is an error". It maps
// a field path — or the root of a field subtree — to the reason that loss is
// accepted, which must cite where the decision is recorded (BUGS.md).
//
// Matching is exact, or subtree: an entry for "SessionResponse.last_throw"
// covers "SessionResponse.last_throw#presence" and
// "SessionResponse.last_throw.arm_speed", because the recorded reason ("v2 only
// carries throws as events") is a statement about the whole message. Subtree
// matching never crosses a name boundary: "SessionResponse.last_score" does not
// match "SessionResponse.last_score_extra".
//
// Adding an entry is a documented hole in the archive format. It is not a way
// to make a test green.
type Allowlist map[string]string

// Reason reports whether path is allowed, and why.
func (a Allowlist) Reason(path string) (string, bool) {
	if a == nil {
		return "", false
	}
	if r, ok := a[path]; ok {
		return r, true
	}
	for k, r := range a {
		if len(path) <= len(k) || !strings.HasPrefix(path, k) {
			continue
		}
		switch path[len(k)] {
		case '.', '[', '{', '#':
			return r, true
		}
	}
	return "", false
}

// RootReport is one compared message type's result within a Verdict.
type RootReport struct {
	Root      string // schema root name, e.g. "SessionResponse"
	Unreached []string
	Diffs     []Diff
	// Compared is the number of message pairs walked (one per frame).
	Compared int
	// SchemaPaths is every field path in the schema; Reached is how many of
	// them the walk actually visited.
	SchemaPaths int
	Reached     int
}

// Failures is the subset of Diffs not covered by the allowlist.
func (r RootReport) Failures() []Diff {
	var out []Diff
	for _, d := range r.Diffs {
		if !d.Allowed {
			out = append(out, d)
		}
	}
	return out
}

// Allowed is the subset of Diffs covered by the allowlist.
func (r RootReport) Allowed() []Diff {
	var out []Diff
	for _, d := range r.Diffs {
		if d.Allowed {
			out = append(out, d)
		}
	}
	return out
}

// Verdict is the structured, per-file result of a round-trip verification: what
// was verified, by identity; whether every schema field was reached; every
// field path that differed; and a single pass/fail. It is built to be a
// receipt — the thing you keep after deleting the original.
type Verdict struct {
	Source       string
	SourceSHA256 string

	Roots []RootReport
	// Errors holds anything that stopped the verification from being complete.
	Errors []string

	// KeyScan is the raw-bytes key-set check: JSON keys present in the file
	// that the proto has no field for, plus payloads that did not parse at all.
	// The comparison lanes cannot see either, because both sides of that
	// comparison come from the same discarding reader.
	KeyScan KeyScanResult

	SourceBytes int64

	// Frames on each side of the comparison. A difference is fatal on its own:
	// a frame missing from one side is content that was never compared.
	FramesOriginal int
	FramesRecon    int

	// SkippedFrames is what the codec's own reader dropped: lines whose session
	// JSON did not parse. Such a frame is absent from BOTH sides of the
	// comparison, exactly like an unknown key, so its count is fatal.
	SkippedFrames int
}

// Failures is every non-allowlisted field-path difference across all roots.
func (v *Verdict) Failures() []Diff {
	var out []Diff
	for _, r := range v.Roots {
		out = append(out, r.Failures()...)
	}
	return out
}

// Unreached is every schema path no walk visited, across all roots.
func (v *Verdict) Unreached() []string {
	var out []string
	for _, r := range v.Roots {
		out = append(out, r.Unreached...)
	}
	return out
}

// Pass reports whether this file round-tripped completely. Every lane is fatal,
// and each is fatal for the same reason: anything it counts is content that was
// either lost or never compared.
func (v *Verdict) Pass() bool { return len(v.FailureReasons()) == 0 }

// FailureReasons lists, in one line each, why the verdict does not pass. Empty
// means the file round-tripped completely.
func (v *Verdict) FailureReasons() []string {
	var out []string
	for _, e := range v.Errors {
		out = append(out, "error: "+e)
	}
	if v.FramesOriginal != v.FramesRecon {
		out = append(out, fmt.Sprintf("frame count: original=%d reconstruction=%d", v.FramesOriginal, v.FramesRecon))
	}
	if v.SkippedFrames > 0 {
		out = append(out, fmt.Sprintf("codec dropped %d unparseable frame(s) at read time: absent from both sides of the comparison", v.SkippedFrames))
	}
	// Independent witness: the key scanner counts frame LINES in the file's own
	// bytes, the comparison counts frames the codec produced. A gap is content
	// that never entered the comparison, whether or not the codec admitted to
	// dropping it. Two counters that can only agree when nothing was lost are
	// worth more than either one's self-report.
	if v.KeyScan.FramesTotal > 0 && v.KeyScan.FramesTotal != v.FramesOriginal {
		out = append(out, fmt.Sprintf("the file contains %d frame line(s) but the comparison saw %d: "+
			"%d frame(s) never reached it", v.KeyScan.FramesTotal, v.FramesOriginal, v.KeyScan.FramesTotal-v.FramesOriginal))
	}
	if v.KeyScan.ParseErrors > 0 {
		out = append(out, fmt.Sprintf("%d payload(s) are not parseable JSON", v.KeyScan.ParseErrors))
	}
	if n := len(v.KeyScan.Findings); n > 0 {
		out = append(out, fmt.Sprintf("%d JSON key(s) present in the file have no proto field: discarded at read time", n))
	}
	for _, r := range v.Roots {
		if n := len(r.Unreached); n > 0 {
			out = append(out, fmt.Sprintf("%s: %d/%d schema field(s) never reached by the comparison", r.Root, n, r.SchemaPaths))
		}
		for _, d := range r.Failures() {
			out = append(out, fmt.Sprintf("%s does not round-trip in %d comparison(s), e.g. %s "+
				"— it is not in the allowlist: either fix the loss, or record it with a BUGS.md citation. "+
				"Do not add an entry to make this green without a ruling", d.Path, d.Count, d.Example))
		}
	}
	return out
}

// String renders the receipt.
func (v *Verdict) String() string {
	var b strings.Builder
	verdict := "PASS"
	if !v.Pass() {
		verdict = "FAIL"
	}
	fmt.Fprintf(&b, "FIDELITY %s  %s\n", verdict, v.Source)
	fmt.Fprintf(&b, "  identity: %d bytes sha256=%s\n", v.SourceBytes, v.SourceSHA256)
	fmt.Fprintf(&b, "  frames: original=%d reconstruction=%d codec-skipped=%d\n", v.FramesOriginal, v.FramesRecon, v.SkippedFrames)
	fmt.Fprintf(&b, "  keyscan: %s\n", v.KeyScan.Summary())
	for _, r := range v.Roots {
		fmt.Fprintf(&b, "  %s: compared %d pair(s), reached %d/%d schema fields\n", r.Root, r.Compared, r.Reached, r.SchemaPaths)
		for _, p := range r.Unreached {
			fmt.Fprintf(&b, "    UNREACHED  %s\n", p)
		}
		for _, d := range r.Allowed() {
			fmt.Fprintf(&b, "    allowed    %-64s x%-8d %s\n      (%s)\n", d.Path, d.Count, d.Example, d.Reason)
		}
		for _, d := range r.Failures() {
			fmt.Fprintf(&b, "    DIFF       %-64s x%-8d %s\n", d.Path, d.Count, d.Example)
		}
	}
	for _, e := range v.Errors {
		fmt.Fprintf(&b, "  ERROR %s\n", e)
	}
	return b.String()
}

// Identify fills in the source identity of a verdict: size and content hash.
// The hash is what makes the verdict a receipt for a SPECIFIC file rather than
// a claim about a filename.
func (v *Verdict) Identify(path string) (err error) {
	v.Source = path
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	v.SourceBytes = fi.Size()
	f, err := os.Open(path) //nolint:gosec // path is caller-provided
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	v.SourceSHA256 = hex.EncodeToString(h.Sum(nil))
	return nil
}

// Classify marks each diff against the allowlist. It is called by the driver
// once the comparison is complete.
func (v *Verdict) Classify(a Allowlist) {
	for i := range v.Roots {
		for j := range v.Roots[i].Diffs {
			d := &v.Roots[i].Diffs[j]
			d.Reason, d.Allowed = a.Reason(d.Path)
		}
	}
}
