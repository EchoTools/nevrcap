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
