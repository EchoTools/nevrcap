package codec

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
)

// F9 — an envelope variant this reader does not know is skipped and counted,
// while a KNOWN variant in the wrong place stays an error.
//
// WHY THE SPLIT IS THE TEST AND NOT AN IMPLEMENTATION DETAIL. Skipping is what
// makes a new envelope kind additive rather than a break for every deployed
// reader. Erroring is what catches a capture that is damaged or concatenated.
// Collapsing them either way loses one of those: skip everything and an ordering
// defect reads as success (the F2 shape), error on everything and the format can
// never grow.
//
// unknownEnvelopeWire is a length-delimited envelope setting field 99, which is
// not in the oneof (header=1, frame=2, footer=3). Tag = 99<<3|2 = 794, varint
// 0x9A 0x06; then a 3-byte payload. Measured 2026-09-07: this parses with
// Message==nil and 6 bytes preserved in unknown fields, and re-marshals
// byte-identically.
var unknownEnvelopeWire = []byte{0x06, 0x9A, 0x06, 0x03, 'a', 'b', 'c'}

// emptyEnvelopeWire is a zero-length envelope: Message==nil like an unknown
// variant, but with NO unknown fields. It is the third case, and it must NOT be
// skipped — it carries nothing, which is malformation rather than a message from
// the future.
var emptyEnvelopeWire = []byte{0x00}

