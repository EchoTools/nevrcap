package conversion

import (
	"testing"
	"time"

	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestMapFrame_NegativeOffsetClampedToZero(t *testing.T) {
	baseTime := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	// Frame timestamp is 10 seconds BEFORE baseTime.
	frameTime := baseTime.Add(-10 * time.Second)

	v1f := &telemetryv1.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(frameTime),
	}

	v2f := MapFrame(v1f, baseTime)
	if v2f == nil {
		t.Fatal("MapFrame returned nil")
	}

	// Without the fix, this would wrap around to a huge uint32 value.
	if v2f.TimestampOffsetMs != 0 {
		t.Errorf("TimestampOffsetMs = %d, want 0 (negative offset should clamp)", v2f.TimestampOffsetMs)
	}
}

func TestMapFrame_PositiveOffsetUnchanged(t *testing.T) {
	baseTime := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	// Frame timestamp is 5 seconds AFTER baseTime.
	frameTime := baseTime.Add(5 * time.Second)

	v1f := &telemetryv1.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(frameTime),
	}

	v2f := MapFrame(v1f, baseTime)
	if v2f == nil {
		t.Fatal("MapFrame returned nil")
	}

	if v2f.TimestampOffsetMs != 5000 {
		t.Errorf("TimestampOffsetMs = %d, want 5000", v2f.TimestampOffsetMs)
	}
}
