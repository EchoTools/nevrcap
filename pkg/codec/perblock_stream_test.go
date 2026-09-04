package codec

import (
	"bytes"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"google.golang.org/protobuf/proto"
)

// The per-block layout's envelope stream, checked against the default layout's.
//
// WHY THIS EXISTS. TestBackwardCompatibility compares HEAD's DEFAULT output
// against a baseline's, which leaves the per-block layout — the newer half of
// the format, and the half most likely to change — checked only for
// readability. An audit padded the varint prefix in the per-block branch of
// writeEnvelope alone, genuinely changing what a per-block tape looks like on
// disk, and the entire suite went green: property 4 asks only "does an old
// reader parse it", and a redundant continuation byte still parses.
//
// A cross-VERSION comparison is not available for this layout: every baseline
// predates per-block, so there is no old per-block file to compare against and
// no old writer that could produce one. What IS available is a cross-LAYOUT
// invariant, and it is stronger than a golden would be because it encodes the
// design rather than a snapshot of one run — no frozen artifact, no
// regeneration ritual, nothing to re-bless.
//
// THE INVARIANT, AND ITS ONE MEASURED EXCEPTION. Per-block changes the
// CONTAINER, not the content: the same envelopes are compressed in independent
// blocks instead of one stream. So the decompressed envelope stream must be
// identical across the two layouts — except for the footer, because
// KeyframeEntry.ByteOffset is deliberately redefined by the per-block layout
// from an offset into the decompressed stream to an offset into the compressed
// file. That is the whole point of the layout, and it is the only licensed
// difference.
//
// Measured on the fixed corpus: 402 envelopes, of which 401 are byte-identical
// and the sole difference is the final one, the footer. The exception below is
// therefore pinned to exactly that field: any OTHER footer change — a frame
// count, a duration, an event index, a keyframe's frame_index — fails.

// TestPerBlockEnvelopeStreamMatchesDefault holds the per-block layout to the
// default layout's envelope stream, envelope by envelope.
func TestPerBlockEnvelopeStreamMatchesDefault(t *testing.T) {
	dir := t.TempDir()
	defaultPath := dir + "/default.tape"
	perBlockPath := dir + "/perblock.tape"
	writeCompatCapture(t, defaultPath)
	writeCompatCapture(t, perBlockPath, WithPerBlockCompression())

	// Guard the guard: if per-block silently produced the default layout, this
	// whole test would compare a file with itself and pass on nothing.
	index, err := OpenBlockIndex(perBlockPath)
	if err != nil {
		t.Fatalf("the per-block capture has no seek table (%v), so this test would be "+
			"comparing the default layout against itself", err)
	}
	if index.Blocks() < 3 {
		t.Fatalf("per-block capture has %d blocks; it is not actually blocked", index.Blocks())
	}

	defaultEnvs := envelopeStream(t, decompressAll(t, defaultPath))
	perBlockEnvs := envelopeStream(t, decompressAll(t, perBlockPath))

	if len(defaultEnvs) != len(perBlockEnvs) {
		t.Fatalf("the two layouts carry different envelope counts: default %d, per-block %d",
			len(defaultEnvs), len(perBlockEnvs))
	}
	if len(defaultEnvs) < 3 {
		t.Fatalf("expected a header, frames and a footer; got %d envelopes", len(defaultEnvs))
	}

	// Header and every frame must be byte-identical. This is what the audit's
	// mutation broke and what nothing was checking.
	last := len(defaultEnvs) - 1
	identical := 0
	for i := range last {
		if !bytes.Equal(defaultEnvs[i], perBlockEnvs[i]) {
			t.Errorf("envelope %d of %d DIFFERS between the default and per-block layouts "+
				"(%d vs %d bytes). Per-block changes the container, not the content: only the "+
				"footer's keyframe byte offsets are licensed to differ.",
				i, len(defaultEnvs), len(defaultEnvs[i]), len(perBlockEnvs[i]))
			if testing.Short() || i > 3 {
				break
			}
		} else {
			identical++
		}
	}

	// The footer is the one licensed difference, and it is licensed in exactly
	// one field. Zeroing KeyframeEntry.ByteOffset on both sides must make them
	// equal; if anything else moved, this fails.
	defaultFooter := footerFrom(t, defaultEnvs[last])
	perBlockFooter := footerFrom(t, perBlockEnvs[last])
	// The footer's own framing is not licensed to differ either; only the
	// message content is, and only in one field.
	if dn, pn := prefixLen(defaultEnvs[last]), prefixLen(perBlockEnvs[last]); dn != pn {
		t.Errorf("the footer's length prefix is %d bytes in the default layout and %d in "+
			"per-block; the framing is not licensed to differ between layouts", dn, pn)
	}
	if proto.Equal(defaultFooter, perBlockFooter) {
		t.Error("the per-block footer is identical to the default footer, so " +
			"KeyframeEntry.ByteOffset was NOT redefined to a compressed-file offset — " +
			"the recorded offsets are still unseekable positions in the decompressed stream")
	}
	if !proto.Equal(withoutKeyframeOffsets(defaultFooter), withoutKeyframeOffsets(perBlockFooter)) {
		t.Errorf("the footers differ in something other than KeyframeEntry.ByteOffset, which is "+
			"the only licensed difference between the layouts.\n  default:   %v\n  per-block: %v",
			withoutKeyframeOffsets(defaultFooter), withoutKeyframeOffsets(perBlockFooter))
	}

	t.Logf("checked %d envelopes: %d byte-identical across layouts, footer differs only in "+
		"KeyframeEntry.ByteOffset (%d blocks in the seek table)",
		len(defaultEnvs), identical, index.Blocks())
}

