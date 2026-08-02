package codec

import (
	"errors"
	"path/filepath"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
)

// testFrame returns a minimal valid frame for the given sequential index: a
// game payload is set so the frame encodes real state, and frame_index matches
// the caller's intent. The Writer rejects nil-payload frames as meaningless.
func testFrame(index uint32) *capturepb.Frame {
	return &capturepb.Frame{
		FrameIndex: index,
		Payload: &capturepb.Frame_EchoArena{
			EchoArena: &capturepb.EchoArenaFrame{},
		},
	}
}

// TestWriter_WriteFrameBeforeHeader_Errors pins R2 (release-audit finding:
// "Writer accepts invalid order and inconsistent indexes"): a frame written
// before the header would produce a stream the reader rejects, so WriteFrame
// must refuse it with ErrWriteOrder.
func TestWriter_WriteFrameBeforeHeader_Errors(t *testing.T) {
	t.Parallel()

	w, err := NewWriter(filepath.Join(t.TempDir(), "nohdr.tape"))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close() //nolint:errcheck // writer was never made usable

	if err := w.WriteFrame(testFrame(0)); !errors.Is(err, ErrWriteOrder) {
		t.Fatalf("WriteFrame before header: want ErrWriteOrder, got %v", err)
	}
}

// TestWriter_DuplicateWriteHeader_Errors pins R2: the format allows exactly one
// CaptureHeader envelope; a second one would make ReadHeader fail on the
// trailing envelope, so WriteHeader must refuse it.
func TestWriter_DuplicateWriteHeader_Errors(t *testing.T) {
	t.Parallel()

	w, err := NewWriter(filepath.Join(t.TempDir(), "duphdr.tape"))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close() //nolint:errcheck // test teardown

	if err := w.WriteHeader(&capturepb.CaptureHeader{}); err != nil {
		t.Fatalf("first WriteHeader: %v", err)
	}
	if err := w.WriteHeader(&capturepb.CaptureHeader{}); !errors.Is(err, ErrWriteOrder) {
		t.Fatalf("duplicate WriteHeader: want ErrWriteOrder, got %v", err)
	}
}

// TestWriter_WriteAfterClose_Errors pins R2: after Close has flushed the footer
// and released the encoder/file, no further WriteFrame or WriteHeader may be
// accepted, and a second Close must not write a second footer.
func TestWriter_WriteAfterClose_Errors(t *testing.T) {
	t.Parallel()

	t.Run("frame after Close", func(t *testing.T) {
		w, err := NewWriter(filepath.Join(t.TempDir(), "afterframe.tape"))
		if err != nil {
			t.Fatal(err)
		}
		if err := w.WriteHeader(&capturepb.CaptureHeader{}); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := w.WriteFrame(testFrame(0)); !errors.Is(err, ErrWriteOrder) {
			t.Fatalf("WriteFrame after Close: want ErrWriteOrder, got %v", err)
		}
	})

	t.Run("header after Close", func(t *testing.T) {
		w, err := NewWriter(filepath.Join(t.TempDir(), "afterhdr.tape"))
		if err != nil {
			t.Fatal(err)
		}
		if err := w.WriteHeader(&capturepb.CaptureHeader{}); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := w.WriteHeader(&capturepb.CaptureHeader{}); !errors.Is(err, ErrWriteOrder) {
			t.Fatalf("WriteHeader after Close: want ErrWriteOrder, got %v", err)
		}
	})

	t.Run("second Close", func(t *testing.T) {
		w, err := NewWriter(filepath.Join(t.TempDir(), "afterclose.tape"))
		if err != nil {
			t.Fatal(err)
		}
		if err := w.WriteHeader(&capturepb.CaptureHeader{}); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); !errors.Is(err, ErrWriteOrder) {
			t.Fatalf("second Close: want ErrWriteOrder, got %v", err)
		}
	})
}

// TestWriter_NilFrame_Errors pins R2 item 4: a nil *Frame is a caller bug and
// would panic on field access, so WriteFrame must refuse it with ErrNilFrame.
func TestWriter_NilFrame_Errors(t *testing.T) {
	t.Parallel()

	w, err := NewWriter(filepath.Join(t.TempDir(), "nilframe.tape"))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close() //nolint:errcheck // test teardown
	if err := w.WriteHeader(&capturepb.CaptureHeader{}); err != nil {
		t.Fatal(err)
	}

	if err := w.WriteFrame(nil); !errors.Is(err, ErrNilFrame) {
		t.Fatalf("WriteFrame(nil): want ErrNilFrame, got %v", err)
	}
}

