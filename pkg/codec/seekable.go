package codec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/klauspost/compress/zstd"
)

// The zstd seekable format — the index that makes a per-block .tape servable.
//
// A .tape written as one continuous zstd stream records keyframe byte offsets
// that nothing can seek to: the offsets are positions in the DECOMPRESSED
// stream, and reaching any of them means decompressing from the start. The
// container doc comment on Writer admits this in as many words. Splitting the
// container into independent zstd frames fixes the seeking, but only if
// something records where each frame begins in the COMPRESSED file.
//
// That index is not invented here. The zstd seekable format is the existing
// prior art for exactly this problem, and it is used verbatim:
//
//	facebook/zstd, contrib/seekable_format/zstd_seekable_compression_format.md
//
// Its shape, from that specification:
//
//	Seek_Table_Frame
//	  Skippable_Magic_Number   4 B LE  0x184D2A5E
//	  Frame_Size               4 B LE  byte length of everything after this field
//	  Seek_Table_Entries       N x (8 B, or 12 B with checksums)
//	  Seek_Table_Footer        9 B
//
//	Seek_Table_Entry
//	  Compressed_Size          4 B LE
//	  Decompressed_Size        4 B LE
//	  Checksum                 4 B LE  (present only if the descriptor says so)
//
//	Seek_Table_Footer
//	  Number_Of_Frames         4 B LE
//	  Seek_Table_Descriptor    1 B     bit 7 = checksums present; bits 6-2 reserved, must be 0
//	  Seekable_Magic_Number    4 B LE  0x8F92EAB1
//
// Two properties are why this shape and not a bespoke trailer, and both are
// verified against the pinned decoder in zstd_container_behavior_test.go rather
// than taken on faith:
//
//   - The table rides in a zstd SKIPPABLE frame, so a decoder that knows
//     nothing about seeking skips it instead of failing on it. The index costs
//     existing readers nothing.
//   - The footer is fixed-size and sits at EOF, so the table is locatable
//     backwards from the end of the file without decompressing anything.
//
// The table records block sizes; it does not record which capture frame starts
// which block. That mapping is already the job of CaptureFooter.keyframe_index,
// and the two are read together: the keyframe index answers "which block holds
// frame N", the seek table answers "where does that block begin and how big is
// it".

const (
	// seekableSkippableMagic marks the skippable frame that carries the seek
	// table. It is one of the 16 legal Skippable_Magic_Numbers
	// (0x184D2A50-0x184D2A5F); the seekable format reserves 0x184D2A5E.
	seekableSkippableMagic uint32 = 0x184D2A5E

	// seekableFooterMagic terminates the seek table footer, so a reader
	// scanning backwards from EOF can tell a seek table from arbitrary
	// trailing bytes.
	seekableFooterMagic uint32 = 0x8F92EAB1

	// seekTableFooterSize is Number_Of_Frames (4) + Seek_Table_Descriptor (1)
	// + Seekable_Magic_Number (4).
	seekTableFooterSize = 9

	// seekTableEntrySize is Compressed_Size (4) + Decompressed_Size (4). Tape
	// writes no per-entry checksums: zstd frames carry their own optional
	// content checksum, and a second one here would cost 50% more index for a
	// weaker guarantee.
	seekTableEntrySize = 8

	// skippableFrameHeaderSize is Skippable_Magic_Number (4) + Frame_Size (4).
	skippableFrameHeaderSize = 8

	// seekTableChecksumFlag is bit 7 of Seek_Table_Descriptor.
	seekTableChecksumFlag byte = 0x80

	// seekTableReservedMask covers descriptor bits 6-2, which the format
	// requires to be zero. A reader that ignored them would misparse a future
	// revision instead of refusing it.
	seekTableReservedMask byte = 0x7C
)

// Errors returned when a seek table is absent or unusable. They are distinct
// because the two states mean different things to a caller: ErrNoSeekTable
// means "this file was not written for seeking" (a legal state — the shipped
// whole-stream layout has no table), while the others mean "there is a table
// here and it is damaged".
var (
	// ErrNoSeekTable reports that the file does not end in a seek table. This
	// is not corruption: a whole-stream .tape is a valid capture with no index.
	ErrNoSeekTable = errors.New("tape: no zstd seek table at end of file")

	// ErrSeekTableCorrupt reports a seek table whose own fields disagree —
	// a declared frame count that does not fit the skippable frame it lives
	// in, or reserved descriptor bits this reader does not understand.
	ErrSeekTableCorrupt = errors.New("tape: seek table is malformed")
)

