package conversion

// Format-property benchmarks for the .tape container.
//
// These benchmarks exist to decide FORMAT questions while the format is still
// unreleased and breaking changes are free. They are deliberately written
// against format PROPERTIES -- bytes per frame at interval X, decode cost to
// reach frame N, whether byte-range access is possible at all -- and NOT
// against the current writer API. A benchmark coupled to
// NewWriterWithKeyframeInterval would obstruct the very change it exists to
// measure, and would then be deleted rather than fixed.
//
// Every benchmark below opens with the QUESTION it answers, WHY IT MATTERS,
// and what would ANSWER it. A benchmark whose question is settled says so and
// stays as a regression guard; it is not silently re-run forever as if open.
//
// FIXED CORPUS: testdata/sample.echoreplay, checked into this repo. Numbers are
// only comparable across commits if the input does not drift, so the corpus is
// pinned rather than taken from a scratch directory. Frames are the real
// marshalled Envelope payloads produced by the real conversion pipeline.
//
// -----------------------------------------------------------------------------
// ESTABLISHED BEFORE WRITING THESE -- read this before interpreting any number.
//
// The container does NOT delta-encode frames, and a "keyframe" is NOT a
// hydrated snapshot.
//
//   - Writer.WriteFrame (tape.go:154-160) appends a KeyframeEntry{FrameIndex,
//     ByteOffset} every keyframeInterval frames. That is an INDEX ENTRY. The
//     frame written at a keyframe boundary is in every way an ordinary frame;
//     no state is hydrated into it and no neighbouring frame is shrunk.
//     Therefore the keyframe interval changes ONLY footer index density -- it
//     does not change the frame stream at all.
//
//   - Telemetry v2 IS a delta format at the SEMANTIC level, one layer up:
//     identity, loadout, grab, stats, the scoreboard and the last goal are
//     carried by events and materialized by replaying them from the beginning
//     (see cmd/tapedeck/trim_seed.go:15-21). State is accumulated by replay,
//     not restated per frame.
//
// The two facts together are the finding: to reach frame N you must replay
// events from frame 0, so a KeyframeEntry gives you a byte offset but NOT a
// state seed -- and because the whole container is one zstd stream
// (tape.go:97), you cannot seek to that byte offset either. Today a keyframe
// delivers neither of the two things a keyframe normally provides.
//
// This is why the "keyframe interval" axis is measured here as what it
// currently is (footer cost), and the substantive axis is the COMPRESSION
// LAYOUT, which is what actually decides byte-range access.
// -----------------------------------------------------------------------------

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/echotools/tape/v4/pkg/codec"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
)

// convertCorpus turns the checked-in .echoreplay into a real .tape using the
// real pipeline. Going through ConvertFile rather than hand-building frames is
// deliberate: the corpus must be what the product actually produces.
func convertCorpus(src, dst string) error {
	_, err := ConvertFile(src, dst)
	return err
}

