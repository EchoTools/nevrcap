package codec

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
)

// GH #41: the reader must validate the footer against what the stream actually
// carried. A capture whose footer disagrees with its frames has lost data, and
// answering io.EOF there reports success on a damaged file.

// writeRawCapture builds a Zstd envelope stream by hand so a footer can lie
// about frame_count. The Writer always emits a truthful count, so a
// hand-rolled stream is the only way to exercise the mismatch path.
func writeRawCapture(t *testing.T, path string, frames int, declaredCount uint32) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer file.Close() //nolint:errcheck // test cleanup

	enc, err := zstd.NewWriter(file)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}

	writeEnv := func(env *capturepb.Envelope) {
		t.Helper()
		data, err := proto.Marshal(env)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var buf [10]byte
		length := uint64(len(data))
		i := 0
		for length >= 0x80 {
			buf[i] = byte(length) | 0x80
			length >>= 7
			i++
		}
		buf[i] = byte(length)
		i++
		if _, err := enc.Write(buf[:i]); err != nil {
			t.Fatalf("write len: %v", err)
		}
		if _, err := enc.Write(data); err != nil {
			t.Fatalf("write body: %v", err)
		}
	}

	writeEnv(&capturepb.Envelope{
		Message: &capturepb.Envelope_Header{Header: &capturepb.CaptureHeader{FormatVersion: 2}},
	})
	for i := range frames {
		writeEnv(&capturepb.Envelope{
			Message: &capturepb.Envelope_Frame{Frame: &capturepb.Frame{
				FrameIndex: uint32(i), //nolint:gosec // small test loop bound
				Payload:    &capturepb.Frame_EchoArena{EchoArena: &capturepb.EchoArenaFrame{}},
			}},
		})
	}
	writeEnv(&capturepb.Envelope{
		Message: &capturepb.Envelope_Footer{Footer: &capturepb.CaptureFooter{FrameCount: declaredCount}},
	})

	if err := enc.Close(); err != nil {
		t.Fatalf("zstd close: %v", err)
	}
}

// drain reads frames to the end and returns the terminating error.
func drain(t *testing.T, path string) error {
	t.Helper()
	r, err := NewReader(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close() //nolint:errcheck // test cleanup
	if _, err := r.ReadHeader(); err != nil {
		t.Fatalf("header: %v", err)
	}
	for {
		if _, err := r.ReadFrame(); err != nil {
			return err
		}
	}
}

func TestReadFrame_FooterOvercountIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overcount.tape")
	// Footer claims 10 frames; the stream carries 3. This is what a truncated
	// capture looks like.
	writeRawCapture(t, path, 3, 10)

	err := drain(t, path)
	if !errors.Is(err, ErrFooterMismatch) {
		t.Fatalf("want ErrFooterMismatch for a footer claiming more frames than the stream carried, got %v", err)
	}
	if errors.Is(err, io.EOF) {
		t.Fatal("a damaged capture must not terminate as a clean io.EOF")
	}
}

func TestReadFrame_FooterUndercountIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "undercount.tape")
	// Footer claims 1 frame; the stream carries 5 — a concatenated capture.
	writeRawCapture(t, path, 5, 1)

	if err := drain(t, path); !errors.Is(err, ErrFooterMismatch) {
		t.Fatalf("want ErrFooterMismatch for a concatenated capture, got %v", err)
	}
}

func TestReadFrame_HonestFooterIsCleanEOF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "honest.tape")
	writeRawCapture(t, path, 4, 4)

	err := drain(t, path)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("an intact capture must terminate with io.EOF, got %v", err)
	}
	if errors.Is(err, ErrFooterMismatch) {
		t.Fatal("intact capture wrongly flagged as a footer mismatch")
	}
}

func TestReadFrame_EmptyCaptureIsCleanEOF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.tape")
	writeRawCapture(t, path, 0, 0)

	if err := drain(t, path); !errors.Is(err, io.EOF) {
		t.Fatalf("a zero-frame capture is intact, want io.EOF, got %v", err)
	}
}

// A caller that stops early is doing a partial read, not reading a damaged
// file. ReadFooter must not manufacture a mismatch from it.
func TestReadFooter_PartialReadIsNotAMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.tape")
	writeRawCapture(t, path, 6, 6)

	r, err := NewReader(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close() //nolint:errcheck // test cleanup
	if _, err := r.ReadHeader(); err != nil {
		t.Fatalf("header: %v", err)
	}
	// Read two of six frames, then stop.
	for range 2 {
		if _, err := r.ReadFrame(); err != nil {
			t.Fatalf("frame: %v", err)
		}
	}
	// Draining the rest reaches the footer honestly.
	for {
		if _, err := r.ReadFrame(); err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("want io.EOF after a full scan, got %v", err)
			}
			break
		}
	}
	footer, err := r.ReadFooter()
	if err != nil {
		t.Fatalf("ReadFooter: %v", err)
	}
	if footer.GetFrameCount() != 6 {
		t.Fatalf("footer frame_count = %d, want 6", footer.GetFrameCount())
	}
}

// The Writer's own output must satisfy the check it is validated against —
// otherwise every capture this library produces would fail to read back.
func TestWriterOutputSatisfiesFooterValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roundtrip.tape")

	w, err := NewWriter(path)
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	if err := w.WriteHeader(&capturepb.CaptureHeader{FormatVersion: 2}); err != nil {
		t.Fatalf("header: %v", err)
	}
	for i := range 7 {
		if err := w.WriteFrame(&capturepb.Frame{
			FrameIndex: uint32(i), //nolint:gosec // small test loop bound
			Payload:    &capturepb.Frame_EchoArena{EchoArena: &capturepb.EchoArenaFrame{}},
		}); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := drain(t, path); !errors.Is(err, io.EOF) {
		t.Fatalf("writer output must read back as intact, got %v", err)
	}
}
