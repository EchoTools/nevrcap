package codec

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"google.golang.org/protobuf/proto"
)

// Corruption, detected rather than served.
//
// WHY THIS FILE EXISTS. A bug hunt on 2026-09-05 (F1,
// spritz/findings/2026-09-05-tape-bughunt-fable.md) measured the shipped reader
// against every single-bit flip of a 7,553-byte capture:
//
//	error            25098
//	clean-identical    241
//	SILENT-WRONG     35085   <- clean read, wrong frame content, no error
//	flips inside the 4 checksum bytes: 32, of which reported an error: 0
//
// 58% of flips read back as a clean capture with wrong telemetry. zstd writes a
// content checksum on every frame and the reader never reached it: ReadFrame
// returns io.EOF the instant it sees the footer envelope, and klauspost's
// streaming decoder verifies the checksum only when the frame's end is
// consumed. The whole-stream layout is ONE frame, so the check was never
// reached for any capture ever written.
//
// limits.go already says what that means, about tape's own trailer, in words
// that apply exactly: "A reader that ignores its own trailer reports success on
// a file that lost data, which is the one failure this library must never
// have." The trailer being ignored was zstd's.
//
// WHY A SWEEP AND NOT ONE FIXTURE. A single committed corrupt file proves one
// byte position and silently stops covering the rest the moment the corpus,
// the compressor or the layout moves. The property is universal — NO single-bit
// flip anywhere may produce a clean read of wrong content — so the test is
// universal over the file, and it re-derives its own corpus every run. It also
// cannot pass vacuously: it asserts a floor on how many flips it actually
// exercised and how many the reader rejected, so a corpus that stopped being
// corruptible fails here instead of going quiet.

// corruptionCorpus builds a float-heavy capture and returns its path and the
// frames that went into it. Floats are the point: at SpeedFastest most of a
// real telemetry frame is raw literals, and a flipped literal byte is a
// different valid float inside a valid protobuf — which is why the silent-wrong
// rate is 58% and not 5%.
func corruptionCorpus(t *testing.T, name string, opts ...WriterOption) (string, []*capturepb.Frame) {
	t.Helper()
	frames := blockTestFrames(60)
	path := filepath.Join(t.TempDir(), name)
	w, err := NewWriterWithOptions(path, append([]WriterOption{WithKeyframeInterval(10)}, opts...)...)
	if err != nil {
		t.Fatalf("NewWriterWithOptions: %v", err)
	}
	if err := w.WriteHeader(blockTestHeader()); err != nil {
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
	return path, frames
}

// readWholeCapture reads a capture end to end the way every consumer in this
// repo does — header, frames to io.EOF, footer, Close — and returns the frames
// together with the first error any of those steps produced. A nil error is the
// reader saying "this file is intact", which is the claim under test.
func readWholeCapture(t *testing.T, path string) ([]*capturepb.Frame, error) {
	t.Helper()
	r, err := NewReader(path)
	if err != nil {
		return nil, err
	}
	var out []*capturepb.Frame
	readErr := func() error {
		if _, err := r.ReadHeader(); err != nil {
			return err
		}
		for {
			f, err := r.ReadFrame()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			out = append(out, f)
		}
		if _, err := r.ReadFooter(); err != nil {
			return err
		}
		return nil
	}()
	if closeErr := r.Close(); readErr == nil {
		readErr = closeErr
	}
	return out, readErr
}

// TestNoBitFlipIsSilentlyAccepted is F1 in assertion form: over every
// single-bit flip of a real capture, the reader must never return a clean read
// of content that is not what was written.
func TestNoBitFlipIsSilentlyAccepted(t *testing.T) {
	for _, layout := range []struct {
		name string
		opts []WriterOption
	}{
		{"default-perblock", nil},
		{"whole-stream", []WriterOption{WithWholeStreamCompression()}},
	} {
		t.Run(layout.name, func(t *testing.T) {
			path, truth := corruptionCorpus(t, "corpus.tape", layout.opts...)
			clean, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read corpus: %v", err)
			}
			// The corpus must read cleanly BEFORE anything is flipped.
			// Otherwise every "rejected" below is the reader rejecting a file
			// that was never valid, and the sweep proves nothing.
			if got, err := readWholeCapture(t, path); err != nil {
				t.Fatalf("the unmodified corpus does not read cleanly: %v", err)
			} else if len(got) != len(truth) {
				t.Fatalf("the unmodified corpus read back %d frames, wrote %d", len(got), len(truth))
			}

			dir := t.TempDir()
			target := filepath.Join(dir, "flipped.tape")

			var rejected, silentWrong, cleanIdentical, exercised int
			var firstSilent string
			for off := range clean {
				for _, bit := range []uint{0, 3, 7} {
					damaged := bytes.Clone(clean)
					damaged[off] ^= 1 << bit
					if err := os.WriteFile(target, damaged, 0o600); err != nil {
						t.Fatalf("write flipped: %v", err)
					}
					exercised++

					got, err := readWholeCapture(t, target)
					if err != nil {
						rejected++
						continue
					}
					// A clean read. Every frame must then be exactly what was
					// written; anything else is silent corruption.
					identical := len(got) == len(truth)
					if identical {
						for i := range got {
							if !proto.Equal(truth[i], got[i]) {
								identical = false
								break
							}
						}
					}
					if identical {
						cleanIdentical++
						continue
					}
					silentWrong++
					if firstSilent == "" {
						firstSilent = describeSilentRead(truth, got, off, bit)
					}
				}
			}

			if silentWrong > 0 {
				t.Errorf("SILENT CORRUPTION: %d of %d single-bit flips read back as a clean "+
					"capture with content that is not what was written (F1). The reader must "+
					"verify zstd's content checksum.\n  first: %s",
					silentWrong, exercised, firstSilent)
			}

			// Guard the guard. A sweep that exercised nothing, or that rejected
			// nothing, would report zero silent reads and look identical to a
			// pass.
			if exercised < 3*len(clean) {
				t.Fatalf("the sweep exercised %d flips over a %d-byte file; it did not run",
					exercised, len(clean))
			}
			if rejected == 0 {
				t.Fatalf("the reader rejected NONE of %d flips; it is not reading the damaged "+
					"file at all", exercised)
			}
			t.Logf("%d flips over %d bytes: %d rejected, %d clean-and-identical, %d SILENT-WRONG",
				exercised, len(clean), rejected, cleanIdentical, silentWrong)
		})
	}
}

