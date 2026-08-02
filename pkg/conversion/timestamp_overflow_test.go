package conversion

import (
	"errors"
	"io"
	"math"
	"path/filepath"
	"testing"
	"time"

	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestMapFrame_TimestampOffsetAtMaxUint32 pins the upper representable boundary
// of the v2 TimestampOffsetMs uint32 delta (release-audit finding R5): an offset
// of exactly math.MaxUint32 milliseconds (~49.7 days) maps normally.
func TestMapFrame_TimestampOffsetAtMaxUint32(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	frameTime := baseTime.Add(time.Duration(math.MaxUint32) * time.Millisecond)

	v1f := &telemetryv1.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(frameTime),
	}

	got, err := MapFrame(v1f, baseTime)
	if err != nil {
		t.Fatalf("MapFrame at MaxUint32 offset: %v", err)
	}
	if got.TimestampOffsetMs != math.MaxUint32 {
		t.Errorf("TimestampOffsetMs = %d, want %d", got.TimestampOffsetMs, uint64(math.MaxUint32))
	}
}

// TestMapFrame_TimestampOffsetOverflowErrors proves R5: an offset one
// millisecond past math.MaxUint32 must return an error rather than wrap around
// to an unrelated offset. This is the assertion that was RED before the fix —
// the cast wrapped silently (a 2100-era timestamp became an 8.69-day offset).
func TestMapFrame_TimestampOffsetOverflowErrors(t *testing.T) {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	frameTime := baseTime.Add(time.Duration(math.MaxUint32+1) * time.Millisecond)

	v1f := &telemetryv1.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(frameTime),
	}

	got, err := MapFrame(v1f, baseTime)
	if !errors.Is(err, ErrTimestampOffsetOverflow) {
		t.Fatalf("MapFrame at MaxUint32+1: want ErrTimestampOffsetOverflow, got frame=%v err=%v",
			got, err)
	}
	if got != nil {
		t.Errorf("MapFrame at MaxUint32+1 returned a frame (offset %d); overflow must not map",
			got.TimestampOffsetMs)
	}
}

// stubV1Reader feeds the conversion pipeline a header and one frame, letting a
// test drive convertFromV1Reader without touching disk formats.
type stubV1Reader struct {
	header *telemetryv1.TelemetryHeader
	frame  *telemetryv1.LobbySessionStateFrame
}

func (s *stubV1Reader) ReadHeader() (*telemetryv1.TelemetryHeader, error) {
	return s.header, nil
}

func (s *stubV1Reader) ReadFrame() (*telemetryv1.LobbySessionStateFrame, error) {
	if s.frame == nil {
		return nil, io.EOF
	}
	f := s.frame
	s.frame = nil
	return f, nil
}

func (s *stubV1Reader) Close() error { return nil }

// TestConvertTimestampOffsetOverflowSurfaces proves R5 is not silent at the
// conversion boundary: convertFromV1Reader returns an error wrapping
// ErrTimestampOffsetOverflow instead of writing a tape with wrapped offsets.
// The stub header's created_at (2000) with a 2100-era frame timestamp puts the
// offset past math.MaxUint32 — the exact shape of the measured finding.
func TestConvertTimestampOffsetOverflowSurfaces(t *testing.T) {
	reader := &stubV1Reader{
		header: &telemetryv1.TelemetryHeader{
			CreatedAt: timestamppb.New(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
		frame: &telemetryv1.LobbySessionStateFrame{
			FrameIndex: 0,
			Timestamp:  timestamppb.New(time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
	}

	out := filepath.Join(t.TempDir(), "overflow.tape")
	_, err := convertFromV1Reader(reader, "in.echoreplay", out)
	if !errors.Is(err, ErrTimestampOffsetOverflow) {
		t.Fatalf("convertFromV1Reader: want ErrTimestampOffsetOverflow, got %v", err)
	}
}
