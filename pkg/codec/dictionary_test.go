package codec

import (
	"errors"
	"io"
	"os"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"google.golang.org/protobuf/proto"
)

// TestTrainDictionaryFromCapturesRoundTrip is the end-to-end obligation: train
// a dictionary on a real capture, write a new capture with it, and read that
// capture back through the public reader with the dictionary in hand.
//
// It is the whole lifecycle in one test because a dictionary is only useful if
// all three steps work together — a trainer whose output the writer accepts but
// the reader cannot use would pass three narrower tests and be worthless.
func TestTrainDictionaryFromCapturesRoundTrip(t *testing.T) {
	frames := blockTestFrames(400)
	source := writeCapture(t, "source.tape", frames, WithKeyframeInterval(50))

	dict, used, err := TrainDictionaryFromCaptures(MinPrivateDictionaryID, nil, source)
	if err != nil {
		t.Fatalf("TrainDictionaryFromCaptures: %v", err)
	}
	if used != len(frames) {
		t.Errorf("trained on %d frames, capture has %d", used, len(frames))
	}
	if len(dict) == 0 {
		t.Fatal("trained an empty dictionary")
	}

	compressed := writeCapture(t, "dict.tape", frames,
		WithKeyframeInterval(50), WithPerBlockCompression(), WithDictionary(dict))

	r, err := NewReaderWithDictionary(compressed, dict)
	if err != nil {
		t.Fatalf("NewReaderWithDictionary: %v", err)
	}
	defer r.Close() //nolint:errcheck // read-only

	if _, err := r.ReadHeader(); err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	var got []*capturepb.Frame
	for {
		f, err := r.ReadFrame()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		got = append(got, f)
	}
	if len(got) != len(frames) {
		t.Fatalf("read %d frames, wrote %d", len(got), len(frames))
	}
	for i := range frames {
		if !proto.Equal(frames[i], got[i]) {
			t.Fatalf("frame %d differs after training, writing and reading with a dictionary", i)
		}
	}

	// The dictionary must earn its obligation: a capture written with it should
	// be smaller than the same capture written without it.
	plain := writeCapture(t, "plain.tape", frames,
		WithKeyframeInterval(50), WithPerBlockCompression())
	withDict, err := os.Stat(compressed)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	without, err := os.Stat(plain)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if withDict.Size() >= without.Size() {
		t.Errorf("dictionary made the capture no smaller: %d bytes with, %d without",
			withDict.Size(), without.Size())
	}
	// This corpus is synthetic and far more repetitive than real telemetry, so
	// the ratio here is not the format's number. The measurement that counts is
	// on the real corpus in pkg/conversion/shipped_layout_bench_test.go, where
	// the dictionary is worth 5.0-5.6%.
	t.Logf("SYNTHETIC corpus — not the format's ratio: per-block %d B, per-block+dict %d B (%.2f%%), dictionary %d B",
		without.Size(), withDict.Size(),
		100*float64(withDict.Size()-without.Size())/float64(without.Size()), len(dict))
}

// TestTrainDictionaryRejectsUnusableInput covers the two ways a caller can ask
// for a dictionary that cannot exist. Both must be errors rather than an empty
// or id-less dictionary, because either would produce captures that cannot say
// what they need to be read.
func TestTrainDictionaryRejectsUnusableInput(t *testing.T) {
	t.Run("id zero", func(t *testing.T) {
		// zstd reads a zero id as "no dictionary declared", so a capture
		// compressed with one could not announce its own requirement.
		if _, err := TrainDictionary(0, [][]byte{[]byte("sample")}); !errors.Is(err, ErrNoTrainingData) {
			t.Fatalf("got %v, want ErrNoTrainingData", err)
		}
	})
	t.Run("no samples", func(t *testing.T) {
		if _, err := TrainDictionary(MinPrivateDictionaryID, nil); !errors.Is(err, ErrNoTrainingData) {
			t.Fatalf("got %v, want ErrNoTrainingData", err)
		}
	})
	t.Run("no captures", func(t *testing.T) {
		if _, _, err := TrainDictionaryFromCaptures(MinPrivateDictionaryID, nil); !errors.Is(err, ErrNoTrainingData) {
			t.Fatalf("got %v, want ErrNoTrainingData", err)
		}
	})
}

// TestTrainedDictionaryIDIsOnTheWire ties the trainer to the format's own
// mechanism for expressing the obligation: whatever id the caller chose must
// appear in the frame headers of every capture written with the dictionary, so
// a reader can tell WHICH dictionary a capture needs, not merely that it needs
// one.
func TestTrainedDictionaryIDIsOnTheWire(t *testing.T) {
	const id = MinPrivateDictionaryID + 7
	frames := blockTestFrames(200)
	source := writeCapture(t, "source.tape", frames, WithKeyframeInterval(50))

	dict, _, err := TrainDictionaryFromCaptures(id, nil, source)
	if err != nil {
		t.Fatalf("TrainDictionaryFromCaptures: %v", err)
	}

	path := writeCapture(t, "ided.tape", frames,
		WithKeyframeInterval(50), WithPerBlockCompression(), WithDictionary(dict))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := frameDictionaryID(t, data); got != id {
		t.Fatalf("first block declares dictionary id %d, want %d", got, id)
	}
}

