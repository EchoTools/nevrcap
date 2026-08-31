package conversion

// Change-rate census for the v2 frame message.
//
// docs/format-design.md §2 ("The v2 design philosophy (do not violate this)")
// organizes the format by HOW OFTEN DATA CHANGES: session-constant to the
// header, discrete changes to events, per-frame-varying to per-frame fields.
// This file measures whether the current field placement actually matches that
// principle, instead of reasoning about it from the schema.
//
// SCHEMA PROVENANCE: measured against the generated Go for telemetry/v2 that
// go.mod pins (buf.build/gen/go/echotools/nevr-api). It is NOT read from a
// checked-out .proto -- there are at least five copies of capture.proto on this
// machine (src/nevr-proto, src/demo-viewer, and three module hashes under
// ~/.cache/buf) and this file makes no claim that they agree.
//
// CORPUS: testdata/sample.echoreplay, checked in, via the real ConvertFile
// pipeline. Single recording -- see the gap note on BenchmarkFieldChangeRate.

import (
	"bytes"
	"fmt"
	"sort"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// isolateField returns the wire bytes of exactly one field of m, by clearing
// every other field on a copy. It doubles as the comparison key for "did this
// field change" and as the field's own wire size, so the two can never
// disagree about what a field is.
func isolateField(m protoreflect.Message, keep protoreflect.FieldDescriptor) []byte {
	c := m.New()
	proto.Merge(c.Interface(), m.Interface())
	c.Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		if fd.FullName() != keep.FullName() {
			c.Clear(fd)
		}
		return true
	})
	b, err := proto.Marshal(c.Interface())
	if err != nil {
		return nil
	}
	return b
}

// frameFields returns the EchoArenaFrame field descriptors, in field order.
func frameFields(f *capturepb.Frame) []protoreflect.FieldDescriptor {
	ea := f.GetEchoArena()
	if ea == nil {
		return nil
	}
	fds := ea.ProtoReflect().Descriptor().Fields()
	out := make([]protoreflect.FieldDescriptor, 0, fds.Len())
	for i := range fds.Len() {
		out = append(out, fds.Get(i))
	}
	return out
}

type censusRow struct {
	name       string
	present    int // frames where the field is set/non-zero on the wire
	changed    int // frames where it differs from the previous frame
	totalBytes int // summed wire bytes across all frames
}

// runCensus walks the corpus once and measures presence, change count and wire
// bytes for every top-level EchoArenaFrame field.
func runCensus(frames []*capturepb.Frame) ([]censusRow, int) {
	if len(frames) == 0 {
		return nil, 0
	}
	fds := frameFields(frames[0])
	rows := make(map[string]*censusRow, len(fds))
	prev := map[string][]byte{}
	compared := 0

	for i, f := range frames {
		ea := f.GetEchoArena()
		if ea == nil {
			continue
		}
		m := ea.ProtoReflect()
		for _, fd := range fds {
			name := string(fd.Name())
			r, ok := rows[name]
			if !ok {
				r = &censusRow{name: name}
				rows[name] = r
			}
			b := isolateField(m, fd)
			r.totalBytes += len(b)
			if len(b) > 0 {
				r.present++
			}
			if i > 0 {
				if !bytes.Equal(b, prev[name]) {
					r.changed++
				}
			}
			prev[name] = b
		}
		if i > 0 {
			compared++
		}
	}

	out := make([]censusRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].totalBytes > out[b].totalBytes })
	return out, compared
}

func zstdSize(payloads [][]byte) int {
	var buf bytes.Buffer
	enc, _ := zstd.NewWriter(&buf, zstd.WithEncoderLevel(zstd.SpeedFastest))
	for _, p := range payloads {
		writeDelimited(enc, p)
	}
	enc.Close() //nolint:errcheck // buffer writer
	return buf.Len()
}

