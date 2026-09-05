package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/echotools/tape/v4/pkg/codec"
	"github.com/spf13/cobra"
)

// tapedeck's read path against the two things the writer can produce that it
// could not consume — F6.
//
// THE DEFECT CLASS, which AGENTS.md §4 already names: a write path with no
// operator read path. codec grew dictionary compression and a seekable layout;
// tapedeck could open neither. `tapedeck show` on a dictionary capture failed
// with "read header: unknown dictionary" and there was no flag to supply one
// (`tapedeck --help | grep -i dict` was empty), and `tapedeck trim` rewrote a
// per-block input as whole-stream, so a servable capture stopped being servable
// with nothing said.

// dictionaryCapture writes a capture compressed with a trained dictionary and
// returns the capture's path and the dictionary bytes.
func dictionaryCapture(t *testing.T) (tapePath, dictPath string) {
	t.Helper()
	if _, err := os.Stat(goldenTape); os.IsNotExist(err) {
		t.Skipf("%s not available", goldenTape)
	}

	dict, trained, err := codec.TrainDictionaryFromCaptures(codec.MinPrivateDictionaryID, nil, goldenTape)
	if err != nil {
		t.Fatalf("TrainDictionaryFromCaptures: %v", err)
	}
	if trained == 0 {
		t.Fatal("trained on 0 frames")
	}

	dir := t.TempDir()
	dictPath = filepath.Join(dir, "corpus.dict")
	if err := os.WriteFile(dictPath, dict, 0o600); err != nil {
		t.Fatalf("write dictionary: %v", err)
	}

	src, err := codec.NewReader(goldenTape, codec.WithoutLimits())
	if err != nil {
		t.Fatalf("open golden: %v", err)
	}
	header, err := src.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	var frames []*capturepb.Frame
	for {
		f, err := src.ReadFrame()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		frames = append(frames, f)
	}
	if err := src.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	tapePath = filepath.Join(dir, "dict.tape")
	w, err := codec.NewWriterWithOptions(tapePath, codec.WithDictionary(dict))
	if err != nil {
		t.Fatalf("NewWriterWithOptions: %v", err)
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
	return tapePath, dictPath
}

// TestTapedeckReadsADictionaryTape is F6's read half: every command that opens
// a capture must be able to open one written with a dictionary.
//
// It asserts the REFUSAL first. A capture needing a dictionary must fail loudly
// without one — that is the property the dictionary's whole obligation rests on
// — so a --dict flag that made the error go away by not needing the dictionary
// would be a worse defect than the missing flag.
func TestTapedeckReadsADictionaryTape(t *testing.T) {
	tapePath, dictPath := dictionaryCapture(t)

	// verify is deliberately absent: its argument is an .echoreplay, not a
	// .tape, so it cannot be driven with a capture path. It carries the flag
	// (verify.go) and reaches the same loader, but the assertion below is not
	// the shape that exercises it.
	trimOut := filepath.Join(t.TempDir(), "trimmed.tape")
	for _, tc := range []struct {
		name string
		new  func() *cobra.Command
		args []string
	}{
		{"show", newShowCommand, []string{tapePath}},
		{"stats", newStatsCommand, []string{tapePath}},
		{"trim", newTrimCommand, []string{tapePath, "-o", trimOut}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Without the dictionary: refused, and the message must say why.
			cmd := tc.new()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatal("a dictionary capture opened with NO dictionary and no error; the " +
					"obligation that a reader without the dictionary fails loudly is broken")
			}
			if !strings.Contains(err.Error(), "dictionary") {
				t.Errorf("refusal does not mention the dictionary: %v", err)
			}

			// With it: read.
			var out bytes.Buffer
			cmd = tc.new()
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(append(append([]string{}, tc.args...), "--dict", dictPath))
			if err := cmd.Execute(); err != nil {
				t.Fatalf("%s --dict: %v\n%s", tc.name, err, out.String())
			}
		})
	}
}

// TestTrimPreservesSeekability is F6's write half: a trim of a seekable capture
// must produce a seekable capture. It rewrote whole-stream by construction,
// because trim.go calls codec.NewWriter — which is exactly why the default
// mattering is the fix rather than a special case in trim.
func TestTrimPreservesSeekability(t *testing.T) {
	if _, err := os.Stat(goldenTape); os.IsNotExist(err) {
		t.Skipf("%s not available", goldenTape)
	}
	in, err := codec.OpenBlockIndex(goldenTape)
	if err != nil {
		t.Fatalf("the input capture is not seekable (%v); this test would prove nothing", err)
	}

	out := filepath.Join(t.TempDir(), "trimmed.tape")
	runTrimCmd(t, goldenTape, out, frameTimestampAt(t, goldenTape, 100))

	trimmed, err := codec.OpenBlockIndex(out)
	if err != nil {
		t.Fatalf("trim produced a capture with no seek table (%v): a servable capture stopped "+
			"being servable, with nothing said", err)
	}
	if trimmed.Blocks() < 2 {
		t.Fatalf("trimmed capture has %d block(s); it is not actually blocked", trimmed.Blocks())
	}
	t.Logf("trim: %d blocks in, %d blocks out", in.Blocks(), trimmed.Blocks())
}
