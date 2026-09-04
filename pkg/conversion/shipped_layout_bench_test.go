package conversion

// Does the SHIPPED per-block writer deliver the numbers the prototype promised?
//
// WHY THIS FILE IS SEPARATE FROM format_properties_bench_test.go. That suite
// measures format PROPERTIES using layouts built inline, on purpose: it was
// written to decide whether per-block was worth implementing at all, and a
// benchmark bound to the writer API would have obstructed the change it existed
// to measure. Its per-block numbers are therefore a MODEL of a layout, not a
// measurement of one -- no header block, no footer block, no real envelope
// framing around the header, an estimated seek table rather than a written one.
//
// The layout now exists (pkg/codec/tape_blocks.go, opt-in via
// codec.WithPerBlockCompression). This file measures THAT -- the bytes an
// actual codec.Writer puts on disk and the cost an actual reader pays to reach
// frame N through codec.OpenBlockIndex. The two files answer the same questions
// about different objects, and the gap between them is itself a finding: it is
// the price of the parts a model leaves out.
//
// FIXED CORPUS: testdata/sample.echoreplay, the same corpus the property suite
// pins, so the two sets of numbers are comparable.

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/echotools/tape/v4/pkg/codec"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// shippedHeader is a fixed header so size numbers do not drift with a clock.
func shippedHeader() *capturepb.CaptureHeader {
	return &capturepb.CaptureHeader{
		CaptureId:     "bench-corpus",
		CreatedAt:     timestamppb.New(time.Unix(1756600000, 0).UTC()),
		FormatVersion: 2,
		GameHeader: &capturepb.CaptureHeader_EchoArena{
			EchoArena: &capturepb.EchoArenaHeader{
				SessionId: "BENCH-001",
				MapName:   "mpl_arena_a",
				MatchType: capturepb.MatchType_MATCH_TYPE_ARENA,
			},
		},
	}
}

