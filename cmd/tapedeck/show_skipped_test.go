package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
)

// F9's counter has to reach a human or it is not a counter. These two tests are
// the operator half: one proves the line appears when a capture carries envelope
// kinds this binary does not know, the other proves it stays QUIET otherwise —
// because a line that prints on every healthy file is one operators learn to
// ignore, and then they ignore the one that matters.

// unknownEnvelope is field 99 (not in the oneof: header=1, frame=2, footer=3),
// length-delimited: tag 99<<3|2 = 794 -> 0x9A 0x06, then a 3-byte payload.
var unknownEnvelope = []byte{0x06, 0x9A, 0x06, 0x03, 'a', 'b', 'c'}

func writeCaptureWithParts(t *testing.T, path string, parts ...[]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := zstd.NewWriter(f, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range parts {
		if _, err := enc.Write(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func frameEnvelope(t *testing.T, e *capturepb.Envelope) []byte {
	t.Helper()
	data, err := proto.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var buf [10]byte
	l := uint64(len(data))
	i := 0
	for l >= 0x80 {
		buf[i] = byte(l) | 0x80
		l >>= 7
		i++
	}
	buf[i] = byte(l)
	i++
	return append(append([]byte(nil), buf[:i]...), data...)
}

func showOutput(t *testing.T, path string) string {
	t.Helper()
	var out bytes.Buffer
	cmd := newShowCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("show %s: %v", filepath.Base(path), err)
	}
	return out.String()
}

func captureParts(t *testing.T, withUnknown bool) [][]byte {
	t.Helper()
	parts := [][]byte{
		frameEnvelope(t, &capturepb.Envelope{
			Message: &capturepb.Envelope_Header{Header: &capturepb.CaptureHeader{FormatVersion: 2}},
		}),
		frameEnvelope(t, &capturepb.Envelope{
			Message: &capturepb.Envelope_Frame{Frame: &capturepb.Frame{
				FrameIndex: 0,
				Payload: &capturepb.Frame_EchoArena{
					EchoArena: &capturepb.EchoArenaFrame{GameClock: 1},
				},
			}},
		}),
	}
	if withUnknown {
		parts = append(parts, unknownEnvelope)
	}
	return append(parts, frameEnvelope(t, &capturepb.Envelope{
		Message: &capturepb.Envelope_Footer{Footer: &capturepb.CaptureFooter{FrameCount: 1}},
	}))
}

func TestShowReportsSkippedEnvelopes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.tape")
	writeCaptureWithParts(t, path, captureParts(t, true)...)

	got := showOutput(t, path)
	if !strings.Contains(got, "skipped_envelopes: 1") {
		t.Errorf("show output does not report the skipped envelope.\n"+
			"A reader that silently does not see part of a capture is the defect "+
			"AGENTS.md §4 names; the counter must reach an operator.\ngot:\n%s", got)
	}
	if !strings.Contains(got, "frames: 1") {
		t.Errorf("show should still report the frames it DID read; got:\n%s", got)
	}
	t.Logf("show output:\n%s", got)
}

func TestShowIsQuietWhenNothingWasSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "normal.tape")
	writeCaptureWithParts(t, path, captureParts(t, false)...)

	got := showOutput(t, path)
	if strings.Contains(got, "skipped_envelopes") {
		t.Errorf("show printed a skipped_envelopes line for a capture with nothing "+
			"skipped. A gate that cries wolf trains dismissal of its own true "+
			"fires.\ngot:\n%s", got)
	}
	t.Logf("QUIET show output:\n%s", got)
}
