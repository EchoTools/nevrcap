package conversion

import (
	"errors"
	"path/filepath"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/echotools/tape/v4/pkg/codec"
)

// BUGS.md SEC-001 — decompression bomb, accumulation sites.
//
// BAC: the frame-accumulating entry points (OpenSession,
// NewSessionReconstructor) must respect the reader's configured budgets and
// return a clear error past budget instead of accumulating an unbounded
// []*Frame from a tiny hostile capture.

// writeBombTapeV2 writes a v2 tape of n minimal valid frames.
func writeBombTapeV2(t *testing.T, path string, n int) {
	t.Helper()
	w, err := codec.NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteHeader(&capturepb.CaptureHeader{}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	frame := &capturepb.Frame{}
	for range n {
		if err := w.WriteFrame(frame); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestOpenSession_SEC001_FrameBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.tape")
	writeBombTapeV2(t, path, 100_000)

	r, err := codec.NewReader(path, codec.WithMaxFrameCount(1000))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	if _, err := OpenSession(r); !errors.Is(err, codec.ErrMaxFrameCount) {
		t.Fatalf("OpenSession: want ErrMaxFrameCount, got %v", err)
	}
}

func TestNewSessionReconstructor_SEC001_FrameBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.tape")
	writeBombTapeV2(t, path, 100_000)

	r, err := codec.NewReader(path, codec.WithMaxFrameCount(1000))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	if _, err := NewSessionReconstructor(r); !errors.Is(err, codec.ErrMaxFrameCount) {
		t.Fatalf("NewSessionReconstructor: want ErrMaxFrameCount, got %v", err)
	}
}

func TestOpenSession_SEC001_DecodedBytesBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.tape")
	writeBombTapeV2(t, path, 100_000)

	r, err := codec.NewReader(path, codec.WithMaxDecodedBytes(32<<10))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	if _, err := OpenSession(r); !errors.Is(err, codec.ErrMaxDecodedBytes) {
		t.Fatalf("OpenSession: want ErrMaxDecodedBytes, got %v", err)
	}
}
