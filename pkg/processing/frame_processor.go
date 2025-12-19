package processing

import (
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevrcap/v3/pkg/events"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Processor handles high-performance processing of game frames
// optimized for up to 600 Hz operation
type Processor struct {
	frameIndex    uint32
	eventDetector events.Detector
	unmarshaler   *protojson.UnmarshalOptions
}

// New creates a new optimized frame processor
func New() *Processor {
	return NewWithDetector(events.New())
}

// NewWithDetector allows callers to supply a custom Detector implementation.
func NewWithDetector(det events.Detector) *Processor {
	if det == nil {
		det = events.New()
	}

	return &Processor{
		frameIndex:    0,
		eventDetector: det,
		unmarshaler: &protojson.UnmarshalOptions{
			AllowPartial: true,
		},
	}
}

// ProcessAndDetectEvents takes raw session and user bones data, unmarshals it, and sends it through the event detector
// This is optimized for high-frequency invocation (up to 600 Hz)
// Note: Events are now processed asynchronously and can be received via EventDetector.EventsChan()
func (fp *Processor) ProcessAndDetectEvents(sessionResponseData, userBonesData []byte, timestamp time.Time) (*telemetry.LobbySessionStateFrame, error) {
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
	frame := &telemetry.LobbySessionStateFrame{
		FrameIndex:  fp.frameIndex,
		Timestamp:   timestamppb.New(timestamp),
		Session:     sessionResponse,
		PlayerBones: bonesResponse,
	}

	// Send frame to event detector for async processing
	fp.eventDetector.ProcessFrame(frame)
	fp.frameIndex++

	return frame, nil
}

// DetectEvents queues a frame for event detection
func (p *Processor) DetectEvents(f *telemetry.LobbySessionStateFrame) {
	p.eventDetector.ProcessFrame(f)
}

// EventsChan returns the channel for receiving detected events
func (fp *Processor) EventsChan() <-chan []*telemetry.LobbySessionEvent {
	return fp.eventDetector.EventsChan()
}

// Reset clears the processor state
func (fp *Processor) Reset() {
	fp.frameIndex = 0
	fp.eventDetector.Reset()
}

// Stop gracefully shuts down the frame processor
func (fp *Processor) Stop() {
	fp.eventDetector.Stop()
}
