package codec

import (
	"bytes"
	"errors"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"google.golang.org/protobuf/proto"
)

// The per-block layout's envelope stream, checked against the whole-stream
// layout's.
//
// WHY THIS EXISTS. TestBackwardCompatibility compares HEAD's reproduction of
// the pre-v4.1.0 layout against a baseline's output, which leaves the per-block
// layout — the newer half of the format, and since v4.1.0 the DEFAULT half —
// checked there only for readability and for the one licensed footer
// difference. An audit padded the varint prefix in the per-block branch of
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
// WHICH ARM IS WHICH, SINCE v4.1.0. Per-block is now the default and
// whole-stream takes WithWholeStreamCompression; the comparison is unchanged,
// but the arm that needs an option swapped sides. Naming the option on the
// whole-stream arm is what keeps this test comparing two DIFFERENT layouts
// rather than a file with itself.
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

// TestPerBlockEnvelopeStreamMatchesWholeStream holds the per-block layout to
// the whole-stream layout's envelope stream, envelope by envelope.
func TestPerBlockEnvelopeStreamMatchesWholeStream(t *testing.T) {
	dir := t.TempDir()
	wholePath := dir + "/wholestream.tape"
	perBlockPath := dir + "/perblock.tape"
	writeCompatCapture(t, wholePath, WithWholeStreamCompression())
	writeCompatCapture(t, perBlockPath, WithPerBlockCompression())

	// Guard the guard, both directions: if per-block silently produced the
	// whole-stream layout, or the opt-out silently produced per-block, this
	// whole test would compare a file with itself and pass on nothing.
	index, err := OpenBlockIndex(perBlockPath)
	if err != nil {
		t.Fatalf("the per-block capture has no seek table (%v), so this test would be "+
			"comparing the whole-stream layout against itself", err)
	}
	if _, wholeErr := OpenBlockIndex(wholePath); !errors.Is(wholeErr, ErrNoSeekTable) {
		t.Fatalf("the WithWholeStreamCompression capture carries a seek table (%v), so this "+
			"test would be comparing the per-block layout against itself", wholeErr)
	}
	if index.Blocks() < 3 {
		t.Fatalf("per-block capture has %d blocks; it is not actually blocked", index.Blocks())
	}

	wholeEnvs := envelopeStream(t, decompressAll(t, wholePath))
	perBlockEnvs := envelopeStream(t, decompressAll(t, perBlockPath))

	if len(wholeEnvs) != len(perBlockEnvs) {
		t.Fatalf("the two layouts carry different envelope counts: whole-stream %d, per-block %d",
			len(wholeEnvs), len(perBlockEnvs))
	}
	if len(wholeEnvs) < 3 {
		t.Fatalf("expected a header, frames and a footer; got %d envelopes", len(wholeEnvs))
	}

	// Header and every frame must be byte-identical. This is what the audit's
	// mutation broke and what nothing was checking.
	last := len(wholeEnvs) - 1
	identical := 0
	for i := range last {
		if !bytes.Equal(wholeEnvs[i], perBlockEnvs[i]) {
			t.Errorf("envelope %d of %d DIFFERS between the whole-stream and per-block layouts "+
				"(%d vs %d bytes). Per-block changes the container, not the content: only the "+
				"footer's keyframe byte offsets are licensed to differ.",
				i, len(wholeEnvs), len(wholeEnvs[i]), len(perBlockEnvs[i]))
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
	wholeFooter := footerFrom(t, wholeEnvs[last])
	perBlockFooter := footerFrom(t, perBlockEnvs[last])
	// The footer's own framing is not licensed to differ either; only the
	// message content is, and only in one field.
	if dn, pn := prefixLen(wholeEnvs[last]), prefixLen(perBlockEnvs[last]); dn != pn {
		t.Errorf("the footer's length prefix is %d bytes in the whole-stream layout and %d in "+
			"per-block; the framing is not licensed to differ between layouts", dn, pn)
	}
	if proto.Equal(wholeFooter, perBlockFooter) {
		t.Error("the per-block footer is identical to the whole-stream footer, so " +
			"KeyframeEntry.ByteOffset was NOT redefined to a compressed-file offset — " +
			"the recorded offsets are still unseekable positions in the decompressed stream")
	}
	if !proto.Equal(withoutKeyframeOffsets(wholeFooter), withoutKeyframeOffsets(perBlockFooter)) {
		t.Errorf("the footers differ in something other than KeyframeEntry.ByteOffset, which is "+
			"the only licensed difference between the layouts.\n  whole-stream: %v\n  per-block:    %v",
			withoutKeyframeOffsets(wholeFooter), withoutKeyframeOffsets(perBlockFooter))
	}

	t.Logf("checked %d envelopes: %d byte-identical across layouts, footer differs only in "+
		"KeyframeEntry.ByteOffset (%d blocks in the seek table)",
		len(wholeEnvs), identical, index.Blocks())
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
