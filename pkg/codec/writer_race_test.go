package codec

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
)

// TestWriter_ConcurrentWriteFrameRace proves GH #29: the Writer carries shared
// mutable state (frameCount, keyframes, eventIndex, bytesWritten, the zstd
// encoder) with no synchronization. Concurrent WriteFrame calls race on those
// fields.
//
// The test spawns N goroutines each writing a frame and verifies no panic. Under
// `-race` the unsynchronized reads and writes to frameCount (line 90, 117),
// eventIndex (line 105), keyframes (line 94), bytesWritten (line 96, generated
// writeEnvelope), and lastTimestampMs (line 118) are detected.
//
// Currently RED under -race: the Writer has no mutex. The dedicated test is
// designed to trigger the race detector on every field that is read in one
// goroutine while another writes it.
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

	const concurrentWriters = 8

	var wg sync.WaitGroup
	wg.Add(concurrentWriters)

	errs := make([]error, concurrentWriters)
	for i := range concurrentWriters {
		go func(idx int) {
			defer wg.Done()
			frame := &capturepb.Frame{
				FrameIndex:        uint32(idx),
				TimestampOffsetMs: uint32(idx * 16),
				Payload: &capturepb.Frame_EchoArena{
					EchoArena: &capturepb.EchoArenaFrame{
						GameClock: float32(idx) * 10.0,
					},
				},
			}
			errs[idx] = w.WriteFrame(frame)
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: WriteFrame: %v", i, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// The resulting file should at least be readable: a header plus however many
	// frames survived the race.
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
		_, err := r.ReadFrame()
		if err != nil {
			break
		}
		read++
	}
	if read == 0 {
		t.Error("read 0 frames from concurrent output — data likely corrupted")
	}
	_ = os.Remove(path)
}