// seekTableEntry describes one independently decompressible block: how many
// bytes it occupies in the file, and how many bytes it yields when decoded.
type seekTableEntry struct {
	compressedSize   uint32
	decompressedSize uint32
}

// appendSeekTable appends a complete seek table skippable frame to dst and
// returns the extended slice. It is append-style rather than io.Writer-style
// because the table is built once, at Close, from sizes already in memory —
// there is no streaming case, and a []byte keeps the caller's error handling
// at one place instead of nine.
//
// An entry whose sizes exceed uint32 cannot be represented by the format at
// all, so this reports it rather than truncating: a silently wrapped size
// would produce a table that seeks to the wrong offset, which is worse than
// no table.
func appendSeekTable(dst []byte, entries []seekTableEntry) ([]byte, error) {
	if len(entries) > math.MaxUint32 {
		return nil, fmt.Errorf("tape: seek table: %d blocks exceeds the format's uint32 frame count: %w",
			len(entries), ErrSeekTableCorrupt)
	}

	contentSize := len(entries)*seekTableEntrySize + seekTableFooterSize
	if contentSize > math.MaxUint32 {
		return nil, fmt.Errorf("tape: seek table: %d bytes exceeds a skippable frame's uint32 size: %w",
			contentSize, ErrSeekTableCorrupt)
	}

	dst = binary.LittleEndian.AppendUint32(dst, seekableSkippableMagic)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(contentSize)) //nolint:gosec // bounded above
	for _, e := range entries {
		dst = binary.LittleEndian.AppendUint32(dst, e.compressedSize)
		dst = binary.LittleEndian.AppendUint32(dst, e.decompressedSize)
	}
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(entries))) //nolint:gosec // bounded above
	dst = append(dst, 0)                                              // descriptor: no checksums, no reserved bits
	dst = binary.LittleEndian.AppendUint32(dst, seekableFooterMagic)
	return dst, nil
}

// readSeekTable locates and parses the seek table at the end of a file of the
// given size, reading backwards from EOF so nothing is decompressed to find it.
//
// It returns ErrNoSeekTable when the file simply does not carry one — the
// shipped whole-stream layout, a truncated live capture, or any other zstd
// file — so a caller can fall back to sequential reading rather than treating
// the absence as damage. This mirrors the design chain's C′: an absent index
// means unfinished, not corrupt.
func readSeekTable(r io.ReaderAt, size int64) ([]seekTableEntry, error) {
	if size < seekTableFooterSize+skippableFrameHeaderSize {
		return nil, ErrNoSeekTable
	}

	var footer [seekTableFooterSize]byte
	if _, err := r.ReadAt(footer[:], size-seekTableFooterSize); err != nil {
		return nil, fmt.Errorf("tape: seek table: reading footer: %w", err)
	}
	if binary.LittleEndian.Uint32(footer[5:9]) != seekableFooterMagic {
		return nil, ErrNoSeekTable
	}

	numFrames := binary.LittleEndian.Uint32(footer[0:4])
	descriptor := footer[4]
	if descriptor&seekTableReservedMask != 0 {
		return nil, fmt.Errorf("tape: seek table descriptor %#02x sets reserved bits: %w",
			descriptor, ErrSeekTableCorrupt)
	}
	entrySize := int64(seekTableEntrySize)
	if descriptor&seekTableChecksumFlag != 0 {
		entrySize += 4 // Checksum
	}

	entriesSize := int64(numFrames) * entrySize
	frameSize := entriesSize + seekTableFooterSize
	tableStart := size - frameSize - skippableFrameHeaderSize
	if tableStart < 0 {
		return nil, fmt.Errorf("tape: seek table declares %d blocks (%d bytes) but the file is %d bytes: %w",
			numFrames, frameSize, size, ErrSeekTableCorrupt)
	}

	var header [skippableFrameHeaderSize]byte
	if _, err := r.ReadAt(header[:], tableStart); err != nil {
		return nil, fmt.Errorf("tape: seek table: reading skippable frame header: %w", err)
	}
	if magic := binary.LittleEndian.Uint32(header[0:4]); magic != seekableSkippableMagic {
		// The trailing bytes looked like a footer but the frame they claim to
		// end does not exist. Treating this as "no table" would hide a
		// damaged index behind a legal-sounding answer.
		return nil, fmt.Errorf("tape: seek table footer at EOF but skippable magic is %#x: %w",
			magic, ErrSeekTableCorrupt)
	}
	if declared := int64(binary.LittleEndian.Uint32(header[4:8])); declared != frameSize {
		return nil, fmt.Errorf("tape: skippable frame declares %d content bytes, footer implies %d: %w",
			declared, frameSize, ErrSeekTableCorrupt)
	}

	if numFrames == 0 {
		return nil, nil
	}

	buf := make([]byte, entriesSize)
	if _, err := r.ReadAt(buf, tableStart+skippableFrameHeaderSize); err != nil {
		return nil, fmt.Errorf("tape: seek table: reading %d entries: %w", numFrames, err)
	}

	entries := make([]seekTableEntry, numFrames)
	for i := range entries {
		off := int64(i) * entrySize
		entries[i] = seekTableEntry{
			compressedSize:   binary.LittleEndian.Uint32(buf[off : off+4]),
			decompressedSize: binary.LittleEndian.Uint32(buf[off+4 : off+8]),
		}
	}
	return entries, nil
}

