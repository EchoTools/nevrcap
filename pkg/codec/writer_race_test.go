package codec

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
)

// TestWriter_ConcurrentWriteFrameRace proves GH #29: the Writer carries shared
// mutable state (frameCount, keyframes, eventIndex, bytesWritten, the zstd
// encoder) that concurrent calls must not race on.
//
// The writer's stream contract is one header, then sequential frames, then one
// Close (tape.go, R2 release-audit finding): frame_index is validated against
// the stream position, so only one producer can drive the valid stream. The
// test therefore runs a single producer goroutine writing the sequential
// stream while N hammer goroutines concurrently attempt invalid calls
// (duplicate WriteHeader, payload-less frame, nil frame). Those must all
// return clean errors without corrupting the stream. Under `-race`, removing
// the mutex would be detected on writer state (read by every hammer, written
// by the producer) and on the encoder.
func TestWriter_ConcurrentWriteFrameRace(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "concurrent.tape")
	w, err := NewWriter(path)
	if err != nil {
		t.Fatal(err)
	}

	// Write a header so the stream is valid.
	if err := w.WriteHeader(&capturepb.CaptureHeader{}); err != nil {
		t.Fatal(err)
	}

	const producerFrames = 500
	const hammers = 8

	errs := make([]error, hammers)

	var wg sync.WaitGroup
	wg.Add(hammers)
	for i := range hammers {
		go func(idx int) {
			defer wg.Done()
			switch idx % 3 {
			case 0:
				// Duplicate header: always out of order.
				errs[idx] = w.WriteHeader(&capturepb.CaptureHeader{})
			case 1:
				// Payload-less frame: always invalid (a tape frame must carry
				// game state), rejected without mutating the stream.
				errs[idx] = w.WriteFrame(&capturepb.Frame{FrameIndex: uint32(idx + 1000)})
			case 2:
				// Nil frame: rejected, no state mutated.
				errs[idx] = w.WriteFrame(nil)
			}
		}(i)
	}

	// The single producer drives the valid sequential stream while the hammers
	// contend for the mutex.
	for i := range uint32(producerFrames) {
		frame := &capturepb.Frame{
			FrameIndex:        i,
			TimestampOffsetMs: i * 16,
			Payload: &capturepb.Frame_EchoArena{
				EchoArena: &capturepb.EchoArenaFrame{
					GameClock: float32(i) * 0.5,
				},
			},
		}
		if err := w.WriteFrame(frame); err != nil {
			t.Fatalf("producer WriteFrame %d: %v", i, err)
		}
	}
	wg.Wait()

	for i, err := range errs {
		if err == nil {
			t.Errorf("hammer %d: expected an error, got nil", i)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// The resulting file is a valid sequential stream: a header plus exactly
	// the producer's frames, each carrying its stream position as frame_index.
	r, err := NewReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close() //nolint:errcheck // test cleanup
	if _, err := r.ReadHeader(); err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	read := 0
	for {
		f, err := r.ReadFrame()
		if err != nil {
			break
		}
		if f.GetFrameIndex() != uint32(read) {
			t.Fatalf("frame %d read with frame_index %d", read, f.GetFrameIndex())
		}
		read++
	}
	if read != producerFrames {
		t.Errorf("read %d frames, want %d", read, producerFrames)
	}

	if !errors.Is(errs[0], ErrWriteOrder) || !errors.Is(errs[1], ErrEmptyFrame) || !errors.Is(errs[2], ErrNilFrame) {
		t.Errorf("hammer errors did not match the expected sentinels: %v", errs)
	}
}
