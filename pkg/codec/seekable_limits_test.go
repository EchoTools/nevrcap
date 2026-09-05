package codec

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// The seeking read path's resource budgets — F4.
//
// WHY THIS FILE EXISTS. SEC-001 (decompression bombs) and SEC-002
// (allocate-before-verify) are enforced by the SEQUENTIAL reader and were not
// reachable from BlockIndex at all: Limits could not be passed to
// OpenBlockIndex, ReadBlock sized its buffer from a uint32 read straight off
// disk before reading a byte, and its decoder was constructed with no
// WithDecoderMaxMemory (klauspost's own default cap is 64 GiB). Measured by the
// bug hunt, 2026-09-05:
//
//	sequential Reader, WithMaxDecodedBytes(16 MiB): clean error, HeapAlloc 9 MiB
//	BlockIndex.ReadBlock(1): len=2147483648 err=nil in 10.5s, HeapAlloc 3699 MiB
//	compressedSize=0xFFFFFFFF: ReadBlock(1) -> "reading block 1 at 52: EOF"
//	                           AFTER HeapAlloc 4097 MiB
//
// The second line is the shape of the defect precisely: the error was correct
// and it arrived AFTER the allocation. A reader that refuses a hostile file
// only once it has already allocated what the file asked for has not refused
// it.
//
// The budgets are the same ones the sequential reader uses, reached the same
// way — a ReaderOption — so a caller does not have to know which read path a
// library function took in order to bound it.

// seekTableEntryOffsets returns the byte offsets, within a per-block capture,
// of entry n's Compressed_Size and Decompressed_Size fields.
//
// The layout is fixed by the zstd seekable format and is documented at the top
// of seekable.go: the table is the last thing in the file, ending with
// Number_Of_Frames (4) + Seek_Table_Descriptor (1) + Seekable_Magic_Number (4),
// preceded by the entries. Computing it here rather than exporting a mutator
// keeps the crafting in the test, where a hostile file belongs.
func seekTableEntryOffsets(t *testing.T, data []byte, n, blocks int) (compressed, decompressed int) {
	t.Helper()
	entriesEnd := len(data) - seekTableFooterSize
	entriesStart := entriesEnd - blocks*seekTableEntrySize
	if entriesStart < 0 {
		t.Fatalf("file of %d bytes cannot hold a %d-entry seek table", len(data), blocks)
	}
	// Sanity: the footer magic must actually be where the format says.
	if got := binary.LittleEndian.Uint32(data[len(data)-4:]); got != seekableFooterMagic {
		t.Fatalf("no seekable footer magic at EOF (got %#x); this helper is reading the wrong bytes", got)
	}
	base := entriesStart + n*seekTableEntrySize
	return base, base + 4
}

