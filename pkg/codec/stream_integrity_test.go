package codec

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
)

// TestReader_MidStreamNonFrameEnvelopeErrors proves F-10: a non-frame,
// non-footer envelope encountered mid-stream (here a stray second header, as
// from a truncated-and-concatenated capture) must ERROR, not signal a clean EOF
// that silently truncates the frame stream.
func TestReader_MidStreamNonFrameEnvelopeErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concat.tape")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	writeEnv := func(e *capturepb.Envelope) {
		data, err := proto.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		var buf [10]byte
		l := uint64(len(data))
		i := 0
		for l >= 0x80 {
			buf[i] = byte(l) | 0x80
			l >>= 7
			i++
		}
		buf[i] = byte(l)
		i++
		if _, err := enc.Write(buf[:i]); err != nil {
			t.Fatal(err)
		}
		if _, err := enc.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	writeEnv(&capturepb.Envelope{Message: &capturepb.Envelope_Header{Header: &capturepb.CaptureHeader{}}})
	writeEnv(&capturepb.Envelope{Message: &capturepb.Envelope_Frame{Frame: &capturepb.Frame{}}})
	writeEnv(&capturepb.Envelope{Message: &capturepb.Envelope_Header{Header: &capturepb.CaptureHeader{}}}) // stray
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := NewReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close() //nolint:errcheck // test cleanup
	if _, err := r.ReadHeader(); err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if _, err := r.ReadFrame(); err != nil {
		t.Fatalf("ReadFrame #1: %v", err) // the real frame
	}
	_, err = r.ReadFrame() // hits the stray header
	if !errors.Is(err, ErrUnexpectedEnvelope) {
		t.Fatalf("mid-stream stray header: want ErrUnexpectedEnvelope, got %v", err)
	}
}
