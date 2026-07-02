package codec

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// BUGS.md SEC-002 — allocate-before-verify.
//
// BAC: A varint length prefix declaring MaxMessageSize (256 MiB) followed by
// EOF must NOT cause an allocation proportional to the declared length. The
// reader returns a clean error and total bytes allocated while reading the
// truncated stream stay far below the declared 256 MiB (threshold 64 MiB,
// which generously covers zstd decoder overhead but not the attack).

// writeZstdFile writes decompressed as a zstd-compressed file at path.
func writeZstdFile(t *testing.T, path string, decompressed []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	enc, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	if _, err := enc.Write(decompressed); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("close encoder: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
}

// allocDelta returns total bytes allocated while running f.
func allocDelta(f func()) uint64 {
	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)
	f()
	runtime.ReadMemStats(&m2)
	return m2.TotalAlloc - m1.TotalAlloc
}

// giantLengthPrefix is the varint encoding of MaxMessageSize (256 MiB =
// 0x10000000): five bytes claiming a payload that is not present.
var giantLengthPrefix = []byte{0x80, 0x80, 0x80, 0x80, 0x01}

// sec002AllocThreshold bounds acceptable allocation for reading the 5-byte
// truncated stream: well above zstd streaming-decoder overhead, well below
// the attacker-declared 256 MiB.
const sec002AllocThreshold = 64 << 20

func TestReader_SEC002_TruncatedGiantLengthPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "truncated.tape")
	writeZstdFile(t, path, giantLengthPrefix)

	delta := allocDelta(func() {
		r, err := NewReader(path)
		if err != nil {
			t.Fatalf("NewReader: %v", err)
		}
		defer r.Close()
		if _, err := r.ReadHeader(); err == nil {
			t.Fatal("expected error reading truncated stream, got nil")
		}
	})

	if delta > sec002AllocThreshold {
		t.Fatalf("Reader allocated %d bytes (%.1f MiB) reading a 5-byte truncated stream declaring 256 MiB; want < %d",
			delta, float64(delta)/(1<<20), int64(sec002AllocThreshold))
	}
}

func TestLegacyReader_SEC002_TruncatedGiantLengthPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "truncated.nevrcap")
	writeZstdFile(t, path, giantLengthPrefix)

	delta := allocDelta(func() {
		r, err := NewLegacyReader(path)
		if err != nil {
			t.Fatalf("NewLegacyReader: %v", err)
		}
		defer r.Close()
		if _, err := r.ReadHeader(); err == nil {
			t.Fatal("expected error reading truncated stream, got nil")
		}
	})

	if delta > sec002AllocThreshold {
		t.Fatalf("LegacyReader allocated %d bytes (%.1f MiB) reading a 5-byte truncated stream declaring 256 MiB; want < %d",
			delta, float64(delta)/(1<<20), int64(sec002AllocThreshold))
	}
}