// allocatedDuring reports how many bytes were allocated while fn ran.
// TotalAlloc is cumulative and monotonic, so a gigabyte-scale make() is
// unmistakable in it regardless of what the collector does afterwards — which
// is the point, since the defect is an allocation that happens and is then
// thrown away.
func allocatedDuring(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestReadBlockRefusesASizeTheFileCannotHold is SEC-002 on the seeking path: a
// seek table claiming a block larger than the file must be refused BEFORE the
// buffer is allocated.
func TestReadBlockRefusesASizeTheFileCannotHold(t *testing.T) {
	path, _ := corruptionCorpus(t, "corpus.tape")
	clean, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	index, err := OpenBlockIndex(path)
	if err != nil {
		t.Fatalf("OpenBlockIndex: %v", err)
	}
	blocks := index.Blocks()

	// One gibibyte, from a 1.2 KB file. The format's field is uint32, so this
	// is well inside what a crafted table can say; it is deliberately smaller
	// than the 4 GiB the bug hunt used, because the assertion is about whether
	// the allocation happens at all, not about how large it can be made.
	const claimed = 1 << 30
	damaged := append([]byte(nil), clean...)
	off, _ := seekTableEntryOffsets(t, damaged, 1, blocks)
	binary.LittleEndian.PutUint32(damaged[off:], claimed)

	target := filepath.Join(t.TempDir(), "huge-block.tape")
	if err := os.WriteFile(target, damaged, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	bad, err := OpenBlockIndex(target)
	if err != nil {
		t.Fatalf("OpenBlockIndex on the crafted file: %v", err)
	}

	var readErr error
	allocated := allocatedDuring(func() {
		_, readErr = bad.ReadBlock(1, nil)
	})

	if readErr == nil {
		t.Fatal("ReadBlock returned no error for a block claiming 1 GiB from a 1.2 KB file")
	}
	if !errors.Is(readErr, ErrSeekTableCorrupt) {
		t.Errorf("ReadBlock returned %v; want ErrSeekTableCorrupt — a size the file cannot "+
			"hold is the table disagreeing with the file, not an I/O failure", readErr)
	}
	// The whole point. An error that arrives after the allocation is not a
	// refusal; the budget must be checked against the file's actual size first.
	if allocated > 16<<20 {
		t.Errorf("ReadBlock allocated %d bytes refusing a block that claims %d from a %d-byte "+
			"file (SEC-002, allocate-before-verify). It must reject the size before allocating.",
			allocated, claimed, len(clean))
	}
	t.Logf("claimed %d bytes from a %d-byte file: %v, %d bytes allocated",
		claimed, len(clean), readErr, allocated)
}

// TestReadBlockBoundsDecoderMemory is SEC-001 on the seeking path: a block that
// decodes to far more than the seek table declares must be refused, and the
// refusal must not cost what the block asked for.
func TestReadBlockBoundsDecoderMemory(t *testing.T) {
	path, _ := corruptionCorpus(t, "corpus.tape")
	clean, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	index, err := OpenBlockIndex(path)
	if err != nil {
		t.Fatalf("OpenBlockIndex: %v", err)
	}
	blocks := index.Blocks()
	blockOff, blockLen, err := index.BlockRange(1)
	if err != nil {
		t.Fatalf("BlockRange: %v", err)
	}

	// 256 MiB of zeros is a few hundred compressed bytes: the classic bomb,
	// and small enough on disk that the crafted file stays tiny.
	const bombSize = 256 << 20
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd.NewWriter: %v", err)
	}
	bomb := enc.EncodeAll(make([]byte, bombSize), nil)
	if err := enc.Close(); err != nil {
		t.Fatalf("encoder close: %v", err)
	}
	t.Logf("bomb: %d compressed bytes decode to %d", len(bomb), bombSize)

	// Swap block 1 for the bomb and tell the seek table only how many bytes it
	// occupies — leaving Decompressed_Size at the honest, small original. The
	// table now describes a block that decodes to 256 MiB while claiming it
	// yields a few hundred bytes, which is exactly the disagreement a reader
	// must not resolve in the file's favour.
	damaged := make([]byte, 0, len(clean)+len(bomb))
	damaged = append(damaged, clean[:blockOff]...)
	damaged = append(damaged, bomb...)
	damaged = append(damaged, clean[blockOff+blockLen:]...)
	cOff, _ := seekTableEntryOffsets(t, damaged, 1, blocks)
	binary.LittleEndian.PutUint32(damaged[cOff:], uint32(len(bomb))) //nolint:gosec // bounded by the corpus

	target := filepath.Join(t.TempDir(), "bomb-block.tape")
	if err := os.WriteFile(target, damaged, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	bad, err := OpenBlockIndex(target)
	if err != nil {
		t.Fatalf("OpenBlockIndex on the crafted file: %v", err)
	}

	var out []byte
	var readErr error
	allocated := allocatedDuring(func() {
		out, readErr = bad.ReadBlock(1, nil)
	})

	if readErr == nil {
		t.Errorf("ReadBlock returned %d bytes and NO error for a block the seek table says "+
			"yields %d; a decoded size that disagrees with the index must be refused",
			len(out), binary.LittleEndian.Uint32(damaged[cOff+4:]))
	}
	if allocated > 64<<20 {
		t.Errorf("ReadBlock allocated %d bytes on a block that decodes to %d (SEC-001). The "+
			"decoder must be constructed with a memory cap from the reader's limits.",
			allocated, bombSize)
	}
	t.Logf("bomb block: err=%v, %d bytes allocated", readErr, allocated)
}

// TestOpenBlockIndexTakesReaderOptions is the reachability half of F4: Limits
// existed and this path could not be given them. The budget a caller sets must
// govern whichever read path a library function happens to take, because the
// caller does not choose that path.
func TestOpenBlockIndexTakesReaderOptions(t *testing.T) {
	path, _ := corruptionCorpus(t, "corpus.tape")

	// A budget far below any real block. The default must still read it.
	index, err := OpenBlockIndex(path)
	if err != nil {
		t.Fatalf("OpenBlockIndex: %v", err)
	}
	if _, err := index.ReadBlock(1, nil); err != nil {
		t.Fatalf("default-limits ReadBlock: %v", err)
	}

	tiny, err := OpenBlockIndex(path, WithMaxDecodedBytes(8))
	if err != nil {
		t.Fatalf("OpenBlockIndex with options: %v", err)
	}
	if _, err := tiny.ReadBlock(1, nil); !errors.Is(err, ErrMaxDecodedBytes) {
		t.Errorf("ReadBlock under WithMaxDecodedBytes(8) returned %v; want ErrMaxDecodedBytes", err)
	}

	// And the escape hatch reaches this path too.
	unlimited, err := OpenBlockIndex(path, WithoutLimits())
	if err != nil {
		t.Fatalf("OpenBlockIndex WithoutLimits: %v", err)
	}
	if _, err := unlimited.ReadBlock(1, nil); err != nil {
		t.Errorf("ReadBlock under WithoutLimits: %v", err)
	}
}
