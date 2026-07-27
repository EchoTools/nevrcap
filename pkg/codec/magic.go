package codec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

// Container magic numbers. Captures in the wild are not always wrapped in their
// expected container: some recorders emitted raw NDJSON with no zip, and a tape
// written by a tool that skipped compression is still a valid envelope stream.
// Readers sniff for these rather than assuming, so an uncompressed capture is
// read instead of rejected with a magic-number mismatch.
var (
	// zstdMagic is the Zstandard frame magic number (RFC 8878 §3.1.1).
	zstdMagic = []byte{0x28, 0xB5, 0x2F, 0xFD}
	// zipMagic is the two-byte prefix shared by every PKZIP record signature
	// (local file, central directory, end-of-central-directory), so an empty
	// archive is still recognized as a zip.
	zipMagic = []byte{'P', 'K'}
)

// startsWith reports whether f begins with prefix, leaving the read offset back
// at the start. A file shorter than prefix is simply not a match.
func startsWith(f *os.File, prefix []byte) (bool, error) {
	buf := make([]byte, len(prefix))
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, fmt.Errorf("codec.startsWith: read magic: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false, fmt.Errorf("codec.startsWith: rewind: %w", err)
	}
	return n == len(prefix) && bytes.Equal(buf, prefix), nil
}

// fileStartsWith opens filename solely to sniff its leading bytes. Used where
// the container decision must be made before the file is opened for real (a zip
// reader takes a path, not a handle).
func fileStartsWith(filename string, prefix []byte) (bool, error) {
	f, err := os.Open(filename) //nolint:gosec // filename is caller-provided path
	if err != nil {
		return false, fmt.Errorf("codec.fileStartsWith: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only sniff; no writes to flush

	return startsWith(f, prefix)
}