// writeShipped writes the corpus through the real codec.Writer with the given
// options and returns the resulting file's size in bytes.
func writeShipped(tb testing.TB, dir, name string, frames []*capturepb.Frame, opts ...codec.WriterOption) (string, int64) {
	tb.Helper()
	path := filepath.Join(dir, name)
	w, err := codec.NewWriterWithOptions(path, opts...)
	if err != nil {
		tb.Fatalf("NewWriterWithOptions: %v", err)
	}
	if err := w.WriteHeader(shippedHeader()); err != nil {
		tb.Fatalf("WriteHeader: %v", err)
	}
	for _, f := range frames {
		if err := w.WriteFrame(f); err != nil {
			tb.Fatalf("WriteFrame %d: %v", f.GetFrameIndex(), err)
		}
	}
	if err := w.Close(); err != nil {
		tb.Fatalf("Close: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		tb.Fatalf("stat: %v", err)
	}
	return path, info.Size()
}

// corpusDict trains a dictionary on the corpus through the package's own
// trainer (codec.TrainDictionary), so the benchmark measures the dictionary a
// caller would actually get rather than a hand-rolled equivalent.
func corpusDict(tb testing.TB, frames []*capturepb.Frame) []byte {
	tb.Helper()
	samples := make([][]byte, len(frames))
	for i, f := range frames {
		b, err := proto.Marshal(f)
		if err != nil {
			tb.Fatalf("Marshal: %v", err)
		}
		samples[i] = b
	}
	d, err := codec.TrainDictionary(codec.MinPrivateDictionaryID, samples)
	if err != nil {
		tb.Skipf("TrainDictionary: %v", err)
	}
	return d
}

// =============================================================================
// QUESTION: what does the SHIPPED per-block writer actually cost in bytes,
// against the shipped whole-stream writer, on the same corpus?
//
// WHY IT MATTERS: the decision to implement per-block was made on modelled
// numbers -- 1509 B/frame for per-block+dict against 1601 shipped, 5.7% smaller
// (2026-08-31-tape-transcode-spec.md, row F1). A model that omits the header
// block, the footer block and the real seek table can only be optimistic. If
// the real writer lands materially off that, the decision was made on a number
// that does not exist and the gap has to be stated, not smoothed over.
//
// ANSWERED WHEN: bytes/frame for the real writer is known at each interval,
// with and without a dictionary, and stated next to the modelled figure.
// =============================================================================
func BenchmarkShippedLayoutSize(b *testing.B) {
	frames := loadCorpusProtos(b)
	dict := corpusDict(b, frames)
	dir := b.TempDir()

	_, wholeBytes := writeShipped(b, dir, "whole.tape", frames)

	report := func(b *testing.B, size int64, rangeable bool) {
		b.ReportMetric(float64(size), "totalB")
		b.ReportMetric(float64(size)/float64(len(frames)), "B/frame")
		b.ReportMetric(100*float64(size-wholeBytes)/float64(wholeBytes), "%%vsWhole")
		v := 0.0
		if rangeable {
			v = 1
		}
		b.ReportMetric(v, "byteRangeable")
	}

	b.Run("whole-stream", func(b *testing.B) {
		for b.Loop() {
			_ = wholeBytes
		}
		report(b, wholeBytes, false)
	})

	for _, kc := range keyframeIntervalCases {
		if kc.frames == 0 {
			continue // "none" is the whole-stream row
		}
		interval := uint32(kc.frames) //nolint:gosec // fixed small constants

		b.Run("per-block/"+kc.name, func(b *testing.B) {
			_, size := writeShipped(b, dir, "pb-"+kc.name+".tape", frames,
				codec.WithKeyframeInterval(interval), codec.WithPerBlockCompression())
			for b.Loop() {
				_ = size
			}
			report(b, size, true)
		})

		b.Run("per-block+dict/"+kc.name, func(b *testing.B) {
			_, size := writeShipped(b, dir, "pbd-"+kc.name+".tape", frames,
				codec.WithKeyframeInterval(interval), codec.WithPerBlockCompression(),
				codec.WithDictionary(dict))
			for b.Loop() {
				_ = size
			}
			report(b, size, true)
		})
	}
	b.ReportMetric(float64(len(dict)), "dictB")
	b.ReportMetric(float64(len(frames)), "frames")
}

// =============================================================================
// QUESTION: through the SHIPPED reader API, what does it cost to reach frame N
// deep in a capture?
//
// WHY IT MATTERS: the modelled figure was 3.17 ms -> 0.098 ms, 32x
// (2026-08-31-tape-transcode-spec.md, row F1). That model decompressed a block
// it had already located. The real path has to FIND the block first: open the
// file, read the seek table backwards from EOF, read the footer's keyframe
// index, resolve the offset, then decode. Whether the 32x survives paying for
// the lookup is the question the model could not answer.
//
// The two arms are deliberately asymmetric because the layouts are asymmetric:
// the whole-stream arm does the only thing that layout permits -- ReadFrame
// until it arrives at N. The per-block arm does what the layout permits -- one
// ranged read of one block. That IS the comparison.
//
// ANSWERED WHEN: both paths are timed end to end, from opening the file to
// holding frame N.
// =============================================================================
func BenchmarkShippedDecodeToFrameN(b *testing.B) {
	frames := loadCorpusProtos(b)
	dict := corpusDict(b, frames)
	dir := b.TempDir()
	target := uint32(len(frames) * 9 / 10) //nolint:gosec // corpus size is bounded

	wholePath, _ := writeShipped(b, dir, "n-whole.tape", frames)

	b.Run("whole-stream", func(b *testing.B) {
		for b.Loop() {
			r, err := codec.NewReader(wholePath)
			if err != nil {
				b.Fatalf("NewReader: %v", err)
			}
			if _, err := r.ReadHeader(); err != nil {
				b.Fatalf("ReadHeader: %v", err)
			}
			var got *capturepb.Frame
			for {
				f, err := r.ReadFrame()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					b.Fatalf("ReadFrame: %v", err)
				}
				if f.GetFrameIndex() == target {
					got = f
					break
				}
			}
			r.Close() //nolint:errcheck // read-only
			if got == nil {
				b.Fatalf("frame %d not reached", target)
			}
		}
		b.ReportMetric(float64(target), "targetFrame")
	})

	for _, kc := range keyframeIntervalCases {
		if kc.frames == 0 {
			continue
		}
		interval := uint32(kc.frames) //nolint:gosec // fixed small constants

		run := func(name string, opts ...codec.WriterOption) {
			b.Run(name, func(b *testing.B) {
				path, _ := writeShipped(b, dir, "n-"+name[:2]+kc.name+".tape", frames, opts...)
				var useDict []byte
				if len(opts) == 3 { // the dictionary variant
					useDict = dict
				}
				for b.Loop() {
					got := seekFrame(b, path, target, useDict)
					if got.GetFrameIndex() != target {
						b.Fatalf("seek landed on frame %d, want %d", got.GetFrameIndex(), target)
					}
				}
				b.ReportMetric(float64(target), "targetFrame")
			})
		}

		run("per-block/"+kc.name,
			codec.WithKeyframeInterval(interval), codec.WithPerBlockCompression())
		run("per-block+dict/"+kc.name,
			codec.WithKeyframeInterval(interval), codec.WithPerBlockCompression(),
			codec.WithDictionary(dict))
	}
}