// TestTrainDictionaryOnACorpusSmallerThanTheHistory is F5: training failed on
// any corpus under DefaultDictionaryHistory (112 KiB) with an error that blamed
// the trainer.
//
// THE MECHANISM, which is why the boundary was exactly 114,688 bytes. The match
// history was built from the FIRST samples and the SAME samples were passed as
// Contents. When the whole corpus fits under the budget, every sample is
// already in the history, klauspost's BuildDict finds nothing left to learn
// from, and it reports "0 literals found". Measured: realistic ~216-byte frames
// failed at every n <= 500 (108,367 B cumulative) and succeeded from n >= 550
// (119,267 B).
//
// WHY IT MATTERS RATHER THAN BEING A CURIOSITY: TrainDictionaryFromCaptures on
// ONE capture is the operation someone actually performs first, and a single
// capture of a normal match is well under 112 KiB of payload. The feature was
// unreachable from its own front door.
func TestTrainDictionaryOnACorpusSmallerThanTheHistory(t *testing.T) {
	// REAL telemetry, not a synthetic corpus, and the distinction cost a
	// debugging pass. The first version of this test built 300 near-identical
	// 13-byte frames; those failed to train for a different and legitimate
	// reason — a corpus with no variety has nothing to learn from at any size —
	// which would have made this test fail forever on a correct fix. The
	// committed sample is what a capture actually looks like.
	sample := "../../testdata/sample.tape.golden"
	if _, err := os.Stat(sample); os.IsNotExist(err) {
		t.Skipf("%s not available", sample)
	}

	// Take frames until the marshalled payload is just under the history
	// budget. That is precisely the reported case: one short capture.
	src, err := NewReader(sample, WithoutLimits())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	header, err := src.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	var frames []*capturepb.Frame
	payloadBytes := 0
	for {
		f, err := src.ReadFrame()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		b, err := proto.Marshal(f)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if payloadBytes+len(b) >= DefaultDictionaryHistory {
			break
		}
		payloadBytes += len(b)
		frames = append(frames, f)
	}
	if err := src.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if payloadBytes >= DefaultDictionaryHistory {
		t.Fatalf("corpus is %d bytes, not smaller than the %d-byte history budget; this test "+
			"is not exercising the case it exists for", payloadBytes, DefaultDictionaryHistory)
	}
	if len(frames) < 2 {
		t.Fatalf("only %d frames fit under the history budget; nothing to train on", len(frames))
	}

	// Write them back out as a capture, so training runs through
	// TrainDictionaryFromCaptures — the front door, and the operation someone
	// performs first.
	short := t.TempDir() + "/short.tape"
	w, err := NewWriter(short)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteHeader(header); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	for _, f := range frames {
		if err := w.WriteFrame(f); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	dict, trained, err := TrainDictionaryFromCaptures(MinPrivateDictionaryID, nil, short)
	if err != nil {
		t.Fatalf("training on ONE capture of %d frames (%d payload bytes, under the %d-byte "+
			"history budget) failed: %v", len(frames), payloadBytes, DefaultDictionaryHistory, err)
	}
	if len(dict) == 0 {
		t.Fatal("training returned an empty dictionary with no error")
	}
	if trained != len(frames) {
		t.Errorf("trained on %d frames, capture holds %d", trained, len(frames))
	}

	// A dictionary that trains but does not work is not a fix. Round-trip a
	// capture through it.
	path := t.TempDir() + "/dict.tape"
	dw, err := NewWriterWithOptions(path, WithDictionary(dict))
	if err != nil {
		t.Fatalf("NewWriterWithOptions: %v", err)
	}
	if err := dw.WriteHeader(header); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	for _, f := range frames {
		if err := dw.WriteFrame(f); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
	}
	if err := dw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	r, err := NewReaderWithDictionary(path, dict, WithoutLimits())
	if err != nil {
		t.Fatalf("NewReaderWithDictionary: %v", err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()
	if _, err := r.ReadHeader(); err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	for i := range frames {
		got, err := r.ReadFrame()
		if err != nil {
			t.Fatalf("ReadFrame %d: %v", i, err)
		}
		if !proto.Equal(frames[i], got) {
			t.Fatalf("frame %d round-tripped differently through the dictionary", i)
		}
	}
	t.Logf("one capture of %d frames (%d payload bytes) trained a %d-byte dictionary",
		len(frames), payloadBytes, len(dict))
}
