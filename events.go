package nevrcap

import (
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
)

// EventDetector detects post_match events
type EventDetector struct {
	prevGameStatus string
	curGameStatus  string

	// Ring buffer for frames (fixed size array)
	frameBuffer [10]*rtapi.LobbySessionStateFrame
	writeIndex  int // Current write position
	frameCount  int // Number of frames currently in buffer
}

// NewEventDetector creates a new event detector
func NewEventDetector() *EventDetector {
	return &EventDetector{}
}

// AddFrame processes a frame and returns detected events
func (ed *EventDetector) AddFrame(frame *rtapi.LobbySessionStateFrame) []*rtapi.LobbySessionEvent {
	// Add frame to buffer
	ed.addFrameToBuffer(frame)

	// Detect events using the detection algorithm
	events := ed.detectEvents()

	if s := frame.GetSession().GetGameStatus(); s != ed.curGameStatus {
		ed.prevGameStatus = ed.curGameStatus
		ed.curGameStatus = s
	}
	// Update state for next frame

	return events
}

// addFrameToBuffer adds a frame to the buffer
func (ed *EventDetector) addFrameToBuffer(frame *rtapi.LobbySessionStateFrame) {
	// Write to current position
	ed.frameBuffer[ed.writeIndex] = frame

	// Advance write index (wrap around)
	ed.writeIndex = (ed.writeIndex + 1) % len(ed.frameBuffer)

	// Track frame count (max is buffer size)
	if ed.frameCount < len(ed.frameBuffer) {
		ed.frameCount++
	}
}

func (ed *EventDetector) latestFrameIndex() int {
	return (ed.writeIndex - 1 + len(ed.frameBuffer)) % len(ed.frameBuffer)
}

// Reset clears the event detector state
func (ed *EventDetector) Reset() {
	ed.prevGameStatus = ""
	ed.writeIndex = 0
	ed.frameCount = 0
}
