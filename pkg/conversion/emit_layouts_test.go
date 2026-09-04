package conversion

import (
	"os"
	"testing"

	"github.com/echotools/tape/v4/pkg/codec"
	"google.golang.org/protobuf/proto"
)

// TestEmitLayoutsForCrossVersionCheck writes the fixed corpus in each layout to
// $TAPE_EMIT_DIR, so a reader BUILT FROM AN OLDER COMMIT can be pointed at the
// files. It is skipped unless that variable is set: it produces artifacts for
// an external check rather than asserting anything itself.
//
// The check it exists for cannot be written as an ordinary test, because an
// ordinary test compiles against the current package and therefore cannot be an
// old reader. The procedure:
//
//	git archive <old-commit> pkg/codec go.mod go.sum | tar -x -C /tmp/oldreader
//	# build a reader binary there, then:
//	TAPE_EMIT_DIR=/tmp/tapes go test ./pkg/conversion/ -run TestEmitLayouts
//	/tmp/oldreader/oldread /tmp/tapes/perblock.tape
//
// Run against 2ca18fa — the commit before per-block compression — that reader
// reads default.tape and perblock.tape in full, and fails loudly on
// perblock-dict.tape with "unknown dictionary". Those three results are the
// backward-compatibility evidence for the per-block layout.
func TestEmitLayoutsForCrossVersionCheck(t *testing.T) {
	dir := os.Getenv("TAPE_EMIT_DIR")
	if dir == "" {
		t.Skip("TAPE_EMIT_DIR not set; this test emits artifacts for a cross-version check")
	}
	frames := loadCorpusProtos(t)

	emit := func(name string, opts ...codec.WriterOption) {
		w, err := codec.NewWriterWithOptions(dir+"/"+name, opts...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := w.WriteHeader(shippedHeader()); err != nil {
			t.Fatalf("%s: WriteHeader: %v", name, err)
		}
		for _, f := range frames {
			if err := w.WriteFrame(f); err != nil {
				t.Fatalf("%s: WriteFrame: %v", name, err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatalf("%s: Close: %v", name, err)
		}
	}

	emit("default.tape")
	emit("perblock.tape", codec.WithKeyframeInterval(30), codec.WithPerBlockCompression())

	var samples [][]byte
	for _, f := range frames {
		b, err := proto.Marshal(f)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		samples = append(samples, b)
	}
	dict, err := codec.TrainDictionary(codec.MinPrivateDictionaryID, samples)
	if err != nil {
		t.Fatalf("TrainDictionary: %v", err)
	}
	emit("perblock-dict.tape", codec.WithKeyframeInterval(30),
		codec.WithPerBlockCompression(), codec.WithDictionary(dict))

	t.Logf("emitted 3 layouts of %d frames to %s (frame indices %d..%d)",
		len(frames), dir, frames[0].GetFrameIndex(), frames[len(frames)-1].GetFrameIndex())
}
