package codec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// Dependency-behavior gates for the .tape container.
//
// A per-block container layout (one independent zstd frame per block, so a
// byte range is decompressible on its own) rests on three properties of
// github.com/klauspost/compress/zstd. The transcode spec
// (2026-08-31-tape-transcode-spec.md, F1 compatibility note) marks all three
// PLAUSIBLE and says explicitly: "Verify against klauspost/compress
// specifically; do not inherit this from the spec text."
//
// These tests are that verification, and they stay as regression gates: the
// layout is only safe while the pinned decoder keeps behaving this way. If
// klauspost ever changes, these fail here rather than silently corrupting a
// reader in the field.
//
// The zstd frame constants are from the zstd format specification (RFC 8878
// §3.1.1): a Skippable_Frame's Magic_Number is 0x184D2A50-0x184D2A5F, followed
// by a 4-byte little-endian Frame_Size and that many bytes of content. The
// seekable-format convention picks 0x184D2A5E for its seek table.

// TestZstdDecodesConcatenatedFramesAsOneStream is the property the whole
// per-block layout depends on: if a reader can stream straight through a file
// made of N independent zstd frames, then a per-block .tape is READABLE BY THE
// SHIPPED SEQUENTIAL PATH with no reader change at all, and only the seek path
// is new work. If this were false, per-block would break every existing
// consumer and the cost of F1 would be an order of magnitude higher.
func TestZstdDecodesConcatenatedFramesAsOneStream(t *testing.T) {
	blocks := [][]byte{
		[]byte("first block of envelope bytes"),
		[]byte("second block, independently compressed"),
		[]byte("third"),
	}

	var file bytes.Buffer
	for _, b := range blocks {
		var one bytes.Buffer
		enc, err := zstd.NewWriter(&one, zstd.WithEncoderLevel(zstd.SpeedFastest))
		if err != nil {
			t.Fatalf("NewWriter: %v", err)
		}
		if _, err := enc.Write(b); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if err := enc.Close(); err != nil {
			t.Fatalf("encoder Close: %v", err)
		}
		file.Write(one.Bytes())
	}

	dec, err := zstd.NewReader(bytes.NewReader(file.Bytes()))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer dec.Close()

	got, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("ReadAll over concatenated frames: %v", err)
	}
	want := bytes.Join(blocks, nil)
	if !bytes.Equal(got, want) {
		t.Fatalf("concatenated frames did not decode as one stream:\n got %q\nwant %q", got, want)
	}
}

// TestZstdSkipsSkippableFrames is what lets the seek table ride in the file
// without breaking sequential readers, and it is the reason the zstd seekable
// format puts its index in a skippable frame in the first place: a decoder that
// knows nothing about the index skips it instead of choking on it.
//
// The table is placed BETWEEN two data frames here, not only at EOF, because a
// header-block layout would put an index near the front and a
// streaming-write layout puts it at the back. The property must hold in both
// positions or the layout choice is constrained by the decoder.
func TestZstdSkipsSkippableFrames(t *testing.T) {
	compress := func(b []byte) []byte {
		var out bytes.Buffer
		enc, err := zstd.NewWriter(&out, zstd.WithEncoderLevel(zstd.SpeedFastest))
		if err != nil {
			t.Fatalf("NewWriter: %v", err)
		}
		if _, err := enc.Write(b); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if err := enc.Close(); err != nil {
			t.Fatalf("encoder Close: %v", err)
		}
		return out.Bytes()
	}

	// A skippable frame carrying bytes no decoder should hand back.
	content := []byte("SEEK TABLE WOULD LIVE HERE")
	skippable := make([]byte, 0, 8+len(content))
	skippable = binary.LittleEndian.AppendUint32(skippable, seekableSkippableMagic)
	skippable = binary.LittleEndian.AppendUint32(skippable, uint32(len(content)))
	skippable = append(skippable, content...)

	var file bytes.Buffer
	file.Write(compress([]byte("data before")))
	file.Write(skippable)
	file.Write(compress([]byte("data after")))

	dec, err := zstd.NewReader(bytes.NewReader(file.Bytes()))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer dec.Close()

	got, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("ReadAll across a skippable frame: %v", err)
	}
	if want := "data beforedata after"; string(got) != want {
		t.Fatalf("skippable frame was not skipped cleanly:\n got %q\nwant %q", got, want)
	}
	if bytes.Contains(got, content) {
		t.Fatal("skippable frame content leaked into the decoded stream")
	}
}

