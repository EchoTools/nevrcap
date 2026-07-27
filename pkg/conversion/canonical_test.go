package conversion

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/echotools/tape/pkg/codec"
)

// Byte-level canonical round-trip.
//
// The field-level BAC (roundtrip_v2_test.go) compares parsed SessionResponse
// values, so it cannot see differences that survive parsing: a key the original
// omitted but the writer emits, a different number of teams, key ordering, or
// float formatting. This compares the bytes instead.
//
// The comparison is against the CANONICAL form of the original, not the
// original file. Captures in the wild are not canonical — the Spark recorder
// writes booleans where the proto wants numbers, older recorders omit
// client_name entirely, and exponent notation varies — so comparing raw source
// bytes would measure recorder drift rather than v2 fidelity. Canonicalizing
// means running the original through the echoreplay writer, which is the same
// writer the reconstruction uses. Both sides then differ only where v2 lost
// something.

// echoReplayBody returns the raw bytes of a capture's single zip member, or the
// whole file when it is not zipped.
func echoReplayBody(t *testing.T, path string) []byte {
	t.Helper()

	zr, err := zip.OpenReader(path)
	if err != nil {
		body, readErr := os.ReadFile(path) //nolint:gosec // test fixture path
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		return body
	}
	defer zr.Close() //nolint:errcheck // read-only test fixture

	if len(zr.File) != 1 {
		t.Fatalf("%s has %d zip members, want 1", path, len(zr.File))
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatalf("open member of %s: %v", path, err)
	}
	defer rc.Close() //nolint:errcheck // read-only test fixture

	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read member of %s: %v", path, err)
	}
	return body
}

// writeEchoReplayFrom reads src with the echoreplay codec and writes it back out
// through the echoreplay writer, producing the canonical spelling of whatever
// the reader understood.
func writeEchoReplayFrom(t *testing.T, src, dst string) {
	t.Helper()

	r, err := codec.NewEchoReplayReader(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	frames, err := r.ReadFrames()
	skipped := r.SkippedFrames()
	_ = r.Close()
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if skipped != 0 {
		t.Fatalf("reader skipped %d line(s) of %s; canonical comparison would "+
			"be against a subset of the source", skipped, src)
	}

	w, err := codec.NewEchoReplayWriter(dst)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	for i, f := range frames {
		if err := w.WriteFrame(f); err != nil {
			t.Fatalf("write frame %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close %s: %v", dst, err)
	}
}

// canonicalRoundTrip returns the canonical form of src and the canonical form of
// src after a full echoreplay -> tape -> echoreplay cycle.
func canonicalRoundTrip(t *testing.T, src string) (canonical, viaTape []byte) {
	t.Helper()

	dir := t.TempDir()
	canonPath := filepath.Join(dir, "canonical.echoreplay")
	tapePath := filepath.Join(dir, "out.tape")
	reconPath := filepath.Join(dir, "recon.echoreplay")

	writeEchoReplayFrom(t, src, canonPath)

	if _, err := ConvertFile(src, tapePath); err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}
	if _, err := ReconstructFile(tapePath, reconPath); err != nil {
		t.Fatalf("ReconstructFile: %v", err)
	}

	return echoReplayBody(t, canonPath), echoReplayBody(t, reconPath)
}

// firstDifference reports the byte offset of the first difference and the lines
// containing it, for a diagnosis that names the field rather than the offset.
func firstDifference(a, b []byte) (offset int, lineA, lineB string) {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}

	// Centre the excerpt on the divergence rather than the line start: frame
	// lines run to tens of kilobytes, so a line-start window usually shows
	// identical prefixes.
	const before, after = 60, 180
	from := max(i-before, 0)
	return i,
		string(a[from:min(i+after, len(a))]),
		string(b[from:min(i+after, len(b))])
}