// envelopeStream splits a decompressed capture into its RAW framed envelopes —
// each one the varint length prefix followed by the message bytes, exactly as
// they sit in the stream.
//
// It parses the varint here rather than going through Reader.readEnvelope, and
// that is a deliberate reversal of the usual "reuse the real framing" rule. An
// earlier version of this helper returned RE-MARSHALLED envelopes, which made it
// blind to the framing itself: an audit mutation that padded the varint prefix
// left all 401 message payloads byte-identical, and was caught only incidentally
// because the padding also moved footer_offset. Comparing what the reader hands
// back cannot detect a change in how the reader was told to read.
//
// Each payload is still unmarshalled as an Envelope, so a slice that is not a
// valid message fails here rather than downstream.
func envelopeStream(t *testing.T, stream []byte) [][]byte {
	t.Helper()
	var out [][]byte
	for off := 0; off < len(stream); {
		length, n := consumeUvarint(stream[off:])
		if n <= 0 {
			t.Fatalf("envelope %d: malformed length prefix at byte %d", len(out), off)
		}
		end := off + n + int(length)
		if end > len(stream) {
			t.Fatalf("envelope %d: length %d at byte %d overruns the %d-byte stream",
				len(out), length, off, len(stream))
		}
		if err := proto.Unmarshal(stream[off+n:end], &capturepb.Envelope{}); err != nil {
			t.Fatalf("envelope %d: payload is not a valid Envelope: %v", len(out), err)
		}
		out = append(out, stream[off:end])
		off = end
	}
	return out
}

// consumeUvarint decodes a protobuf-style varint, returning the value and the
// number of bytes it occupied. A non-minimal encoding is decoded faithfully —
// it must be, or the padding this test exists to catch would be normalised away
// before the comparison ever saw it.
func consumeUvarint(b []byte) (uint64, int) {
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

// prefixLen returns how many bytes the varint length prefix occupies.
func prefixLen(framed []byte) int {
	_, n := consumeUvarint(framed)
	return n
}

// footerFrom unmarshals a RAW FRAMED envelope that must carry the capture
// footer, skipping its length prefix.
func footerFrom(t *testing.T, framed []byte) *capturepb.CaptureFooter {
	t.Helper()
	_, n := consumeUvarint(framed)
	if n <= 0 {
		t.Fatal("malformed length prefix on the final envelope")
	}
	env := &capturepb.Envelope{}
	if err := proto.Unmarshal(framed[n:], env); err != nil {
		t.Fatalf("unmarshal final envelope: %v", err)
	}
	footer := env.GetFooter()
	if footer == nil {
		t.Fatal("the final envelope is not a footer")
	}
	return footer
}

// withoutKeyframeOffsets returns a copy with every KeyframeEntry.ByteOffset
// cleared, so two footers can be compared on everything the layout does NOT
// license to differ.
func withoutKeyframeOffsets(footer *capturepb.CaptureFooter) *capturepb.CaptureFooter {
	clone, ok := proto.Clone(footer).(*capturepb.CaptureFooter)
	if !ok {
		panic("proto.Clone returned a different type")
	}
	for _, kf := range clone.GetKeyframeIndex() {
		kf.SetByteOffset(0)
	}
	return clone
}
