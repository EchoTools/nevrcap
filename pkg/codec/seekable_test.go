package codec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// Tests for the zstd seek table (seekable.go).
//
// The table is the piece that turns KeyframeEntry.ByteOffset from a number
// nothing can use into a servable byte range, so the tests are written against
// the two things a byte-range server actually needs: the table survives a
// round trip, and a file that has no table says so instead of failing.

func TestSeekTableRoundTrip(t *testing.T) {
	entries := []seekTableEntry{
		{compressedSize: 1234, decompressedSize: 45678},
		{compressedSize: 9, decompressedSize: 10},
		{compressedSize: 0xFFFFFFFF, decompressedSize: 1},
	}

	table, err := appendSeekTable(nil, entries)
	if err != nil {
		t.Fatalf("appendSeekTable: %v", err)
	}

	wantSize := len(entries)*seekTableEntrySize + seekTableFooterSize + skippableFrameHeaderSize
	if len(table) != wantSize {
		t.Fatalf("seek table is %d bytes, want %d", len(table), wantSize)
	}

	// A table must be locatable at the end of a file that has other bytes in
	// front of it — that is its whole job.
	file := append([]byte("...pretend these are compressed blocks..."), table...)
	got, err := readSeekTable(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatalf("readSeekTable: %v", err)
	}
	if len(got) != len(entries) {
		t.Fatalf("read %d entries, wrote %d", len(got), len(entries))
	}
	for i := range entries {
		if got[i] != entries[i] {
			t.Errorf("entry %d: got %+v, want %+v", i, got[i], entries[i])
		}
	}
}

func TestSeekTableEmpty(t *testing.T) {
	table, err := appendSeekTable(nil, nil)
	if err != nil {
		t.Fatalf("appendSeekTable: %v", err)
	}
	got, err := readSeekTable(bytes.NewReader(table), int64(len(table)))
	if err != nil {
		t.Fatalf("readSeekTable on an empty table: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty table yielded %d entries", len(got))
	}
}

// TestSeekTableAbsentIsNotCorrupt is the design-chain C′ property in test form:
// a file with no index is UNFINISHED or simply not seekable, never damaged. A
// reader that reported corruption here would make the shipped whole-stream
// layout — and every live capture still being written — look broken.
func TestSeekTableAbsentIsNotCorrupt(t *testing.T) {
	cases := map[string][]byte{
		"empty file":                nil,
		"too short to hold a table": []byte{1, 2, 3},
		"plain bytes":               bytes.Repeat([]byte("no index here"), 40),
	}
	// The real case: today's shipped layout, one continuous zstd stream.
	var whole bytes.Buffer
	enc, err := zstd.NewWriter(&whole, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if _, err := enc.Write(bytes.Repeat([]byte("frame"), 500)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("encoder Close: %v", err)
	}
	cases["shipped whole-stream layout"] = whole.Bytes()

	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := readSeekTable(bytes.NewReader(data), int64(len(data)))
			if !errors.Is(err, ErrNoSeekTable) {
				t.Fatalf("got %v, want ErrNoSeekTable", err)
			}
		})
	}
}