// blockAt returns the file offset and compressed length of the block holding
// the given compressed-file offset, plus its index. It exists so a caller that
// has a KeyframeEntry.ByteOffset can turn it into a servable byte range
// without re-deriving the running sum every time.
//
// The offset must fall on a block boundary: a keyframe offset that lands in the
// middle of a block means the index and the table disagree, which is a defect
// worth reporting rather than rounding away.
func blockAt(entries []seekTableEntry, offset uint64) (index int, length uint64, err error) {
	var running uint64
	for i, e := range entries {
		if running == offset {
			return i, uint64(e.compressedSize), nil
		}
		running += uint64(e.compressedSize)
	}
	return 0, 0, fmt.Errorf("tape: offset %d is not a block boundary in a %d-block seek table: %w",
		offset, len(entries), ErrSeekTableCorrupt)
}

// BlockIndex is the seek table of a per-block capture, read from the end of the
// file without decompressing anything.
//
// It exists so a byte-range server can answer "give me the bytes for frame N"
// with a file offset and a length. That is the whole point of the per-block
// layout: a keyframe's KeyframeEntry.ByteOffset becomes a position in the
// compressed file, and this turns that position into a range.
type BlockIndex struct {
	path    string
	entries []seekTableEntry
}

// OpenBlockIndex reads the seek table of the capture at filename.
//
// It returns ErrNoSeekTable for a capture written in the shipped whole-stream
// layout, or for one still being written. That is a legal answer, not a
// failure: such a capture is read sequentially instead. A table that is
// present but internally inconsistent returns ErrSeekTableCorrupt, because
// answering "no index" there would hide damage behind a legal-sounding state.
func OpenBlockIndex(filename string) (*BlockIndex, error) {
	file, err := os.Open(filename) //nolint:gosec // filename is caller-provided path
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck // read-only

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("tape: stat %s: %w", filename, err)
	}

	entries, err := readSeekTable(file, info.Size())
	if err != nil {
		return nil, err
	}
	return &BlockIndex{entries: entries, path: filename}, nil
}

// Blocks reports how many independently decompressible blocks the capture
// holds, including the header block and the footer block.
func (i *BlockIndex) Blocks() int { return len(i.entries) }

// ByteRange resolves a KeyframeEntry.ByteOffset to the file offset and length
// of the block opening at that keyframe. The returned range is one complete
// zstd frame and decompresses on its own, with no bytes before it.
//
// An offset that is not a block boundary returns ErrSeekTableCorrupt: it means
// the keyframe index and the seek table disagree, and serving the enclosing
// block instead would hand the caller a range that starts at the wrong frame.
func (i *BlockIndex) ByteRange(keyframeOffset uint64) (offset, length uint64, err error) {
	if _, blockLen, err := blockAt(i.entries, keyframeOffset); err != nil {
		return 0, 0, err
	} else {
		return keyframeOffset, blockLen, nil
	}
}