// compareStructure asserts the two canonical forms agree on everything that is
// not a value: record count, field count per record, and the key set of each
// record. A missing or added key is unrecoverable data loss or invented data;
// either is fatal regardless of how the numbers are spelled.
func compareStructure(t *testing.T, canonical, viaTape []byte) {
	t.Helper()

	linesA := bytes.Split(bytes.TrimRight(canonical, "\r\n"), []byte("\n"))
	linesB := bytes.Split(bytes.TrimRight(viaTape, "\r\n"), []byte("\n"))
	if len(linesA) != len(linesB) {
		t.Fatalf("record count: canonical=%d via-tape=%d", len(linesA), len(linesB))
	}

	for i := range linesA {
		fieldsA := bytes.Split(bytes.TrimRight(linesA[i], "\r"), []byte("\t"))
		fieldsB := bytes.Split(bytes.TrimRight(linesB[i], "\r"), []byte("\t"))
		if len(fieldsA) != len(fieldsB) {
			t.Fatalf("record %d: field count canonical=%d via-tape=%d", i, len(fieldsA), len(fieldsB))
		}
		if !bytes.Equal(fieldsA[0], fieldsB[0]) {
			t.Fatalf("record %d: timestamp %q vs %q", i, fieldsA[0], fieldsB[0])
		}

		var objA, objB map[string]json.RawMessage
		if err := json.Unmarshal(fieldsA[1], &objA); err != nil {
			t.Fatalf("record %d: parse canonical: %v", i, err)
		}
		if err := json.Unmarshal(fieldsB[1], &objB); err != nil {
			t.Fatalf("record %d: parse via-tape: %v", i, err)
		}
		for k := range objA {
			if _, ok := objB[k]; !ok {
				t.Errorf("record %d: key %q present in canonical, absent via tape", i, k)
			}
		}
		for k := range objB {
			if _, ok := objA[k]; !ok {
				t.Errorf("record %d: key %q invented by the round-trip", i, k)
			}
		}
	}
}

// TestCanonicalRoundTripStructure is the assertion that holds today: the
// canonical round-trip preserves every record and every key.
//
// Full BYTE identity does not hold yet, for two reasons, both tracked as
// BUGS.md CANONICAL-001 and reported (not asserted) by
// TestCanonicalRoundTripByteDelta below:
//
//  1. last_throw / last_score are not wired through v2 at all.
//  2. The engine spells floats with %.8g, trailing zeros trimmed to one decimal
//     place; protojson spells them with shortest-round-trip float64. Every
//     engine float is exactly a float32, so this is formatting, not lost data.
func TestCanonicalRoundTripStructure(t *testing.T) {
	t.Parallel()

	src := filepath.Join("..", "..", "testdata", "sample.echoreplay")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no sample: %v", err)
	}

	canonical, viaTape := canonicalRoundTrip(t, src)
	compareStructure(t, canonical, viaTape)
}

// TestCanonicalRoundTripByteDelta reports how far the round-trip is from byte
// identity. It does not fail: it is the measurement that tells you when
// CANONICAL-001 is closed, at which point it should become an assertion.
func TestCanonicalRoundTripByteDelta(t *testing.T) {
	t.Parallel()

	src := filepath.Join("..", "..", "testdata", "sample.echoreplay")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no sample: %v", err)
	}

	canonical, viaTape := canonicalRoundTrip(t, src)
	if bytes.Equal(canonical, viaTape) {
		t.Log("CANONICAL BYTE-IDENTICAL — close BUGS.md CANONICAL-001 and make " +
			"TestCanonicalRoundTripStructure assert bytes.Equal instead")
		return
	}

	offset, lineA, lineB := firstDifference(canonical, viaTape)
	t.Logf("not yet byte-identical (BUGS.md CANONICAL-001)\n"+
		"  canonical: %d bytes\n  via tape : %d bytes\n"+
		"  first difference at byte %d\n    canonical: %s\n    via tape : %s",
		len(canonical), len(viaTape), offset, lineA, lineB)
}

// TestCanonicalRoundTripAudit runs the same byte comparison on an external
// recording. Set TAPE_AUDIT_FILE to an absolute path.
func TestCanonicalRoundTripAudit(t *testing.T) {
	src := os.Getenv("TAPE_AUDIT_FILE")
	if src == "" {
		t.Skip("set TAPE_AUDIT_FILE=/absolute/path.echoreplay to run the canonical audit")
	}
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no audit file: %v", err)
	}

	canonical, viaTape := canonicalRoundTrip(t, src)
	compareStructure(t, canonical, viaTape)

	if bytes.Equal(canonical, viaTape) {
		t.Logf("CANONICAL BYTE-IDENTICAL: %s (%d bytes)", src, len(canonical))
		return
	}

	offset, lineA, lineB := firstDifference(canonical, viaTape)
	t.Logf("structure identical; not yet byte-identical for %s (BUGS.md CANONICAL-001)\n"+
		"  canonical: %d bytes\n  via tape : %d bytes\n"+
		"  first difference at byte %d\n    canonical: %s\n    via tape : %s",
		src, len(canonical), len(viaTape), offset, lineA, lineB)
}