// TestSeekTableCorruptIsReported covers the other half: bytes that DO claim to
// be a seek table but do not add up. Answering ErrNoSeekTable to these would
// hide a damaged index behind a legal-sounding state, and the caller would
// silently fall back to a full scan believing the file simply had no index.
func TestSeekTableCorruptIsReported(t *testing.T) {
	valid, err := appendSeekTable(nil, []seekTableEntry{
		{compressedSize: 100, decompressedSize: 200},
		{compressedSize: 300, decompressedSize: 400},
	})
	if err != nil {
		t.Fatalf("appendSeekTable: %v", err)
	}

	t.Run("frame count larger than the file", func(t *testing.T) {
		bad := bytes.Clone(valid)
		binary.LittleEndian.PutUint32(bad[len(bad)-9:len(bad)-5], 0xFFFF)
		if _, err := readSeekTable(bytes.NewReader(bad), int64(len(bad))); !errors.Is(err, ErrSeekTableCorrupt) {
			t.Fatalf("got %v, want ErrSeekTableCorrupt", err)
		}
	})

	t.Run("reserved descriptor bits set", func(t *testing.T) {
		bad := bytes.Clone(valid)
		bad[len(bad)-5] |= seekTableReservedMask
		if _, err := readSeekTable(bytes.NewReader(bad), int64(len(bad))); !errors.Is(err, ErrSeekTableCorrupt) {
			t.Fatalf("got %v, want ErrSeekTableCorrupt", err)
		}
	})

	t.Run("skippable magic does not match", func(t *testing.T) {
		bad := bytes.Clone(valid)
		binary.LittleEndian.PutUint32(bad[0:4], 0xDEADBEEF)
		if _, err := readSeekTable(bytes.NewReader(bad), int64(len(bad))); !errors.Is(err, ErrSeekTableCorrupt) {
			t.Fatalf("got %v, want ErrSeekTableCorrupt", err)
		}
	})

	t.Run("declared frame size disagrees with the footer", func(t *testing.T) {
		bad := bytes.Clone(valid)
		binary.LittleEndian.PutUint32(bad[4:8], 999)
		if _, err := readSeekTable(bytes.NewReader(bad), int64(len(bad))); !errors.Is(err, ErrSeekTableCorrupt) {
			t.Fatalf("got %v, want ErrSeekTableCorrupt", err)
		}
	})
}

// TestSeekTableIsSkippedByTheDecoder proves the index costs a reader that does
// not know about it exactly nothing: appending a real table to a real zstd
// stream must not change what decoding that stream yields.
func TestSeekTableIsSkippedByTheDecoder(t *testing.T) {
	payload := bytes.Repeat([]byte("envelope bytes "), 200)

	var stream bytes.Buffer
	enc, err := zstd.NewWriter(&stream, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if _, err := enc.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("encoder Close: %v", err)
	}

	withTable, err := appendSeekTable(bytes.Clone(stream.Bytes()), []seekTableEntry{
		{compressedSize: uint32(stream.Len()), decompressedSize: uint32(len(payload))},
	})
	if err != nil {
		t.Fatalf("appendSeekTable: %v", err)
	}

	dec, err := zstd.NewReader(bytes.NewReader(withTable))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer dec.Close()
	got, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("ReadAll over a stream carrying a seek table: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("appending a seek table changed what the stream decodes to")
	}
}

func TestBlockAt(t *testing.T) {
	entries := []seekTableEntry{
		{compressedSize: 100, decompressedSize: 1000},
		{compressedSize: 250, decompressedSize: 2000},
		{compressedSize: 30, decompressedSize: 300},
	}

	for _, tc := range []struct {
		offset     uint64
		wantIndex  int
		wantLength uint64
	}{
		{0, 0, 100},
		{100, 1, 250},
		{350, 2, 30},
	} {
		idx, length, err := blockAt(entries, tc.offset)
		if err != nil {
			t.Fatalf("blockAt(%d): %v", tc.offset, err)
		}
		if idx != tc.wantIndex || length != tc.wantLength {
			t.Errorf("blockAt(%d) = (%d, %d), want (%d, %d)", tc.offset, idx, length, tc.wantIndex, tc.wantLength)
		}
	}

	// An offset inside a block means the keyframe index and the seek table
	// disagree. Rounding to the enclosing block would serve a range that
	// starts at the wrong frame, so it is an error.
	if _, _, err := blockAt(entries, 50); !errors.Is(err, ErrSeekTableCorrupt) {
		t.Fatalf("mid-block offset: got %v, want ErrSeekTableCorrupt", err)
	}
	if _, _, err := blockAt(entries, 380); !errors.Is(err, ErrSeekTableCorrupt) {
		t.Fatalf("past-end offset: got %v, want ErrSeekTableCorrupt", err)
	}
}