// writeEnvelopeStream writes a whole-stream zstd capture from pre-framed parts, so a
// test can place bytes the writer would never emit.
func writeEnvelopeStream(t *testing.T, name string, parts ...[]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := zstd.NewWriter(f, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range parts {
		if _, err := enc.Write(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// framed length-delimits a marshalled envelope the way the writer does.
func framed(t *testing.T, e *capturepb.Envelope) []byte {
	t.Helper()
	data, err := proto.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var buf [10]byte
	l := uint64(len(data))
	i := 0
	for l >= 0x80 {
		buf[i] = byte(l) | 0x80
		l >>= 7
		i++
	}
	buf[i] = byte(l)
	i++
	return append(append([]byte(nil), buf[:i]...), data...)
}

func envHeader(t *testing.T) []byte {
	return framed(t, &capturepb.Envelope{
		Message: &capturepb.Envelope_Header{Header: &capturepb.CaptureHeader{FormatVersion: 2}},
	})
}

func envFrame(t *testing.T, idx uint32) []byte {
	return framed(t, &capturepb.Envelope{
		Message: &capturepb.Envelope_Frame{Frame: &capturepb.Frame{
			FrameIndex: idx,
			Payload: &capturepb.Frame_EchoArena{
				EchoArena: &capturepb.EchoArenaFrame{GameClock: 1},
			},
		}},
	})
}

func envFooter(t *testing.T, frames uint32) []byte {
	return framed(t, &capturepb.Envelope{
		Message: &capturepb.Envelope_Footer{Footer: &capturepb.CaptureFooter{FrameCount: frames}},
	})
}

// TestReadFrameSkipsAnUnknownEnvelopeAndCountsIt is F9's main claim: a variant
// from the future costs the caller nothing but a counter.
func TestReadFrameSkipsAnUnknownEnvelopeAndCountsIt(t *testing.T) {
	path := writeEnvelopeStream(t, "unknown-midstream.tape",
		envHeader(t),
		envFrame(t, 0),
		unknownEnvelopeWire,
		envFrame(t, 1),
		unknownEnvelopeWire,
		envFooter(t, 2),
	)

	r, err := NewReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close() //nolint:errcheck // test cleanup
	if _, err := r.ReadHeader(); err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}

	var frames int
	for {
		_, err := r.ReadFrame()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadFrame after %d frames: %v — an unknown variant must be "+
				"skipped, not fail the capture", frames, err)
		}
		frames++
	}
	if frames != 2 {
		t.Errorf("read %d frames, want 2; the unknown envelopes must not consume frames", frames)
	}
	if got := r.SkippedEnvelopes(); got != 2 {
		t.Errorf("SkippedEnvelopes() = %d, want 2. A skip nobody can observe is "+
			"data loss with better manners (AGENTS.md §4).", got)
	}
	t.Logf("frames=%d skipped=%d", frames, r.SkippedEnvelopes())
}

// TestReadFrameRefusesAnEmptyEnvelope is the third case, and it is why
// Message==nil alone is not the test.
func TestReadFrameRefusesAnEmptyEnvelope(t *testing.T) {
	path := writeEnvelopeStream(t, "empty-envelope.tape",
		envHeader(t),
		envFrame(t, 0),
		emptyEnvelopeWire,
		envFooter(t, 1),
	)

	r, err := NewReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close() //nolint:errcheck // test cleanup
	if _, err := r.ReadHeader(); err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if _, err := r.ReadFrame(); err != nil {
		t.Fatalf("ReadFrame #1: %v", err)
	}
	_, err = r.ReadFrame()
	if !errors.Is(err, ErrUnexpectedEnvelope) {
		t.Fatalf("empty envelope: want ErrUnexpectedEnvelope, got %v. An envelope "+
			"carrying nothing is malformation, not a variant from the future.", err)
	}
	if got := r.SkippedEnvelopes(); got != 0 {
		t.Errorf("SkippedEnvelopes() = %d, want 0; an empty envelope must not be skipped", got)
	}
}

// TestSkippedEnvelopesIsZeroOnACleanCapture is the QUIET STATE. A guard with no
// observed silence is not evidence: a counter that is never zero would be
// indistinguishable from one that always fires.
func TestSkippedEnvelopesIsZeroOnACleanCapture(t *testing.T) {
	path, _ := corruptionCorpus(t, "clean.tape")

	r, err := NewReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close() //nolint:errcheck // test cleanup
	if _, err := r.ReadHeader(); err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	var frames int
	for {
		_, err := r.ReadFrame()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		frames++
	}
	if got := r.SkippedEnvelopes(); got != 0 {
		t.Errorf("SkippedEnvelopes() = %d on a capture this library wrote, want 0", got)
	}
	t.Logf("QUIET: %d frames read from a normal capture, skipped=%d", frames, r.SkippedEnvelopes())
}

// TestReadFooterSkipsAnUnknownEnvelope covers the direct-footer path: an unknown
// variant between the last frame and the footer must not cost a caller its footer.
func TestReadFooterSkipsAnUnknownEnvelope(t *testing.T) {
	path := writeEnvelopeStream(t, "unknown-before-footer.tape",
		envHeader(t),
		envFrame(t, 0),
		unknownEnvelopeWire,
		envFooter(t, 1),
	)

	r, err := NewReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close() //nolint:errcheck // test cleanup
	if _, err := r.ReadHeader(); err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if _, err := r.ReadFrame(); err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	footer, err := r.ReadFooter()
	if err != nil {
		t.Fatalf("ReadFooter: %v — an unknown variant before the footer must be skipped", err)
	}
	if got := footer.GetFrameCount(); got != 1 {
		t.Errorf("footer frame_count = %d, want 1", got)
	}
	if got := r.SkippedEnvelopes(); got != 1 {
		t.Errorf("SkippedEnvelopes() = %d, want 1", got)
	}
}

// TestBlockIndexFooterSkipsAnUnknownEnvelope covers the SEEKING path. It matters
// more than the sequential one for F9's purpose: if a future writer puts a new
// envelope kind in the footer's block, a reader that refuses it cannot seek the
// capture at all — it loses the keyframe index, not just one envelope.
//
// The file is built by hand because no writer in this package emits an unknown
// variant: the footer block is decompressed, the unknown envelope is prepended
// to it, the block is recompressed and the seek table is rebuilt around the new
// sizes. That keeps ReadBlock's own checks honest — the table still declares
// exactly what the block decodes to.
func TestBlockIndexFooterSkipsAnUnknownEnvelope(t *testing.T) {
	path, _ := corruptionCorpus(t, "perblock.tape")
	clean, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	index, err := OpenBlockIndex(path)
	if err != nil {
		t.Fatalf("OpenBlockIndex: %v (this test needs a per-block capture)", err)
	}
	last := index.Blocks() - 1
	off, length, err := index.BlockRange(last)
	if err != nil {
		t.Fatal(err)
	}

	dec, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	plain, err := dec.DecodeAll(clean[off:off+length], nil)
	if err != nil {
		t.Fatalf("decoding the footer block: %v", err)
	}

	// The unknown envelope goes FIRST, so Footer meets it before the footer and
	// has to skip past it rather than merely tolerate a trailing byte.
	payload := append(append([]byte(nil), unknownEnvelopeWire...), plain...)

	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		t.Fatal(err)
	}
	block := enc.EncodeAll(payload, nil)
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}

	entries := append([]seekTableEntry(nil), index.entries...)
	entries[last] = seekTableEntry{
		compressedSize:   uint32(len(block)),   //nolint:gosec // test-sized
		decompressedSize: uint32(len(payload)), //nolint:gosec // test-sized
	}
	rebuilt := append([]byte(nil), clean[:off]...)
	rebuilt = append(rebuilt, block...)
	rebuilt, err = appendSeekTable(rebuilt, entries)
	if err != nil {
		t.Fatalf("appendSeekTable: %v", err)
	}

	target := filepath.Join(t.TempDir(), "unknown-in-footer-block.tape")
	if err := os.WriteFile(target, rebuilt, 0o600); err != nil {
		t.Fatal(err)
	}

	bad, err := OpenBlockIndex(target)
	if err != nil {
		t.Fatalf("OpenBlockIndex on the rebuilt file: %v", err)
	}
	footer, err := bad.Footer(nil)
	if err != nil {
		t.Fatalf("Footer: %v — an unknown envelope in the footer's block must be "+
			"skipped, or a future writer makes the capture unseekable", err)
	}
	if footer.GetFrameCount() == 0 {
		t.Errorf("footer frame_count = 0; the footer was not actually recovered")
	}
	if got := bad.SkippedEnvelopes(); got != 1 {
		t.Errorf("SkippedEnvelopes() = %d, want 1", got)
	}
	t.Logf("seeking path: footer frame_count=%d recovered past %d skipped envelope(s)",
		footer.GetFrameCount(), bad.SkippedEnvelopes())
}
