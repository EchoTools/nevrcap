package nevrcap

import (
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// FrameProcessor handles high-performance processing of game frames
// optimized for up to 600 Hz operation
type FrameProcessor struct {
	frameIndex    uint32
	eventDetector *EventDetector
	unmarshaler   *protojson.UnmarshalOptions
}

// NewFrameProcessor creates a new optimized frame processor
func NewFrameProcessor() *FrameProcessor {

	return &FrameProcessor{
		frameIndex:    0,
		eventDetector: NewEventDetector(),
		unmarshaler: &protojson.UnmarshalOptions{
			AllowPartial: true,
		},
	}
}

// ProcessFrame takes raw session and user bones data and processes it into a rtapi.LobbySessionStateFrame
// This is optimized for high-frequency invocation (up to 600 Hz)
func (fp *FrameProcessor) ProcessFrame(sessionResponseData, userBonesData []byte, timestamp time.Time) (*rtapi.LobbySessionStateFrame, error) {
	// Reset the pre-allocated structs to avoid allocations
	// Pre-allocated structs to avoid memory allocations
	sessionResponse := &apigame.SessionResponse{}
	bonesResponse := &apigame.PlayerBonesResponse{}

	// Parse session data
	if err := fp.unmarshaler.Unmarshal(sessionResponseData, sessionResponse); err != nil {
		return nil, err
	}

	// Parse user bones data (if provided)
	if len(userBonesData) > 0 {
		if err := fp.unmarshaler.Unmarshal(userBonesData, bonesResponse); err != nil {
			return nil, err
		}
	}

	// Create the frame
	frame := &rtapi.LobbySessionStateFrame{
		FrameIndex:  fp.frameIndex,
		Timestamp:   timestamppb.New(timestamp),
		Session:     sessionResponse,
		PlayerBones: bonesResponse,
	}

	// Add frame to event detector and get any detected events
	frame.Events = fp.eventDetector.AddFrame(frame)
	fp.frameIndex++

	return frame, nil
}

// Reset clears the processor state
func (fp *FrameProcessor) Reset() {
	fp.frameIndex = 0
	fp.eventDetector.Reset()
}