// TestCorruptedContentChecksumIsRejected pins the sharpest case in the F1
// measurement: all 32 flips inside the 4 checksum bytes were accepted. Those
// leave the payload intact, so a content comparison cannot catch them — the
// only thing that can is actually verifying the checksum, which makes this the
// one assertion that fails for exactly one reason.
func TestCorruptedContentChecksumIsRejected(t *testing.T) {
	path, _ := corruptionCorpus(t, "corpus.tape", WithWholeStreamCompression())
	clean, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	if _, err := readWholeCapture(t, path); err != nil {
		t.Fatalf("the unmodified corpus does not read cleanly: %v", err)
	}

	// In the whole-stream layout the file is exactly one zstd frame, so the
	// last four bytes are that frame's content checksum (klauspost writes one
	// by default). Corrupting only those changes no payload byte at all.
	damaged := bytes.Clone(clean)
	for i := len(damaged) - 4; i < len(damaged); i++ {
		damaged[i] ^= 0xFF
	}
	target := filepath.Join(t.TempDir(), "bad-checksum.tape")
	if err := os.WriteFile(target, damaged, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := readWholeCapture(t, target); err == nil {
		t.Error("a capture whose zstd content checksum was overwritten read back with NO " +
			"error. The checksum is written on every frame and was never verified (F1): the " +
			"reader stops at the footer envelope and never consumes the end of the zstd frame.")
	}
}

// describeSilentRead names the first frame that came back wrong, so a failure
// says what was served rather than only that something was.
func describeSilentRead(truth, got []*capturepb.Frame, off int, bit uint) string {
	if len(got) != len(truth) {
		return "byte " + itoa(off) + " bit " + itoa(int(bit)) + ": read " +
			itoa(len(got)) + " frames, wrote " + itoa(len(truth))
	}
	for i := range got {
		if !proto.Equal(truth[i], got[i]) {
			return "byte " + itoa(off) + " bit " + itoa(int(bit)) + ": frame " + itoa(i) +
				"\n    wrote " + truth[i].String() + "\n    read  " + got[i].String()
		}
	}
	return "byte " + itoa(off) + " bit " + itoa(int(bit)) + ": (no differing frame found)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
