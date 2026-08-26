package processing

import (
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/echotools/tape/v4/pkg/events"
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

// New creates a new Processor wrapping the asynchronous event detector.
// Frames are drained by a background goroutine; when the detector's input or
// events channel fills, frames and event batches are dropped rather than
// blocking the caller (the non-blocking send in pkg/events). That loss is
// counted by the detector and surfaced here via DroppedFrames and
// DroppedEvents. Callers that must not lose frames should consume EventsChan
// promptly and treat a non-zero receipt as a capacity signal.
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
// Note: Events are processed asynchronously and can be received via (*Processor).EventsChan().
func (fp *Processor) ProcessAndDetectEvents(sessionResponseData, userBonesData []byte, timestamp time.Time) (*telemetry.LobbySessionStateFrame, error) {
	// Reset the pre-allocated structs to avoid allocations
	// Pre-allocated structs to avoid memory allocations
	sessionResponse := &enginev1.SessionResponse{}
	bonesResponse := &enginev1.PlayerBonesResponse{}

	// Parse session data
	if err := fp.unmarshaler.Unmarshal(sessionResponseData, sessionResponse); err != nil {
		return nil, err
	}

	// Parse user bones data (if provided)
	var playerBones *enginev1.PlayerBonesResponse
	if len(userBonesData) > 0 {
		if err := fp.unmarshaler.Unmarshal(userBonesData, bonesResponse); err != nil {
			return nil, err
		}
		playerBones = bonesResponse
	}

	// Create the frame — only set PlayerBones when data was present and parsed.
	frame := &telemetry.LobbySessionStateFrame{
		FrameIndex:  fp.frameIndex,
		Timestamp:   timestamppb.New(timestamp),
		Session:     sessionResponse,
		PlayerBones: playerBones,
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

// frameDropCounter is implemented by detectors that track the frames they
// dropped because their input channel was full.
type frameDropCounter interface {
	DroppedFrames() uint64
}

// eventDropCounter is implemented by detectors that track the event batches
// they dropped because their events channel was full.
type eventDropCounter interface {
	DroppedEvents() uint64
}

// DroppedFrames returns the number of frames the wrapped detector dropped
// because its input channel was full. Under the default asynchronous detector
// (events.AsyncDetector) this counts every frame the non-blocking send in
// ProcessFrame discarded under back-pressure; it is monotonic and safe to read
// while frames are still being fed. A custom Detector that does not count
// drops reports zero: the contract is that a detector which does not track
// loss is reported as having none.
func (fp *Processor) DroppedFrames() uint64 {
	if c, ok := fp.eventDetector.(frameDropCounter); ok {
		return c.DroppedFrames()
	}
	return 0
}

// DroppedEvents returns the number of event batches the wrapped detector
// dropped because its events channel was full. Under the default asynchronous
// detector this counts every batch the non-blocking send in the processing
// loop discarded while no caller drained EventsChan. A custom Detector that
// does not count drops reports zero: the contract is that a detector which
// does not track loss is reported as having none.
func (fp *Processor) DroppedEvents() uint64 {
	if c, ok := fp.eventDetector.(eventDropCounter); ok {
		return c.DroppedEvents()
	}
	return 0
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
