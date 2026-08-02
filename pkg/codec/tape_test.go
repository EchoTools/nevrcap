package codec

import (
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTapeRoundTrip(t *testing.T) {
	path := t.TempDir() + "/test.tape"

	header := &capturepb.CaptureHeader{
		CaptureId:     "test-capture-001",
		CreatedAt:     timestamppb.Now(),
		FormatVersion: 2,
		Metadata:      map[string]string{"game_version": "1.0"},
		GameHeader: &capturepb.CaptureHeader_EchoArena{
			EchoArena: &capturepb.EchoArenaHeader{
				SessionId: "ABC-123",
				MapName:   "mpl_arena_a",
				MatchType: capturepb.MatchType_MATCH_TYPE_ARENA,
			},
		},
	}

	const frameCount = 250
	frames := make([]*capturepb.Frame, frameCount)
	for i := range uint32(frameCount) {
		frames[i] = &capturepb.Frame{
			FrameIndex:        i,
			TimestampOffsetMs: i * 16, // ~60 Hz
			Payload: &capturepb.Frame_EchoArena{
				EchoArena: &capturepb.EchoArenaFrame{
					GameStatus:   capturepb.GameStatus_GAME_STATUS_PLAYING,
					GameClock:    float32(300) - float32(i)*0.016,
					BluePoints:   int32(i / 100),
					OrangePoints: int32(i / 150),
				},
			},
		}
	}

	// Add an event at frame 50.
	frames[50].GetEchoArena().Events = []*capturepb.EchoEvent{
		{Event: &capturepb.EchoEvent_GoalScored{GoalScored: &capturepb.GoalScored{
			DiscSpeed:   15.5,
			Team:        capturepb.Role_ROLE_BLUE_TEAM,
			GoalType:    capturepb.GoalType_GOAL_TYPE_LONG_SHOT,
			PointAmount: 3,
		}}},
	}

	// Write.
	w, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteHeader(header); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	for _, f := range frames {
		if err := w.WriteFrame(f); err != nil {
			t.Fatalf("WriteFrame %d: %v", f.FrameIndex, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	// Read back.
	r, err := NewReader(path)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	gotHeader, err := r.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if !proto.Equal(gotHeader, header) {
		t.Errorf("header mismatch")
	}

	var readFrames []*capturepb.Frame
	for {
		f, err := r.ReadFrame()
		if err != nil {
			break
		}
		readFrames = append(readFrames, f)
	}

	if len(readFrames) != frameCount {
		t.Fatalf("frame count: got %d, want %d", len(readFrames), frameCount)
	}

	for i, f := range readFrames {
		if !proto.Equal(f, frames[i]) {
			t.Errorf("frame %d mismatch", i)
		}
	}

	// Read footer.
	footer, err := r.ReadFooter()
	if err != nil {
		t.Fatalf("ReadFooter: %v", err)
	}

	if footer.FrameCount != frameCount {
		t.Errorf("footer frame_count: got %d, want %d", footer.FrameCount, frameCount)
	}

	expectedDurationMs := uint32((frameCount - 1) * 16)
	if footer.DurationMs != expectedDurationMs {
		t.Errorf("footer duration_ms: got %d, want %d", footer.DurationMs, expectedDurationMs)
	}

	if footer.FooterOffset == 0 {
		t.Error("footer_offset should be non-zero")
	}

	// Keyframe index: frames 0, 100, 200 (interval=100, 250 frames).
	if len(footer.KeyframeIndex) != 3 {
		t.Errorf("keyframe_index length: got %d, want 3", len(footer.KeyframeIndex))
	} else {
		expectedKeyframes := []uint32{0, 100, 200}
		for i, kf := range footer.KeyframeIndex {
			if kf.FrameIndex != expectedKeyframes[i] {
				t.Errorf("keyframe[%d].frame_index: got %d, want %d", i, kf.FrameIndex, expectedKeyframes[i])
			}
			if kf.ByteOffset == 0 && i > 0 {
				t.Errorf("keyframe[%d].byte_offset should be non-zero", i)
			}
		}
	}

	// Event index: should have one entry for GOAL_SCORED with frame 50.
	if len(footer.EventIndex) != 1 {
		t.Errorf("event_index length: got %d, want 1", len(footer.EventIndex))
	} else {
		entry := footer.EventIndex[0]
		if entry.EventType != capturepb.EventType_EVENT_TYPE_GOAL_SCORED {
			t.Errorf("event_index[0].event_type: got %v, want GOAL_SCORED", entry.EventType)
		}
		if len(entry.FrameIndices) != 1 || entry.FrameIndices[0] != 50 {
			t.Errorf("event_index[0].frame_indices: got %v, want [50]", entry.FrameIndices)
		}
	}
}

func TestTapeEmptyCapture(t *testing.T) {
	path := t.TempDir() + "/empty.tape"

	header := &capturepb.CaptureHeader{
		CaptureId:     "empty-capture",
		CreatedAt:     timestamppb.Now(),
		FormatVersion: 2,
	}

	w, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteHeader(header); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	r, err := NewReader(path)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	gotHeader, err := r.ReadHeader()
	if err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	if gotHeader.CaptureId != "empty-capture" {
		t.Errorf("capture_id: got %q, want %q", gotHeader.CaptureId, "empty-capture")
	}

	// No frames.
	_, err = r.ReadFrame()
	if err == nil {
		t.Fatal("expected EOF from ReadFrame on empty capture")
	}

	footer, err := r.ReadFooter()
	if err != nil {
		t.Fatalf("ReadFooter: %v", err)
	}
	if footer.FrameCount != 0 {
		t.Errorf("frame_count: got %d, want 0", footer.FrameCount)
	}
}

func TestTapeCustomKeyframeInterval(t *testing.T) {
	path := t.TempDir() + "/keyframe.tape"

	w, err := NewWriterWithKeyframeInterval(path, 10)
	if err != nil {
		t.Fatalf("NewWriterWithKeyframeInterval: %v", err)
	}
	if err := w.WriteHeader(&capturepb.CaptureHeader{
		CaptureId:     "kf-test",
		FormatVersion: 2,
	}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}

	for i := range uint32(25) {
		if err := w.WriteFrame(&capturepb.Frame{
			FrameIndex:        i,
			TimestampOffsetMs: i * 16,
			Payload: &capturepb.Frame_EchoArena{
				EchoArena: &capturepb.EchoArenaFrame{},
			},
		}); err != nil {
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
	defer r.Close()

	if _, err := r.ReadHeader(); err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	for {
		if _, err := r.ReadFrame(); err != nil {
			break
		}
	}

	footer, err := r.ReadFooter()
	if err != nil {
		t.Fatalf("ReadFooter: %v", err)
	}

	// Keyframes at 0, 10, 20 (interval=10, 25 frames).
	if len(footer.KeyframeIndex) != 3 {
		t.Errorf("keyframe_index length: got %d, want 3", len(footer.KeyframeIndex))
	}
}