// TestWriter_EmptyPayloadFrame_Errors pins R2 item 4: a frame whose payload
// oneof is unset encodes no game state at all — it would round-trip as a
// payload-less frame that every consumer treats as absent data. The Writer
// refuses it as meaningless.
func TestWriter_EmptyPayloadFrame_Errors(t *testing.T) {
	t.Parallel()

	w, err := NewWriter(filepath.Join(t.TempDir(), "emptypayload.tape"))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close() //nolint:errcheck // test teardown
	if err := w.WriteHeader(&capturepb.CaptureHeader{}); err != nil {
		t.Fatal(err)
	}

	frame := &capturepb.Frame{FrameIndex: 0} // no Payload set
	if err := w.WriteFrame(frame); !errors.Is(err, ErrEmptyFrame) {
		t.Fatalf("WriteFrame without payload: want ErrEmptyFrame, got %v", err)
	}
}

// TestWriter_NonSequentialFrameIndex_Errors pins R2 item 5 — the core of the
// finding. The footer's keyframe/event indexes are built from the writer's
// stream position, so a caller-supplied frame_index that is not the next
// sequential value would make those indexes disagree with the frames on the
// wire. The Writer rejects any frame whose index skips ahead or repeats.
func TestWriter_NonSequentialFrameIndex_Errors(t *testing.T) {
	t.Parallel()

	w, err := NewWriter(filepath.Join(t.TempDir(), "order.tape"))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close() //nolint:errcheck // test teardown
	if err := w.WriteHeader(&capturepb.CaptureHeader{}); err != nil {
		t.Fatal(err)
	}

	if err := w.WriteFrame(testFrame(0)); err != nil {
		t.Fatalf("first frame: %v", err)
	}
	if err := w.WriteFrame(testFrame(2)); !errors.Is(err, ErrFrameIndexOutOfOrder) {
		t.Fatalf("skipped index: want ErrFrameIndexOutOfOrder, got %v", err)
	}
	if err := w.WriteFrame(testFrame(0)); !errors.Is(err, ErrFrameIndexOutOfOrder) {
		t.Fatalf("repeated index: want ErrFrameIndexOutOfOrder, got %v", err)
	}
}

// TestWriter_ValidSequence_FooterMatchesWire pins the positive contract R2 must
// not break: header, then N sequential frames, then Close still writes a tape
// whose footer keyframe/event indexes reference the same frame indexes the
// frames carry on the wire.
func TestWriter_ValidSequence_FooterMatchesWire(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "valid.tape")
	w, err := NewWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteHeader(&capturepb.CaptureHeader{}); err != nil {
		t.Fatal(err)
	}
	const frameCount = 75
	for i := range uint32(frameCount) {
		f := testFrame(i)
		if i == 50 {
			f.GetEchoArena().Events = []*capturepb.EchoEvent{
				{Event: &capturepb.EchoEvent_GoalScored{GoalScored: &capturepb.GoalScored{}}},
			}
		}
		if err := w.WriteFrame(f); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := NewReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close() //nolint:errcheck // test teardown
	if _, err := r.ReadHeader(); err != nil {
		t.Fatal(err)
	}
	wire := make(map[uint32]struct{}, frameCount)
	for {
		f, err := r.ReadFrame()
		if err != nil {
			break
		}
		wire[f.GetFrameIndex()] = struct{}{}
	}
	footer, err := r.ReadFooter()
	if err != nil {
		t.Fatal(err)
	}

	if got := len(wire); got != frameCount {
		t.Fatalf("read %d distinct wire frames, want %d", got, frameCount)
	}
	for _, kf := range footer.GetKeyframeIndex() {
		if _, ok := wire[kf.GetFrameIndex()]; !ok {
			t.Errorf("keyframe index %d not present on the wire", kf.GetFrameIndex())
		}
	}
	for _, entry := range footer.GetEventIndex() {
		if entry.GetEventType() != capturepb.EventType_EVENT_TYPE_GOAL_SCORED {
			t.Errorf("unexpected event index entry type %v", entry.GetEventType())
		}
		for _, fi := range entry.GetFrameIndices() {
			if fi != 50 {
				t.Errorf("event index references frame %d, want 50", fi)
			}
		}
	}
}
