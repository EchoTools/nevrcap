package codec

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Is the tape header extensible, or fixed-layout?
//
// The question is not academic: `game_type` is a proposed header field
// (transcode spec F5) and a "what wrote this tape, and what version" field is
// expected after it. If the header were fixed-layout, that would be two on-disk
// format breaks instead of zero.
//
// CaptureHeader is a proto3 message, so the answer SHOULD be "extensible" — but
// "should, per the spec" is how a format acquires a defect nobody tested for.
// What actually has to hold is three things, and only the first is obvious:
//
//  1. A reader that does not know a field must not FAIL on it.
//  2. It must not lose the fields it does know.
//  3. It must PRESERVE the unknown field through a decode/re-encode cycle —
//     otherwise an old tool that reads and rewrites a tape silently strips new
//     fields, which is a data-loss bug that looks like nothing at all.
//
// These tests construct a header carrying field numbers the pinned schema does
// not define (8 and 9, the next two free on CaptureHeader — 1-7 are used and 10
// opens the game_header oneof) and check all three.

// headerWithFutureFields returns the wire bytes of a CaptureHeader whose known
// fields are set and which additionally carries two fields this build's schema
// does not define: field 8 as a varint, field 9 as a length-delimited string.
// They stand in for game_type and a producer/user-agent string.
func headerWithFutureFields(t *testing.T) []byte {
	t.Helper()

	known, err := proto.Marshal(&capturepb.CaptureHeader{
		CaptureId:     "extensibility-001",
		CreatedAt:     timestamppb.New(fixedEpoch()),
		FormatVersion: 2,
		Metadata:      map[string]string{"k": "v"},
		GameHeader: &capturepb.CaptureHeader_EchoArena{
			EchoArena: &capturepb.EchoArenaHeader{
				SessionId: "EXT-1",
				MapName:   "mpl_arena_a",
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	out := bytes.Clone(known)
	// Field 8, varint — the shape game_type (an enum) would take.
	out = protowire.AppendTag(out, 8, protowire.VarintType)
	out = protowire.AppendVarint(out, 7)
	// Field 9, length-delimited — the shape a producer/version string takes.
	out = protowire.AppendTag(out, 9, protowire.BytesType)
	out = protowire.AppendString(out, "nevr-agent/1.4.2")
	return out
}

// TestHeaderCarriesUnknownFieldsWithoutFailing answers the first two
// obligations: an unknown field is not an error, and it does not disturb the
// fields the reader does know.
func TestHeaderCarriesUnknownFieldsWithoutFailing(t *testing.T) {
	raw := headerWithFutureFields(t)

	var got capturepb.CaptureHeader
	if err := proto.Unmarshal(raw, &got); err != nil {
		t.Fatalf("a header carrying undefined fields 8 and 9 failed to parse: %v", err)
	}
	if got.GetCaptureId() != "extensibility-001" {
		t.Errorf("capture_id = %q, want %q", got.GetCaptureId(), "extensibility-001")
	}
	if got.GetFormatVersion() != 2 {
		t.Errorf("format_version = %d, want 2", got.GetFormatVersion())
	}
	if got.GetEchoArena().GetSessionId() != "EXT-1" {
		t.Errorf("game_header oneof lost: session_id = %q", got.GetEchoArena().GetSessionId())
	}
	if got.GetMetadata()["k"] != "v" {
		t.Errorf("metadata lost: %v", got.GetMetadata())
	}
}

// TestHeaderPreservesUnknownFieldsThroughRewrite is the obligation that
// actually decides whether adding header fields is safe over time.
//
// A tool built today will read tapes written tomorrow. If it drops the fields
// it does not understand when it rewrites one — a trim, a transcode, a
// re-index — then every such pass silently destroys data and nothing reports
// it. Preservation is what makes "additive" mean additive.
func TestHeaderPreservesUnknownFieldsThroughRewrite(t *testing.T) {
	raw := headerWithFutureFields(t)

	var round capturepb.CaptureHeader
	if err := proto.Unmarshal(raw, &round); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	out, err := proto.Marshal(&round)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if got := fieldVarint(t, out, 8); got != 7 {
		t.Errorf("undefined field 8 did not survive decode/re-encode: got %d, want 7", got)
	}
	if got := fieldString(t, out, 9); got != "nevr-agent/1.4.2" {
		t.Errorf("undefined field 9 did not survive decode/re-encode: got %q", got)
	}
}

// TestWriterAndReaderCarryUnknownHeaderFields runs the same question through
// the actual container rather than through proto in isolation: a real capture
// whose header carries undefined fields must be written, read, and have those
// fields still present.
//
// This is where a container-level defect would show up — a writer that
// re-marshals a header through a narrower type, or a reader that validates
// fields it does not know.
func TestWriterAndReaderCarryUnknownHeaderFields(t *testing.T) {
	var header capturepb.CaptureHeader
	if err := proto.Unmarshal(headerWithFutureFields(t), &header); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	for _, tc := range []struct {
		name string
		opts []WriterOption
	}{
		{"whole-stream", nil},
		{"per-block", []WriterOption{WithKeyframeInterval(30), WithPerBlockCompression()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir() + "/ext.tape"
			w, err := NewWriterWithOptions(path, tc.opts...)
			if err != nil {
				t.Fatalf("NewWriterWithOptions: %v", err)
			}
			if err := w.WriteHeader(&header); err != nil {
				t.Fatalf("WriteHeader: %v", err)
			}
			for _, f := range blockTestFrames(60) {
				if err := w.WriteFrame(f); err != nil {
					t.Fatalf("WriteFrame: %v", err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			r, err := NewReader(path)
			if err != nil {
				t.Fatalf("NewReader: %v", err)
			}
			defer r.Close() //nolint:errcheck // read-only

			got, err := r.ReadHeader()
			if err != nil {
				t.Fatalf("ReadHeader on a capture with undefined header fields: %v", err)
			}
			if got.GetCaptureId() != "extensibility-001" {
				t.Errorf("capture_id = %q", got.GetCaptureId())
			}

			out, err := proto.Marshal(got)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if v := fieldVarint(t, out, 8); v != 7 {
				t.Errorf("field 8 lost through the container: got %d, want 7", v)
			}
			if s := fieldString(t, out, 9); s != "nevr-agent/1.4.2" {
				t.Errorf("field 9 lost through the container: got %q", s)
			}

			// The frames must still be intact — an unknown header field is not
			// allowed to disturb anything downstream of it.
			n := 0
			for {
				if _, err := r.ReadFrame(); errors.Is(err, io.EOF) {
					break
				} else if err != nil {
					t.Fatalf("ReadFrame: %v", err)
				}
				n++
			}
			if n != 60 {
				t.Errorf("read %d frames, wrote 60", n)
			}
		})
	}
}

// fieldVarint returns the varint value of the given field number in a
// marshalled message, or fails.
func fieldVarint(t *testing.T, msg []byte, want protowire.Number) uint64 {
	t.Helper()
	for len(msg) > 0 {
		num, typ, n := protowire.ConsumeTag(msg)
		if n < 0 {
			t.Fatalf("malformed message: %v", protowire.ParseError(n))
		}
		msg = msg[n:]
		if num == want && typ == protowire.VarintType {
			v, n := protowire.ConsumeVarint(msg)
			if n < 0 {
				t.Fatalf("field %d: %v", want, protowire.ParseError(n))
			}
			return v
		}
		n = protowire.ConsumeFieldValue(num, typ, msg)
		if n < 0 {
			t.Fatalf("field %d: %v", num, protowire.ParseError(n))
		}
		msg = msg[n:]
	}
	t.Fatalf("field %d not present", want)
	return 0
}

// fieldString returns the string value of the given field number, or fails.
func fieldString(t *testing.T, msg []byte, want protowire.Number) string {
	t.Helper()
	for len(msg) > 0 {
		num, typ, n := protowire.ConsumeTag(msg)
		if n < 0 {
			t.Fatalf("malformed message: %v", protowire.ParseError(n))
		}
		msg = msg[n:]
		if num == want && typ == protowire.BytesType {
			v, n := protowire.ConsumeString(msg)
			if n < 0 {
				t.Fatalf("field %d: %v", want, protowire.ParseError(n))
			}
			return v
		}
		n = protowire.ConsumeFieldValue(num, typ, msg)
		if n < 0 {
			t.Fatalf("field %d: %v", num, protowire.ParseError(n))
		}
		msg = msg[n:]
	}
	t.Fatalf("field %d not present", want)
	return ""
}

// fixedEpoch keeps generated bytes deterministic.
func fixedEpoch() time.Time { return time.Unix(1756600000, 0).UTC() }
