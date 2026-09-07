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
// not define — 900 and 901 — and check all three.
//
// THEY USED TO BE 8 AND 9, AND THAT IS THE WHOLE LESSON OF THIS FILE. This
// comment previously read "8 and 9, the next two free on CaptureHeader", chosen
// because they were the lowest free numbers. On 2026-09-07 nevr-proto defined
// game_type = 8 and producer = 9, and "the lowest free numbers" turned out to
// mean "the numbers the next author will take".
//
// A stand-in field number is only a stand-in while the schema does not define
// it, and the schema belongs to another repository. Picking free-today numbers
// makes this file's premise a fact someone else can falsify with no change here.
// 900-999 is RESERVED on CaptureHeader — protoc refuses it to everyone — so the
// premise is now true by construction rather than by anyone remembering.
//
// THE GUARD FIRED, WHICH IS WHY THIS IS A CORRECTION AND NOT A NEAR MISS.
// requireUndefinedFields exists because the failure mode here is silent: when a
// stand-in number acquires a definition, the wire bytes parse into a typed
// accessor, re-marshal from it, and every assertion below still passes while
// testing nothing. That is FORMS §Audit's superseded-CHECK case, and its sharper
// half — "a stale sentence is inert. A stale green is an active lie." A stale
// test does not go quiet; it goes on emitting green, and green is read as
// coverage by everyone who does not open it.
//
// Measured on the bump, before the numbers moved:
//
//	--- FAIL: TestHeaderCarriesUnknownFieldsWithoutFailing
//	    field 8 is now DEFINED in the pinned schema as "game_type" (string), so
//	    it is no longer an unknown field and this file has stopped testing
//	    unknown-field handling.
//
// BEFORE, at nevr-api v1.36.12-20260826145031-f5ec961c025e.1: fields 8 and 9 both
// undefined, 20 bytes of unknown fields carried, byte-identical through a
// round-trip. AFTER, at v1.36.12-20260907104457-638a4669f605.2: field 8 defined,
// guard red. The before-state was captured while it was still observable, which
// is the only reason the after can be compared to anything.
//
// One asymmetry worth keeping, because it decides why the guard asks the
// DESCRIPTOR rather than the wire: the old field-9 stand-in was a string against
// a string field, so its types matched and it would have vanished into the typed
// accessor — loud, in the sense that unknown bytes drop. The old field-8 stand-in
// was a VARINT against a string field, so the types MISMATCH and its bytes stay
// unknown, preserved byte-exact, with the typed accessor returning "". Field 8
// would have gone on passing for a reason that had nothing to do with the
// property. The number we argued about was 9; the one that would have hurt was 8.

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

// unknownFieldNumbers reports which field numbers appear in a message's
// preserved unknown-field bytes.
//
// It parses the unknown blob rather than the re-marshalled message on purpose:
// once a message is marshalled, a field that came from a TYPED accessor and one
// that came from unknown fields are byte-identical, so the output cannot answer
// "was this preserved as unknown". Only the blob can.
func unknownFieldNumbers(t *testing.T, b []byte) map[protowire.Number]bool {
	t.Helper()
	present := map[protowire.Number]bool{}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			t.Fatalf("unknown-field blob is not valid wire format at %d bytes remaining: %v",
				len(b), protowire.ParseError(n))
		}
		b = b[n:]
		present[num] = true
		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			t.Fatalf("unknown-field blob: bad value for field %d: %v", num, protowire.ParseError(n))
		}
		b = b[n:]
	}
	return present
}

// headerWithFutureFields returns the wire bytes of a CaptureHeader whose known
// fields are set and which additionally carries two fields this build's schema
// does not define: field 900 as a varint, field 901 as a length-delimited string.
// They stand in for game_type and a producer/user-agent string.
func headerWithFutureFields(t *testing.T) []byte {
	t.Helper()
	// Every test in this file goes through here, so the premise is checked once
	// and cannot be skipped by adding a test that forgets to.
	requireUndefinedFields(t, 900, 901)

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
	// Field 900, varint — the shape game_type would have taken.
	out = protowire.AppendTag(out, 900, protowire.VarintType)
	out = protowire.AppendVarint(out, 7)
	// Field 901, length-delimited — the shape a producer/version string takes.
	out = protowire.AppendTag(out, 901, protowire.BytesType)
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
		t.Fatalf("a header carrying undefined fields 900 and 901 failed to parse: %v", err)
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
	// (a DiscardUnknown unmarshaler, a narrower intermediate type).
	//
	// IT IS PER-FIELD, NOT A NON-EMPTY CHECK, AND THE DIFFERENCE IS LOAD-BEARING.
	// An aggregate "did anything reach unknown fields" is defeated by a PARTIAL
	// bump, and the 2026-09-07 bump was exactly that: nevr-proto defined
	// game_type = 8 (string) and producer = 9 (string), and the two old stand-ins
	// behaved differently against them. The string stand-in at 9 matched types and
	// went typed, leaving unknown fields. The varint stand-in at 8 mismatched and
	// its bytes stayed unknown. Measured against a defined string field
	// (capture_id, field 1):
	//
	//	varint at a defined string field -> unknown = 2 bytes (08 07), round-trip
	//	                                    identical, typed accessor returns ""
	//	string at a defined string field -> unknown = 0 bytes, parses typed
	//
	// So the aggregate count would have been 2, not 0 — non-empty, green, and
	// silent about one stand-in having quietly stopped being tested. Asking for
	// each stand-in BY NUMBER is what survives a partial bump, and it is the
	// assertion the fieldVarint/fieldString checks below cannot make: those read
	// the re-marshalled OUTPUT, where a typed field and a preserved unknown field
	// are byte-identical and indistinguishable.
	//
	// The numbers are 900/901 now and reserved, so a partial bump should be
	// impossible. This stays because "should be impossible" is the reasoning that
	// put the stand-ins at 8 and 9 in the first place.
	unknown := round.ProtoReflect().GetUnknown()
	present := unknownFieldNumbers(t, unknown)
	for _, want := range []protowire.Number{900, 901} {
		if !present[want] {
			t.Fatalf("field %d did NOT reach unknown fields (present: %v, %d bytes: % x). "+
				"Either the schema now defines it — in which case this file has stopped "+
				"testing unknown-field preservation for it — or something stripped it "+
				"before it got here. The assertions below cannot tell you which, because "+
				"they read the re-marshalled output where a typed field and a preserved "+
				"unknown field look the same.", want, present, len(unknown), unknown)
		}
	}
	t.Logf("unknown-field bytes carried: %d (% x), fields present: %v",
		len(unknown), unknown, present)

	out, err := proto.Marshal(&round)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	if got := fieldVarint(t, out, 900); got != 7 {
		t.Errorf("undefined field 900 did not survive decode/re-encode: got %d, want 7", got)
	}
	if got := fieldString(t, out, 901); got != "nevr-agent/1.4.2" {
		t.Errorf("undefined field 901 did not survive decode/re-encode: got %q", got)
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
			if v := fieldVarint(t, out, 900); v != 7 {
				t.Errorf("field 900 lost through the container: got %d, want 7", v)
			}
			if s := fieldString(t, out, 901); s != "nevr-agent/1.4.2" {
				t.Errorf("field 901 lost through the container: got %q", s)
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
