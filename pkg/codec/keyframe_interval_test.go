package codec

import (
	"path/filepath"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
)

// TestNewWriterWithKeyframeInterval_ClampsZero pins GH #23: a zero interval
// reached `frameIndex % w.keyframeInterval` and panicked with an integer divide
// by zero on the first WriteFrame. The constructor clamps instead, matching
// events.WithFrameBufferSize, which answers the same class of bug the same way.
func TestNewWriterWithKeyframeInterval_ClampsZero(t *testing.T) {
	t.Parallel()

	for _, interval := range []uint32{0, 1, DefaultKeyframeInterval} {
		w, err := NewWriterWithKeyframeInterval(filepath.Join(t.TempDir(), "out.tape"), interval)
		if err != nil {
			t.Fatalf("interval %d: new writer: %v", interval, err)
		}
		if err := w.WriteHeader(&capturepb.CaptureHeader{}); err != nil {
			t.Fatalf("interval %d: write header: %v", interval, err)
		}
		// The panic landed here, before any frame was encoded.
		if err := w.WriteFrame(&capturepb.Frame{}); err != nil {
			t.Fatalf("interval %d: write frame: %v", interval, err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("interval %d: close: %v", interval, err)
		}
	}
}

// TestKeyframeIntervalZeroIndexesLikeTheDefault checks the clamp picked a
// meaningful value rather than merely dodging the panic: a zero-interval writer
// must produce the same keyframe index as an explicit default-interval one.
func TestKeyframeIntervalZeroIndexesLikeTheDefault(t *testing.T) {
	t.Parallel()

	index := func(interval uint32) []uint32 {
		path := filepath.Join(t.TempDir(), "out.tape")
		w, err := NewWriterWithKeyframeInterval(path, interval)
		if err != nil {
			t.Fatalf("new writer: %v", err)
		}
		if err := w.WriteHeader(&capturepb.CaptureHeader{}); err != nil {
			t.Fatalf("write header: %v", err)
		}
		for range DefaultKeyframeInterval * 2 {
			if err := w.WriteFrame(&capturepb.Frame{}); err != nil {
				t.Fatalf("write frame: %v", err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		r, err := NewReader(path)
		if err != nil {
			t.Fatalf("new reader: %v", err)
		}
		defer r.Close() //nolint:errcheck // read-only assertion path
		if _, err := r.ReadHeader(); err != nil {
			t.Fatalf("read header: %v", err)
		}
		for {
			if _, err := r.ReadFrame(); err != nil {
				break
			}
		}
		footer, err := r.ReadFooter()
		if err != nil {
			t.Fatalf("read footer: %v", err)
		}
		var out []uint32
		for _, kf := range footer.GetKeyframeIndex() {
			out = append(out, kf.GetFrameIndex())
		}
		return out
	}

	zero, def := index(0), index(DefaultKeyframeInterval)
	if len(zero) != len(def) {
		t.Fatalf("zero interval indexed %d keyframes, default indexed %d", len(zero), len(def))
	}
	for i := range zero {
		if zero[i] != def[i] {
			t.Fatalf("keyframe %d: zero interval %d, default %d", i, zero[i], def[i])
		}
	}
}