// readFramePayloads reads a .tape through the public reader and returns each
// frame's marshalled bytes. It deliberately does not reimplement framing, so it
// keeps working if the container framing changes.
func readFramePayloads(tapePath string) ([][]byte, error) {
	r, err := codec.NewReader(tapePath)
	if err != nil {
		return nil, err
	}
	defer r.Close() //nolint:errcheck // read-only

	if _, err := r.ReadHeader(); err != nil {
		return nil, err
	}
	var out [][]byte
	for {
		f, err := r.ReadFrame()
		if err != nil {
			break // io.EOF at the footer terminates the stream
		}
		b, err := proto.Marshal(f)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// Frame rate the intervals below are expressed against. The corpus is replayed
// at engine rate; 30fps is the production producer rate measured 2026-08-31.
const benchFPS = 30

// keyframeIntervalCases is the interval axis, in FRAMES, named in seconds so a
// reader can tell what question is being asked. 0 means "none".
var keyframeIntervalCases = []struct {
	name   string
	frames int
}{
	{"1s", 1 * benchFPS},
	{"2s", 2 * benchFPS},
	{"5s", 5 * benchFPS},
	{"none", 0},
}

var (
	corpusOnce   sync.Once
	corpusFrames [][]byte // marshalled Envelope payloads, in order
	corpusErr    error
)

// loadCorpusFrames returns the fixed corpus as raw per-frame payloads. It reads
// the container through the public reader rather than reimplementing framing,
// so the corpus stays correct if the framing changes.
func loadCorpusFrames(tb testing.TB) [][]byte {
	tb.Helper()
	corpusOnce.Do(func() {
		const samplePath = "../../testdata/sample.echoreplay"
		if _, err := os.Stat(samplePath); os.IsNotExist(err) {
			return // corpusFrames stays nil; callers skip
		}
		dir, err := os.MkdirTemp("", "tapebench")
		if err != nil {
			corpusErr = err
			return
		}
		defer os.RemoveAll(dir) //nolint:errcheck // best-effort cleanup

		tapePath := filepath.Join(dir, "corpus.tape")
		if corpusErr = convertCorpus(samplePath, tapePath); corpusErr != nil {
			return
		}
		corpusFrames, corpusErr = readFramePayloads(tapePath)
	})
	if corpusErr != nil {
		tb.Fatalf("corpus: %v", corpusErr)
	}
	if len(corpusFrames) == 0 {
		tb.Skip("testdata/sample.echoreplay not available; corpus benchmarks skipped")
	}
	return corpusFrames
}

// --- layouts -----------------------------------------------------------------
//
// Each layout answers the same two questions in different ways: how many bytes,
// and can a reader jump to frame N without decompressing everything before it.

type layoutResult struct {
	bytes         int
	byteRangeable bool // BINARY, and the column that matters most
}

// layoutWholeStream is today's container: one continuous zstd stream over every
// frame. Byte offsets recorded in the footer are positions in the DECOMPRESSED
// stream, so nothing can seek to them.
func layoutWholeStream(frames [][]byte, level zstd.EncoderLevel) layoutResult {
	var buf bytes.Buffer
	enc, _ := zstd.NewWriter(&buf, zstd.WithEncoderLevel(level))
	for _, f := range frames {
		writeDelimited(enc, f)
	}
	enc.Close() //nolint:errcheck // buffer writer
	return layoutResult{bytes: buf.Len(), byteRangeable: false}
}

// layoutPerBlock emits one INDEPENDENT zstd frame per block of n frames. Each
// block can be decompressed without the ones before it, so byte-range access
// becomes possible. The cost is that zstd's match window resets every block.
func layoutPerBlock(frames [][]byte, n int, level zstd.EncoderLevel, dict []byte) layoutResult {
	if n <= 0 {
		n = len(frames)
	}
	total := 0
	for start := 0; start < len(frames); start += n {
		end := min(start+n, len(frames))
		var raw bytes.Buffer
		for _, f := range frames[start:end] {
			writeDelimited(&raw, f)
		}
		opts := []zstd.EOption{zstd.WithEncoderLevel(level)}
		if dict != nil {
			opts = append(opts, zstd.WithEncoderDict(dict))
		}
		var out bytes.Buffer
		enc, _ := zstd.NewWriter(&out, opts...)
		enc.Write(raw.Bytes()) //nolint:errcheck // buffer writer
		enc.Close()            //nolint:errcheck // buffer writer
		total += out.Len()
	}
	// A per-block layout needs a seek table to be useful. The zstd seekable
	// format puts one in a SKIPPABLE frame (magic 0x184D2A5E) with a 9-byte
	// footer, so decoders that do not know about it skip the index instead of
	// choking. Entries are 8 bytes (compressed size, decompressed size), or 12
	// with the optional checksum. That overhead is charged here rather than
	// quietly omitted, so the size comparison is honest.
	blocks := (len(frames) + n - 1) / n
	total += blocks*8 + 9 + 8 // entries + footer + skippable frame header
	return layoutResult{bytes: total, byteRangeable: true}
}

func writeDelimited(w interface{ Write([]byte) (int, error) }, b []byte) {
	var hdr [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(hdr[:], uint64(len(b)))
	w.Write(hdr[:n]) //nolint:errcheck // buffer/encoder writer
	w.Write(b)       //nolint:errcheck // buffer/encoder writer
}

// buildCorpusDict trains a zstd dictionary on the corpus frames.
func buildCorpusDict(tb testing.TB, frames [][]byte) []byte {
	tb.Helper()
	sample := frames
	if len(sample) > 2000 {
		sample = sample[:2000]
	}
	// History is the dictionary CONTENT (the window a block may match
	// against); Contents are the samples the entropy tables are trained on.
	// Both are required -- BuildDict rejects an empty History.
	var hist []byte
	const histCap = 112 << 10 // conventional zstd dictionary size
	for _, f := range sample {
		if len(hist)+len(f) > histCap {
			break
		}
		hist = append(hist, f...)
	}
	d, err := zstd.BuildDict(zstd.BuildDictOptions{
		ID:       1,
		Contents: sample,
		History:  hist,
		Level:    zstd.SpeedDefault,
	})
	if err != nil {
		tb.Skipf("BuildDict: %v", err)
	}
	return d
}

// =============================================================================
// QUESTION: what does one frame actually cost on disk, after compression, in a
// real recording?
//
// WHY IT MATTERS: every other number in this file is a ratio against this one,
// and every capacity estimate for nevr-stream (disk retention, tape sizes,
// memory for a live ring) is derived from it. A guessed frame size silently
// propagates into all of them.
//
// ANSWERED WHEN: this is a SETTLED question. It is kept as a regression guard:
// if bytes/frame moves materially without an intentional format change, the
// format changed under us.
// =============================================================================
func BenchmarkFrameBytesOnDisk(b *testing.B) {
	frames := loadCorpusFrames(b)
	var raw int
	for _, f := range frames {
		raw += len(f)
	}
	res := layoutWholeStream(frames, zstd.SpeedFastest)

	b.ReportMetric(float64(len(frames)), "frames")
	b.ReportMetric(float64(raw)/float64(len(frames)), "rawB/frame")
	b.ReportMetric(float64(res.bytes)/float64(len(frames)), "zstdB/frame")
	b.ReportMetric(float64(raw)/float64(res.bytes), "ratio")
	b.ReportMetric(float64(len(frames))/benchFPS, "seconds@30fps")
}

// =============================================================================
// QUESTION: at 30fps, how much tape SIZE does a 1-second keyframe interval cost
// versus 2s, 5s, or none at all?
//
// WHY IT MATTERS: Andrew's "one per second" is a round number, not a measured
// one. If the cost is 3% the interval should be dense; if it is 30% it is a
// real trade that has to be argued.
//
// ANSWERED WHEN: the size curve across 1s/2s/5s/none is flat or it is not.
//
// STATUS -- READ THIS: with the container as it stands the question is
// ANSWERED, and the answer is that the axis is DEGENERATE. A keyframe is an
// index entry only (see the file header), so the interval changes the FOOTER
// and nothing else; the frame stream is byte-identical at every interval. This
// benchmark therefore measures the footer cost, which is the true cost today,
// and it exists to catch the moment that stops being true -- if keyframes ever
// carry hydrated state, this curve stops being flat and the question reopens by
// itself.
// =============================================================================
func BenchmarkKeyframeIntervalSizeCost(b *testing.B) {
	frames := loadCorpusFrames(b)
	base := layoutWholeStream(frames, zstd.SpeedFastest).bytes

	for _, kc := range keyframeIntervalCases {
		b.Run(kc.name, func(b *testing.B) {
			entries := 0
			if kc.frames > 0 {
				entries = (len(frames) + kc.frames - 1) / kc.frames
			}
			// A KeyframeEntry is two varint fields (frame index, byte offset);
			// 8 bytes is a generous upper bound at these magnitudes.
			footer := entries * 8
			b.ReportMetric(float64(entries), "keyframes")
			b.ReportMetric(float64(base+footer), "totalB")
			b.ReportMetric(100*float64(footer)/float64(base), "%%overFrames")
		})
	}
}

// =============================================================================
// QUESTION: can a reader fetch frame N without decompressing everything before
// it -- and what does making that possible cost in bytes?
//
// WHY IT MATTERS: this is the question that decides whether seek, trim, range
// download and partial replay are cheap or impossible. It is BINARY before it
// is numeric: today the answer is NO for the shipped layout, because
// zstd.NewWriter wraps the entire container in one continuous stream
// (tape.go:97), which makes the footer's byte offsets unusable for seeking.
//
// Per-block layouts make it YES. The zstd seekable format is the published
// solution to exactly this shape -- independent frames plus a seek table in a
// SKIPPABLE frame (magic 0x184D2A5E, 9-byte footer, magic 0x8F92EAB1) that
// older decoders ignore rather than choke on. The seek-table overhead is
// charged in the numbers below.
//
// ANSWERED WHEN: the byte cost of per-block independence is known at each block
// size, and we can say whether a trained dictionary recovers the ratio lost to
// resetting the match window every block.
// =============================================================================
func BenchmarkByteRangeAccessCost(b *testing.B) {
	frames := loadCorpusFrames(b)
	whole := layoutWholeStream(frames, zstd.SpeedFastest)
	dict := buildCorpusDict(b, frames)

	report := func(b *testing.B, r layoutResult) {
		b.ReportMetric(float64(r.bytes), "totalB")
		b.ReportMetric(float64(r.bytes)/float64(len(frames)), "B/frame")
		b.ReportMetric(100*float64(r.bytes-whole.bytes)/float64(whole.bytes), "%%vsWhole")
		v := 0.0
		if r.byteRangeable {
			v = 1
		}
		b.ReportMetric(v, "byteRangeable")
	}

	b.Run("whole-stream", func(b *testing.B) { report(b, whole) })

	for _, kc := range keyframeIntervalCases {
		if kc.frames == 0 {
			continue // "none" == whole-stream, already reported
		}
		b.Run("per-block/"+kc.name, func(b *testing.B) {
			report(b, layoutPerBlock(frames, kc.frames, zstd.SpeedFastest, nil))
		})
		b.Run("per-block+dict/"+kc.name, func(b *testing.B) {
			report(b, layoutPerBlock(frames, kc.frames, zstd.SpeedFastest, dict))
		})
	}
	b.ReportMetric(float64(len(dict)), "dictB")
}

// =============================================================================
// QUESTION: how expensive is it to reach frame N, and does a per-block layout
// actually make it cheaper in practice rather than just in principle?
//
// WHY IT MATTERS: "seek to frame N" is the operation trim, replay and the live
// stream's seek control all rest on. If reaching a frame late in a capture
// costs a full decompress regardless of layout, then byte-range access buys
// nothing and the format question is settled the other way.
//
// ANSWERED WHEN: decode-to-frame-N is measured for both layouts at an N deep
// enough to matter (90% through the capture). Note this measures CONTAINER
// decode only -- it does NOT include replaying events to hydrate state, which
// is a separate and unavoidable cost under a semantic delta format.
// =============================================================================
func BenchmarkDecodeToFrameN(b *testing.B) {
	frames := loadCorpusFrames(b)
	target := len(frames) * 9 / 10
	dict := buildCorpusDict(b, frames)

	b.Run("whole-stream", func(b *testing.B) {
		var buf bytes.Buffer
		enc, _ := zstd.NewWriter(&buf, zstd.WithEncoderLevel(zstd.SpeedFastest))
		for _, f := range frames {
			writeDelimited(enc, f)
		}
		enc.Close() //nolint:errcheck // buffer writer
		blob := buf.Bytes()
		b.SetBytes(int64(len(blob)))
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			dec, _ := zstd.NewReader(bytes.NewReader(blob))
			out, _ := dec.DecodeAll(blob, nil) //nolint:errcheck // benchmark
			_ = out
			dec.Close()
		}
		b.StopTimer()
		b.ReportMetric(float64(target), "targetFrame")
		b.ReportMetric(1, "blocksDecompressed")
	})

	for _, kc := range keyframeIntervalCases {
		if kc.frames == 0 {
			continue
		}
		b.Run("per-block/"+kc.name, func(b *testing.B) {
			var blocks [][]byte
			for start := 0; start < len(frames); start += kc.frames {
				end := min(start+kc.frames, len(frames))
				var raw bytes.Buffer
				for _, f := range frames[start:end] {
					writeDelimited(&raw, f)
				}
				var out bytes.Buffer
				enc, _ := zstd.NewWriter(&out, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderDict(dict))
				enc.Write(raw.Bytes()) //nolint:errcheck // buffer writer
				enc.Close()            //nolint:errcheck // buffer writer
				blocks = append(blocks, out.Bytes())
			}
			// Byte-range access: only the block containing the target is read.
			blk := blocks[target/kc.frames]
			b.SetBytes(int64(len(blk)))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				dec, _ := zstd.NewReader(nil, zstd.WithDecoderDicts(dict))
				out, _ := dec.DecodeAll(blk, nil) //nolint:errcheck // benchmark
				_ = out
				dec.Close()
			}
			b.StopTimer()
			b.ReportMetric(float64(target), "targetFrame")
			b.ReportMetric(1, "blocksDecompressed")
			b.ReportMetric(float64(len(blocks)), "totalBlocks")
		})
	}
}

// =============================================================================
// QUESTION: does encoder layout change how much memory a live capture churns
// through while it is being written?
//
// WHY IT MATTERS: nevr-stream holds an open writer per live match on a box with
// mem_limit 512m, so writer-side memory behaviour is a production capacity
// question, not a tidiness one.
//
// ANSWERED WHEN: NOT YET, and read this before quoting the numbers. This
// measures ALLOCATION CHURN (TotalAlloc delta), which is a proxy and not the
// same thing as peak resident memory -- and because the churn accumulates
// across b.N iterations it is comparable BETWEEN layouts in the same run and
// meaningless as an absolute. Per-block encoders allocate more total because
// each block constructs a fresh encoder; that says nothing about the high-water
// mark, which is what the 512m limit actually constrains. A real answer needs
// peak RSS of a single encode under a memory profiler, or the running service
// measured directly. Left in place because the relative churn signal is still
// worth tracking across format changes; do not cite it as peak memory.
// =============================================================================
func BenchmarkEncodePeakMemory(b *testing.B) {
	frames := loadCorpusFrames(b)

	measure := func(b *testing.B, fn func()) {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		fn()
		runtime.ReadMemStats(&after)
		b.ReportMetric(float64(after.TotalAlloc-before.TotalAlloc)/(1<<20), "churnMB")
		b.ReportMetric(float64(after.HeapSys)/(1<<20), "heapSysMB")
	}

	b.Run("whole-stream", func(b *testing.B) {
		b.ResetTimer()
		measure(b, func() { layoutWholeStream(frames, zstd.SpeedFastest) })
	})
	for _, kc := range keyframeIntervalCases {
		if kc.frames == 0 {
			continue
		}
		b.Run("per-block/"+kc.name, func(b *testing.B) {
			b.ResetTimer()
			measure(b, func() { layoutPerBlock(frames, kc.frames, zstd.SpeedFastest, nil) })
		})
	}
}
