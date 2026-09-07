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
	"google.golang.org/protobuf/reflect/protoreflect"
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
//
// AND THE PREMISE IS NOW ASSERTED RATHER THAN ASSUMED, because it is the kind
// that goes false silently. "Field 8 and field 9 are undefined" was true when
// this file was written and is a fact about a SCHEMA THIS REPOSITORY DOES NOT
// OWN. The day nevr-proto defines either one, the wire bytes below stop being
// unknown fields: they parse into a typed accessor, re-marshal from it, and
// every assertion here still passes — while testing nothing it was written to
// test.
//
// That is FORMS §Audit's superseded-CHECK case exactly, and it is the sharper
// half of it: "a stale sentence is inert; a stale green is an active lie." A
// stale test does not go quiet, it goes on emitting green, and green is read as
// coverage by everyone who does not open it.
//
// requireUndefinedFields is the mechanism that makes that impossible. It fails
// LOUDLY the moment a stand-in number acquires a definition, so the failure mode
// becomes a red build with an explanation instead of a test that quietly retires
// itself. Measured 2026-09-07 at
// nevr-api v1.36.12-20260826145031-f5ec961c025e.1: fields 8 and 9 are both still
// undefined, the header carries 20 bytes of unknown fields, and they survive a
// decode/re-encode byte-identically — so the guard is silent today and this
// comment describes a live test, not an aspiration.
//
// nevr-proto is separately reserving 900-999 on CaptureHeader so a stand-in can
// be undefined FOREVER rather than undefined today; when that lands and tape's
// dependency is bumped, these numbers move there in the same change, and this
// guard is what will prove the move was needed.

// requireUndefinedFields fails unless every listed field number is UNDEFINED in
// the pinned CaptureHeader schema.
//
// It is the guard that keeps this file honest. A stand-in field number is only a
// stand-in while the schema does not define it, and the schema belongs to
// nevr-proto — so the premise can be falsified by a repository this one does not
// control, with no change here and no failing test. Asserting it converts that
// from an invisible retirement into a build failure that says what happened.
//
// It uses the descriptor rather than a hardcoded list, so it answers about the
// schema actually linked into this build. That matters: the question is not
// "what did the .proto say when someone last looked", it is "what does the
// generated type this test just used believe".
func requireUndefinedFields(t *testing.T, numbers ...protowire.Number) {
	t.Helper()
	md := (&capturepb.CaptureHeader{}).ProtoReflect().Descriptor()
	for _, n := range numbers {
		fd := md.Fields().ByNumber(protoreflect.FieldNumber(n))
		if fd == nil {
			continue
		}
		t.Fatalf("field %d is now DEFINED in the pinned schema as %q (%s), so it is no "+
			"longer an unknown field and this file has stopped testing unknown-field "+
			"handling. The wire bytes below would parse into a typed accessor and every "+
			"assertion would still pass. Move the stand-ins to numbers nevr-proto has "+
			"reserved (CaptureHeader 900-999) and re-check that the unknown-field byte "+
			"count is non-zero.", n, fd.Name(), fd.Kind())
	}
}

// headerWithFutureFields returns the wire bytes of a CaptureHeader whose known
// fields are set and which additionally carries two fields this build's schema
// does not define: field 8 as a varint, field 9 as a length-delimited string.
// They stand in for game_type and a producer/user-agent string.
func headerWithFutureFields(t *testing.T) []byte {
	t.Helper()
	// Every test in this file goes through here, so the premise is checked once
	// and cannot be skipped by adding a test that forgets to.
	requireUndefinedFields(t, 8, 9)

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

	// The property itself, asserted directly rather than inferred from the
	// round-trip succeeding. requireUndefinedFields checks the SCHEMA; this
	// checks the parse that actually happened, and the two fail for different
	// reasons — a field could be undefined and still not reach unknown fields
	// (a DiscardUnknown unmarshaler, a narrower intermediate type). A zero here
	// with every other assertion green is the exact signature of a test that has
	// stopped testing.
	unknown := round.ProtoReflect().GetUnknown()
	if len(unknown) == 0 {
		t.Fatalf("the header carries NO unknown fields after parsing, so nothing below " +
			"exercises unknown-field preservation. Expected the bytes for the two " +
			"stand-in fields; got none. Measured 2026-09-07 at this pin: 20 bytes.")
	}
	t.Logf("unknown-field bytes carried: %d (% x)", len(unknown), unknown)

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