// seekFrame reaches frame N the way a byte-range consumer would: read the seek
// table from EOF, read the footer out of the final block, resolve the keyframe
// to a block, read only that block, and scan forward inside it.
//
// Everything it costs is charged here, including re-reading the seek table and
// the footer on every iteration. A production consumer would cache both, so
// this is an upper bound rather than a flattering number.
func seekFrame(tb testing.TB, path string, target uint32, dict []byte) *capturepb.Frame {
	tb.Helper()

	index, err := codec.OpenBlockIndex(path)
	if err != nil {
		tb.Fatalf("OpenBlockIndex: %v", err)
	}
	footer, err := index.Footer(dict)
	if err != nil {
		tb.Fatalf("Footer: %v", err)
	}
	block, err := index.BlockForFrame(footer, target)
	if err != nil {
		tb.Fatalf("BlockForFrame(%d): %v", target, err)
	}
	data, err := index.ReadBlock(block, dict)
	if err != nil {
		tb.Fatalf("ReadBlock(%d): %v", block, err)
	}

	frame := frameFromBlock(tb, data, target)
	if frame == nil {
		tb.Fatalf("frame %d not in block %d", target, block)
	}
	return frame
}

// frameFromBlock decodes a block's envelopes and returns the frame with the
// requested index.
func frameFromBlock(tb testing.TB, block []byte, target uint32) *capturepb.Frame {
	tb.Helper()
	for off := 0; off < len(block); {
		length, n := readUvarint(block[off:])
		if n <= 0 {
			return nil
		}
		off += n
		if off+int(length) > len(block) {
			return nil
		}
		env := &capturepb.Envelope{}
		if err := proto.Unmarshal(block[off:off+int(length)], env); err != nil {
			tb.Fatalf("Unmarshal: %v", err)
		}
		off += int(length)
		if f := env.GetFrame(); f != nil && f.GetFrameIndex() == target {
			return f
		}
	}
	return nil
}

// readUvarint decodes a protobuf-style varint length prefix.
func readUvarint(b []byte) (uint64, int) {
	var v uint64
	var shift uint
	for i, c := range b {
		v |= uint64(c&0x7F) << shift
		if c&0x80 == 0 {
			return v, i + 1
		}
		shift += 7
		if shift >= 64 {
			return 0, -1
		}
	}
	return 0, -1
}