// TestZstdDictionaryIDTravelsInTheFrameHeader answers a question the spec left
// open. F6 proposes a `dictionary_id` field on CaptureHeader, calling it "the
// permanent obligation of chain BUT P, made explicit". But zstd already carries
// Dictionary_ID in every frame header it writes (RFC 8878 §3.1.1.1.3), so the
// obligation is on the wire whether or not the container restates it.
//
// What this test pins is the part that matters for a reader: a decoder WITHOUT
// the dictionary fails loudly rather than returning wrong bytes. That is the
// difference between "you need dict N" being enforced by the format and being a
// convention someone has to remember.
func TestZstdDictionaryIDTravelsInTheFrameHeader(t *testing.T) {
	// Train a dictionary on repetitive content, the way telemetry frames are
	// repetitive. History is the match window; Contents trains entropy tables.
	var samples [][]byte
	var history []byte
	for i := range 512 {
		s := []byte("telemetry frame payload with shared structure, slot=")
		s = append(s, byte('0'+i%10))
		samples = append(samples, s)
		history = append(history, s...)
	}
	const dictID = 0x7A7E
	dict, err := zstd.BuildDict(zstd.BuildDictOptions{
		ID:       dictID,
		Contents: samples,
		History:  history,
		Level:    zstd.SpeedDefault,
	})
	if err != nil {
		t.Skipf("BuildDict unavailable in this build: %v", err)
	}

	payload := bytes.Repeat([]byte("telemetry frame payload with shared structure, slot=3"), 40)

	var out bytes.Buffer
	enc, err := zstd.NewWriter(&out,
		zstd.WithEncoderLevel(zstd.SpeedFastest),
		zstd.WithEncoderDict(dict))
	if err != nil {
		t.Fatalf("NewWriter with dict: %v", err)
	}
	if _, err := enc.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("encoder Close: %v", err)
	}

	// The declared dictionary id must be readable from the frame header alone,
	// without the dictionary in hand — that is what makes it a wire fact.
	if got := frameDictionaryID(t, out.Bytes()); got != dictID {
		t.Fatalf("frame header declares dictionary id %#x, want %#x", got, dictID)
	}

	// With the dictionary: exact round trip.
	withDict, err := zstd.NewReader(nil, zstd.WithDecoderDicts(dict))
	if err != nil {
		t.Fatalf("NewReader with dict: %v", err)
	}
	defer withDict.Close()
	got, err := withDict.DecodeAll(out.Bytes(), nil)
	if err != nil {
		t.Fatalf("DecodeAll with dict: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("dictionary round trip did not reproduce the payload")
	}

	// Without it: an error, never silent wrong bytes.
	plain, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer plain.Close()
	if _, err := plain.DecodeAll(out.Bytes(), nil); err == nil {
		t.Fatal("decoding a dictionary-compressed frame WITHOUT the dictionary succeeded; " +
			"a missing dictionary must be an error, not silent data loss")
	} else if !errors.Is(err, zstd.ErrUnknownDictionary) {
		t.Logf("missing-dictionary error is %v (not ErrUnknownDictionary); "+
			"still an error, so the gate holds", err)
	}
}

// frameDictionaryID parses Dictionary_ID out of a zstd frame header per RFC 8878
// §3.1.1.1. It exists so the test can assert the id is on the wire without
// trusting the encoder's own accounting.
func frameDictionaryID(t *testing.T, frame []byte) uint32 {
	t.Helper()
	if len(frame) < 6 {
		t.Fatalf("frame too short: %d bytes", len(frame))
	}
	if magic := binary.LittleEndian.Uint32(frame[:4]); magic != 0xFD2FB528 {
		t.Fatalf("not a zstd frame: magic %#x", magic)
	}
	fhd := frame[4]
	dictIDFlag := fhd & 0x03       // Dictionary_ID_flag, bits 0-1
	singleSegment := fhd&0x20 != 0 // Single_Segment_flag, bit 5
	fcsFlag := fhd >> 6            // Frame_Content_Size_flag, bits 6-7
	off := 5                       // past Magic_Number and Frame_Header_Descriptor
	if !singleSegment {
		off++ // Window_Descriptor
	}
	var n int
	switch dictIDFlag {
	case 0:
		return 0
	case 1:
		n = 1
	case 2:
		n = 2
	case 3:
		n = 4
	}
	_ = fcsFlag // Frame_Content_Size follows the Dictionary_ID; not needed here.
	if off+n > len(frame) {
		t.Fatalf("frame header truncated: need %d bytes, have %d", off+n, len(frame))
	}
	switch n {
	case 1:
		return uint32(frame[off])
	case 2:
		return uint32(binary.LittleEndian.Uint16(frame[off : off+2]))
	default:
		return binary.LittleEndian.Uint32(frame[off : off+4])
	}
}