// DecompressedSize reports the uncompressed size of block n, which is what a
// caller needs to size a buffer before decoding a served range.
func (i *BlockIndex) DecompressedSize(n int) (uint32, error) {
	if n < 0 || n >= len(i.entries) {
		return 0, fmt.Errorf("tape: block %d out of range (%d blocks): %w", n, len(i.entries), ErrSeekTableCorrupt)
	}
	return i.entries[n].decompressedSize, nil
}

// BlockRange returns the file offset and length of block n. Block 0 is the
// header block and the last block is the footer block; the blocks between them
// each open at a keyframe.
func (i *BlockIndex) BlockRange(n int) (offset, length uint64, err error) {
	if n < 0 || n >= len(i.entries) {
		return 0, 0, fmt.Errorf("tape: block %d out of range (%d blocks): %w", n, len(i.entries), ErrSeekTableCorrupt)
	}
	for _, e := range i.entries[:n] {
		offset += uint64(e.compressedSize)
	}
	return offset, uint64(i.entries[n].compressedSize), nil
}

// ReadBlock reads and decompresses block n, using dict if the capture was
// written with a trained dictionary (nil otherwise). It reads only that block's
// bytes: nothing before it is touched, which is the property the per-block
// layout exists to provide.
func (i *BlockIndex) ReadBlock(n int, dict []byte) ([]byte, error) {
	offset, length, err := i.BlockRange(n)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(i.path) //nolint:gosec // path came from OpenBlockIndex
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck // read-only

	raw := make([]byte, length)
	if _, readErr := file.ReadAt(raw, int64(offset)); readErr != nil { //nolint:gosec // offset is a file position
		return nil, fmt.Errorf("tape: reading block %d at %d: %w", n, offset, readErr)
	}

	var dopts []zstd.DOption
	if len(dict) > 0 {
		dopts = append(dopts, zstd.WithDecoderDicts(dict))
	}
	dec, err := zstd.NewReader(nil, dopts...)
	if err != nil {
		return nil, fmt.Errorf("tape: block %d decoder: %w", n, err)
	}
	defer dec.Close()

	out, err := dec.DecodeAll(raw, nil)
	if err != nil {
		return nil, fmt.Errorf("tape: decompressing block %d: %w", n, err)
	}
	return out, nil
}

// Footer reads the CaptureFooter out of the capture's final block.
//
// This is the reason the footer gets a block of its own. The footer carries the
// keyframe index, so a seeking reader needs it BEFORE it can resolve a frame to
// a byte range — and reading it through the sequential Reader means walking
// every frame in the capture, which costs exactly what seeking was supposed to
// save. Here it costs one block.
func (i *BlockIndex) Footer(dict []byte) (*capturepb.CaptureFooter, error) {
	if len(i.entries) == 0 {
		return nil, ErrNoSeekTable
	}
	block, err := i.ReadBlock(len(i.entries)-1, dict)
	if err != nil {
		return nil, err
	}

	r := &Reader{reader: bytes.NewReader(block), limits: Limits{}}
	env, err := r.readEnvelope()
	if err != nil {
		return nil, fmt.Errorf("tape: reading footer envelope: %w", err)
	}
	footer := env.GetFooter()
	if footer == nil {
		return nil, fmt.Errorf("tape: final block does not hold a footer: %w", ErrUnexpectedEnvelope)
	}
	return footer, nil
}

// BlockForFrame returns the index of the block holding frameIndex, resolved
// against the footer's keyframe index. It is the lookup a byte-range server
// performs: frame number in, servable block out.
//
// A frame before the first keyframe cannot be resolved, because no block is
// known to contain it.
func (i *BlockIndex) BlockForFrame(footer *capturepb.CaptureFooter, frameIndex uint32) (int, error) {
	var offset uint64
	found := false
	for _, kf := range footer.GetKeyframeIndex() {
		if kf.GetFrameIndex() > frameIndex {
			break
		}
		offset, found = kf.GetByteOffset(), true
	}
	if !found {
		return 0, fmt.Errorf("tape: frame %d precedes every keyframe in the index: %w",
			frameIndex, ErrSeekTableCorrupt)
	}
	block, _, err := blockAt(i.entries, offset)
	if err != nil {
		return 0, err
	}
	return block, nil
}