// =============================================================================
// QUESTION: which fields of the frame message repeat unchanged across frames
// but are not events -- i.e. where does the current field placement violate the
// format's own organizing principle?
//
// WHY IT MATTERS: docs/format-design.md §2 says v2 is "organized by how often
// data changes" and calls that principle one to not violate. A field that never
// changes is header material; a field that changes rarely is event material.
// Neither should be paid for on every frame. This census is the evidence for
// which is which, so the coming proto revision can be argued from measurement.
//
// ANSWERED WHEN: every top-level frame field has a measured change rate and a
// classification derived from it. The classification thresholds are stated in
// the output, not hidden: <1% changed = session-constant candidate, <10% =
// event candidate, otherwise genuinely per-frame.
//
// GAP, STATED: this runs on ONE recording (testdata/sample.echoreplay). A change
// rate is a property of gameplay as much as of the schema -- a lobby-heavy or
// a goal-heavy capture will move these numbers. Treat the classification as
// this corpus's answer, not the format's, until a second corpus is measured.
// =============================================================================
func BenchmarkFieldChangeRate(b *testing.B) {
	frames := loadCorpusProtos(b)
	rows, compared := runCensus(frames)
	if compared == 0 {
		b.Skip("corpus too short to compare consecutive frames")
	}

	var total int
	for _, r := range rows {
		total += r.totalBytes
	}
	b.Logf("corpus: %d frames, %d consecutive comparisons, %d raw field bytes",
		len(frames), compared, total)
	b.Logf("%-26s %8s %8s %10s %9s  %s", "FIELD", "PRESENT%", "CHANGED%", "rawBytes", "%ofFrame", "CLASS")
	for _, r := range rows {
		chg := 100 * float64(r.changed) / float64(compared)
		class := "per-frame"
		switch {
		case r.present == 0:
			class = "ABSENT-in-corpus"
		case chg < 1:
			class = "session-constant?"
		case chg < 10:
			class = "event-candidate?"
		}
		b.Logf("%-26s %7.1f%% %7.1f%% %10d %8.2f%%  %s",
			r.name,
			100*float64(r.present)/float64(len(frames)),
			chg, r.totalBytes,
			100*float64(r.totalBytes)/float64(total),
			class)
	}
}

// =============================================================================
// QUESTION: what does field repetition actually COST, post-zstd? A field
// repeated a thousand times that compresses to nothing is not a finding; one
// that does not compress away is.
//
// WHY IT MATTERS: raw byte counts systematically overstate the cost of
// repetition, because repetition is exactly what a compressor removes. Any
// argument to move a field out of the frame has to survive compression, or it
// is arguing about bytes that were never on disk.
//
// METHOD: for each field, rebuild the corpus with that field CLEARED on every
// frame where its value equals the previous frame's, compress, and diff against
// the compressed baseline. The difference is what unchanged repetitions of that
// field cost on disk. Measured, not derived from the change rate.
//
// ANSWERED WHEN: each field has a post-zstd repetition cost in bytes and as a
// percentage of the whole tape, so "move this to the header" can be priced.
// =============================================================================
func BenchmarkUnchangedFieldBytesPostZstd(b *testing.B) {
	frames := loadCorpusProtos(b)
	if len(frames) < 2 {
		b.Skip("corpus too short")
	}
	fds := frameFields(frames[0])

	baseline := make([][]byte, 0, len(frames))
	for _, f := range frames {
		p, err := proto.Marshal(f)
		if err != nil {
			b.Fatalf("marshal: %v", err)
		}
		baseline = append(baseline, p)
	}
	base := zstdSize(baseline)
	b.Logf("baseline post-zstd: %d bytes over %d frames (%.1f B/frame)",
		base, len(frames), float64(base)/float64(len(frames)))
	b.Logf("%-26s %12s %10s", "FIELD", "repeatCostB", "%ofTape")

	type res struct {
		name string
		cost int
	}
	var out []res
	for _, fd := range fds {
		variant := make([][]byte, 0, len(frames))
		var prev []byte
		for _, f := range frames {
			c := proto.Clone(f).(*capturepb.Frame)
			if ea := c.GetEchoArena(); ea != nil {
				m := ea.ProtoReflect()
				cur := isolateField(m, fd)
				if prev != nil && bytes.Equal(cur, prev) {
					m.Clear(fd) // this repetition carries no new information
				}
				prev = cur
			}
			p, err := proto.Marshal(c)
			if err != nil {
				b.Fatalf("marshal: %v", err)
			}
			variant = append(variant, p)
		}
		out = append(out, res{string(fd.Name()), base - zstdSize(variant)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].cost > out[j].cost })
	for _, r := range out {
		b.Logf("%-26s %12d %9.3f%%", r.name, r.cost, 100*float64(r.cost)/float64(base))
	}
}

