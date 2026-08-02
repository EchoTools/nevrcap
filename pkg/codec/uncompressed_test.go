package codec

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/klauspost/compress/zstd"
)

// Captures in the archive are not always in their expected container: some
// recorders wrote .echoreplay as raw NDJSON with no zip, and a .tape written by
// a tool that skipped compression is still a valid envelope stream. Before the
// magic sniff both cases failed with "invalid input: magic number mismatch".

// decompressTape writes the zstd-decoded bytes of src to a new file and returns
// its path, producing an uncompressed but structurally valid tape.
func decompressTape(t *testing.T, src string) string {
	t.Helper()

	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open compressed tape: %v", err)
	}
	defer in.Close() //nolint:errcheck // read-only test fixture

	dec, err := zstd.NewReader(in)
	if err != nil {
		t.Fatalf("open zstd decoder: %v", err)
	}
	defer dec.Close()

	dst := filepath.Join(t.TempDir(), "uncompressed.tape")
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create uncompressed tape: %v", err)
	}
	defer out.Close() //nolint:errcheck // closed explicitly below on the success path

	if _, err := io.Copy(out, dec.IOReadCloser()); err != nil { //nolint:gosec // fixed-size test fixture, not hostile input
		t.Fatalf("decompress: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close uncompressed tape: %v", err)
	}

	return dst
}

// writeTape produces a small compressed tape and returns its path along with the
// number of frames written.
func writeTape(t *testing.T, frames int) (string, int) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "capture.tape")
	w, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	if err := w.WriteHeader(&capturepb.CaptureHeader{
		CaptureId:     "uncompressed-test",
		FormatVersion: 2,
	}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}

	for i := range frames {
		if err := w.WriteFrame(&capturepb.Frame{
			FrameIndex:        uint32(i), //nolint:gosec // loop bound is a small test constant
			TimestampOffsetMs: uint32(i) * 16,
			Payload: &capturepb.Frame_EchoArena{
				EchoArena: &capturepb.EchoArenaFrame{},
			},
		}); err != nil {
			t.Fatalf("WriteFrame %d: %v", i, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Writer.Close: %v", err)
	}

	return path, frames
}

// readTapeByPath opens a tape, drains it, and returns its capture ID and frame
// count. Distinct from readAllFrames, which takes an open Reader and consumes
// the header itself.
func readTapeByPath(t *testing.T, path string) (string, int) {
	t.Helper()

	r, err := NewReader(path)
	if err != nil {
		t.Fatalf("NewReader(%s): %v", path, err)
	}
	defer r.Close() //nolint:errcheck // read-only test fixture

	header, err := r.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader(%s): %v", path, err)
	}

	count := 0
	for {
		_, err := r.ReadFrame()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ReadFrame %d (%s): %v", count, path, err)
		}
		count++
	}

	return header.GetCaptureId(), count
}

func TestNewReader_UncompressedTapeMatchesCompressed(t *testing.T) {
	t.Parallel()

	compressed, want := writeTape(t, 12)
	uncompressed := decompressTape(t, compressed)

	gotID, gotFrames := readTapeByPath(t, uncompressed)
	if gotID != "uncompressed-test" {
		t.Errorf("capture_id = %q, want %q", gotID, "uncompressed-test")
	}
	if gotFrames != want {
		t.Errorf("frames = %d, want %d", gotFrames, want)
	}

	wantID, wantFrames := readTapeByPath(t, compressed)
	if gotID != wantID || gotFrames != wantFrames {
		t.Errorf("uncompressed read (%q, %d frames) != compressed read (%q, %d frames)",
			gotID, gotFrames, wantID, wantFrames)
	}
}

// extractZipMember writes the first member of a zip to a bare file, producing an
// .echoreplay with no zip container.
func extractZipMember(t *testing.T, src, dstName string) string {
	t.Helper()

	zr, err := zip.OpenReader(src)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer zr.Close() //nolint:errcheck // read-only test fixture

	if len(zr.File) == 0 {
		t.Fatalf("zip %s has no members", src)
	}

	member, err := zr.File[0].Open()
	if err != nil {
		t.Fatalf("open zip member: %v", err)
	}
	defer member.Close() //nolint:errcheck // read-only test fixture

	dst := filepath.Join(t.TempDir(), dstName)
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create raw echoreplay: %v", err)
	}
	defer out.Close() //nolint:errcheck // closed explicitly below on the success path

	if _, err := io.Copy(out, member); err != nil {
		t.Fatalf("extract member: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close raw echoreplay: %v", err)
	}

	return dst
}

func countEchoReplayFrames(t *testing.T, path string) int {
	t.Helper()

	r, err := NewEchoReplayReader(path)
	if err != nil {
		t.Fatalf("NewEchoReplayReader(%s): %v", path, err)
	}
	defer r.Close() //nolint:errcheck // read-only test fixture

	frames, err := r.ReadFrames()
	if err != nil {
		t.Fatalf("ReadFrames(%s): %v", path, err)
	}
	if skipped := r.SkippedFrames(); skipped != 0 {
		t.Errorf("SkippedFrames(%s) = %d, want 0", path, skipped)
	}

	return len(frames)
}

func TestNewEchoReplayReader_UncompressedMatchesZipped(t *testing.T) {
	t.Parallel()

	// Build a zipped echoreplay, then strip the container.
	zipped := filepath.Join(t.TempDir(), "capture.echoreplay")
	w, err := NewEchoReplayWriter(zipped)
	if err != nil {
		t.Fatalf("NewEchoReplayWriter: %v", err)
	}

	const wantFrames = 8
	for range wantFrames {
		if err := w.WriteFrame(createTestFrame(t)); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("EchoReplay.Close: %v", err)
	}

	raw := extractZipMember(t, zipped, "capture.echoreplay")

	if got := countEchoReplayFrames(t, raw); got != wantFrames {
		t.Errorf("uncompressed frames = %d, want %d", got, wantFrames)
	}
	if got := countEchoReplayFrames(t, zipped); got != wantFrames {
		t.Errorf("zipped frames = %d, want %d", got, wantFrames)
	}
}
