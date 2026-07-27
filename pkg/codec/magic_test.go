package codec

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestStartsWith(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
		prefix  []byte
		want    bool
	}{
		{"exact match", []byte{0x28, 0xB5, 0x2F, 0xFD, 0x01}, zstdMagic, true},
		{"no match", []byte{'P', 'K', 0x03, 0x04}, zstdMagic, false},
		{"zip local file header", []byte{'P', 'K', 0x03, 0x04}, zipMagic, true},
		{"empty zip end-of-central-directory", []byte{'P', 'K', 0x05, 0x06}, zipMagic, true},
		{"shorter than prefix", []byte{0x28}, zstdMagic, false},
		{"empty file", nil, zstdMagic, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "probe.bin")
			if err := os.WriteFile(path, tt.content, 0o600); err != nil {
				t.Fatalf("write probe: %v", err)
			}

			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("open probe: %v", err)
			}
			defer f.Close() //nolint:errcheck // read-only test fixture

			got, err := startsWith(f, tt.prefix)
			if err != nil {
				t.Fatalf("startsWith: %v", err)
			}
			if got != tt.want {
				t.Errorf("startsWith = %v, want %v", got, tt.want)
			}

			// The sniff must leave the offset at the start so the real reader
			// sees the whole file.
			pos, err := f.Seek(0, io.SeekCurrent)
			if err != nil {
				t.Fatalf("tell: %v", err)
			}
			if pos != 0 {
				t.Errorf("read offset = %d after sniff, want 0", pos)
			}
		})
	}
}