// =============================================================================
// QUESTION: should `blue_points` / `orange_points` / round scores still repeat
// on every frame?
//
// WHY IT MATTERS: docs/format-design.md §2 defends this deliberately -- "scores/
// round repeat every frame, ~6-9 bytes" -- so that "frames are self-contained
// for random access". That reasoning was sound when a keyframe carried nothing
// to start from. BenchmarkHydratedKeyframeCost has since priced a real seed at
// ~270 B, so the trade has moved and is re-derived here rather than defended.
//
// METHOD: measure the post-zstd cost of repeating the score/round fields on
// every frame, against the cost of carrying them only when they change plus a
// hydrated seed at each keyframe interval.
//
// ANSWERED WHEN: the two costs are on the same scale, in the same units, from
// the same corpus.
// =============================================================================
func BenchmarkScoresRepeatVsSeed(b *testing.B) {
	frames := loadCorpusProtos(b)
	if len(frames) < 2 {
		b.Skip("corpus too short")
	}
	scoreFields := map[string]bool{
		"blue_points": true, "orange_points": true,
		"blue_round_score": true, "orange_round_score": true,
		"total_round_count": true, "game_status": true,
	}
	fds := frameFields(frames[0])

	baseline := make([][]byte, 0, len(frames))
	for _, f := range frames {
		p, _ := proto.Marshal(f) //nolint:errcheck // corpus is known-good
		baseline = append(baseline, p)
	}
	base := zstdSize(baseline)

	// Variant: score/round fields present only when they change.
	variant := make([][]byte, 0, len(frames))
	prev := map[string][]byte{}
	changes := 0
	for _, f := range frames {
		c := proto.Clone(f).(*capturepb.Frame)
		if ea := c.GetEchoArena(); ea != nil {
			m := ea.ProtoReflect()
			frameChanged := false
			for _, fd := range fds {
				if !scoreFields[string(fd.Name())] {
					continue
				}
				cur := isolateField(m, fd)
				if p, ok := prev[string(fd.Name())]; ok && bytes.Equal(cur, p) {
					m.Clear(fd)
				} else {
					frameChanged = true
				}
				prev[string(fd.Name())] = cur
			}
			if frameChanged {
				changes++
			}
		}
		p, _ := proto.Marshal(c) //nolint:errcheck // clone of known-good
		variant = append(variant, p)
	}
	onChange := zstdSize(variant)
	saved := base - onChange

	// Report which of the requested fields actually EXIST in the message and
	// are ever set on this corpus. Without this the saving looks like it came
	// from the scores when it may have come from one field that is not a score.
	present := map[string]int{}
	for _, f := range frames {
		if ea := f.GetEchoArena(); ea != nil {
			m := ea.ProtoReflect()
			for _, fd := range fds {
				if scoreFields[string(fd.Name())] && len(isolateField(m, fd)) > 0 {
					present[string(fd.Name())]++
				}
			}
		}
	}
	inMsg := map[string]bool{}
	for _, fd := range fds {
		inMsg[string(fd.Name())] = true
	}
	for _, name := range sortedKeys(scoreFields) {
		switch {
		case !inMsg[name]:
			b.Logf("  %-20s NOT A FIELD of this message -- excluded", name)
		case present[name] == 0:
			b.Logf("  %-20s field exists but is NEVER SET on this corpus "+
				"(proto3 omits zero values) -- contributes nothing", name)
		default:
			b.Logf("  %-20s set on %d/%d frames", name, present[name], len(frames))
		}
	}
	b.Logf("corpus %d frames; requested score/round fields: %v", len(frames), sortedKeys(scoreFields))
	b.Logf("repeat-every-frame (today): %d B post-zstd", base)
	b.Logf("emit-on-change only:        %d B post-zstd", onChange)
	b.Logf("SAVED by not repeating:     %d B (%.3f%% of tape), over %d frames with a change",
		saved, 100*float64(saved)/float64(base), changes)
	b.Logf("compare: a hydrated seed measured at ~270 B/keyframe "+
		"(BenchmarkHydratedKeyframeCost); at 1/s over this corpus that is %d keyframes",
		len(frames)/benchFPS)
	b.ReportMetric(float64(saved), "savedB")
	b.ReportMetric(100*float64(saved)/float64(base), "%%ofTape")
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

var _ = fmt.Sprintf // keep fmt if a future row needs formatting
