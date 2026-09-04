package codec

import (
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
)

// Training a zstd dictionary for the tape format.
//
// WHY A DICTIONARY AT ALL. Per-block compression (tape_blocks.go) resets zstd's
// match window at every block boundary. On this corpus that reset costs +2.7% at
// one-second blocks — small, because whole-stream compression was barely working
// across frames to begin with. A trained dictionary is the standard mitigation,
// and telemetry is close to its ideal case: many small payloads, identical field
// structure, no shared window. Measured on the standing corpus, the dictionary
// does not merely recover the reset — it lands 5.0-5.6% BELOW the whole-stream
// layout, so seekability stops being a premium and becomes a discount
// (pkg/conversion/shipped_layout_bench_test.go).
//
// WHAT A DICTIONARY OBLIGATES. It is permanent. A capture written with
// dictionary D needs D forever. zstd records the dictionary's id in every frame
// header it writes, so a reader without it fails loudly rather than returning
// wrong bytes — but the bytes themselves must be stored somewhere the reader can
// reach. Training, storage and distribution are operational questions this
// package does not answer; it answers only "how is a dictionary for this format
// produced", so that the obligation is at least reproducible.

const (
	// DefaultDictionaryHistory is the training history size. 112 KiB is the
	// conventional zstd dictionary size, and it is what the standing
	// format-property benchmarks trained against, so a dictionary built here
	// is comparable to the numbers those benchmarks report.
	DefaultDictionaryHistory = 112 << 10

	// MinPrivateDictionaryID is the lowest dictionary id safe for captures
	// that may be distributed. RFC 8878 §5 reserves ids <= 32767 and >= 2^31
	// for public distribution; anything between is available. A private
	// corpus may use any id, but starting above the reserved low range costs
	// nothing and avoids a collision that would be discovered late.
	MinPrivateDictionaryID = 32768
)

// ErrNoTrainingData reports that dictionary training was asked for with nothing
// to train on. It is distinct from a training failure: an empty corpus is a
// caller mistake, not a defect in the trainer.
var ErrNoTrainingData = errors.New("tape: no frames to train a dictionary on")

// TrainDictionary builds a zstd dictionary from marshalled frame payloads.
//
// id identifies the dictionary permanently; see MinPrivateDictionaryID. Zero is
// rejected, because zstd treats a zero id as "no dictionary declared", which
// would leave a capture that needs a dictionary unable to say so.
//
// The samples train the entropy tables; the first DefaultDictionaryHistory
// bytes of them, in order, become the match history every block compresses
// against. Both are required — zstd rejects an empty history.
func TrainDictionary(id uint32, samples [][]byte) ([]byte, error) {
	if id == 0 {
		return nil, fmt.Errorf("tape: dictionary id 0 means \"no dictionary\" to zstd: %w", ErrNoTrainingData)
	}
	if len(samples) == 0 {
		return nil, ErrNoTrainingData
	}

	history := make([]byte, 0, DefaultDictionaryHistory)
	for _, s := range samples {
		if len(history)+len(s) > DefaultDictionaryHistory {
			break
		}
		history = append(history, s...)
	}
	if len(history) == 0 {
		return nil, fmt.Errorf("tape: every training sample exceeds the %d-byte history budget: %w",
			DefaultDictionaryHistory, ErrNoTrainingData)
	}

	dict, err := zstd.BuildDict(zstd.BuildDictOptions{
		ID:       id,
		Contents: samples,
		History:  history,
		Level:    zstd.SpeedDefault,
	})
	if err != nil {
		return nil, fmt.Errorf("tape: training dictionary %d on %d samples: %w", id, len(samples), err)
	}
	return dict, nil
}

// TrainDictionaryFromCaptures trains a dictionary on the frames of existing
// captures — the operation someone actually performs, since a dictionary is only
// worth anything if it was trained on the traffic it will compress.
//
// It reads each capture through the public reader, so it works on either
// container layout and on a capture whose own frames were dictionary-compressed
// (pass that dictionary as sourceDict; nil otherwise).
//
// Frames are sampled in file order across all inputs. It reports how many frames
// it trained on, because a dictionary trained on one short capture and one
// trained on a corpus are different artifacts with the same shape, and the
// caller is the only one who can judge whether the count is enough.
func TrainDictionaryFromCaptures(id uint32, sourceDict []byte, filenames ...string) ([]byte, int, error) {
	if len(filenames) == 0 {
		return nil, 0, ErrNoTrainingData
	}

	var samples [][]byte
	var historyBytes int
	for _, name := range filenames {
		r, err := NewReaderWithDictionary(name, sourceDict, WithoutLimits())
		if err != nil {
			return nil, 0, fmt.Errorf("tape: training on %s: %w", name, err)
		}
		if _, err := r.ReadHeader(); err != nil {
			return nil, 0, errors.Join(fmt.Errorf("tape: training on %s: %w", name, err), r.Close())
		}
		for {
			frame, err := r.ReadFrame()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, 0, errors.Join(fmt.Errorf("tape: training on %s: %w", name, err), r.Close())
			}
			payload, err := proto.Marshal(frame)
			if err != nil {
				return nil, 0, errors.Join(fmt.Errorf("tape: training on %s: %w", name, err), r.Close())
			}
			samples = append(samples, payload)
			// Once the history budget is covered there is no reason to keep
			// accumulating samples from a huge corpus: the entropy tables
			// converge long before, and the memory does not.
			historyBytes += len(payload)
		}
		if err := r.Close(); err != nil {
			return nil, 0, fmt.Errorf("tape: training on %s: %w", name, err)
		}
		if historyBytes >= DefaultDictionaryHistory*trainingSampleFactor {
			break
		}
	}

	dict, err := TrainDictionary(id, samples)
	if err != nil {
		return nil, 0, err
	}
	return dict, len(samples), nil
}

// trainingSampleFactor bounds how much more content is fed to the entropy
// trainer than ends up in the match history. The entropy tables benefit from
// more samples than the history holds, but the benefit flattens; reading an
// unbounded corpus into memory to chase it does not.
const trainingSampleFactor = 8
