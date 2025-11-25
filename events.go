package nevrcap

import (
	"context"
	"sync"

	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
)

// Detector defines the behavior required to process frames and emit lobby events.
type Detector interface {
	ProcessFrame(*rtapi.LobbySessionStateFrame)
	EventsChan() <-chan []*rtapi.LobbySessionEvent
	Reset()
	Stop()
}

const MaxFrameBufferCapacity = 10

// EventDetector detects post_match events
type EventDetector struct {
	previousGameStatusFrame *rtapi.LobbySessionStateFrame

	// Ring buffer for frames
	frameBuffer [MaxFrameBufferCapacity]*rtapi.LobbySessionStateFrame
	writeIndex  int // Current write position
	frameCount  int // Number of frames currently in buffer

	sensors []EventSensor

	// Channel-based processing
	inputChan  chan *rtapi.LobbySessionStateFrame
	eventsChan chan []*rtapi.LobbySessionEvent
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.RWMutex // Protects frame buffer access
}

var _ Detector = (*EventDetector)(nil)

// NewEventDetector creates a new event detector with goroutine-based processing
func NewEventDetector() *EventDetector {
	ctx, cancel := context.WithCancel(context.Background())
	ed := &EventDetector{
		inputChan:  make(chan *rtapi.LobbySessionStateFrame, 100),
		eventsChan: make(chan []*rtapi.LobbySessionEvent, 10),
		ctx:        ctx,
		cancel:     cancel,
	}
	ed.Start()
	return ed
}

// Start launches the background processing goroutine
func (ed *EventDetector) Start() {
	ed.wg.Add(1)
	go ed.processLoop()
}

// Stop gracefully shuts down the event detector
func (ed *EventDetector) Stop() {
	ed.cancel()
	ed.wg.Wait()
	close(ed.eventsChan)
}

// Reset clears the event detector state
func (ed *EventDetector) Reset() {
	ed.mu.Lock()
	defer ed.mu.Unlock()
	ed.writeIndex = 0
	ed.frameCount = 0
}

// ProcessFrame writes a frame to the processing channel (non-blocking)
func (ed *EventDetector) ProcessFrame(frame *rtapi.LobbySessionStateFrame) {
	select {
	case ed.inputChan <- frame:
		// Frame sent successfully
	case <-ed.ctx.Done():
		// Detector is stopping, ignore frame
	default:
		// Channel full, drop frame (could also block or log)
	}
}

// EventsChan returns the channel for receiving detected events
func (ed *EventDetector) EventsChan() <-chan []*rtapi.LobbySessionEvent {
	return ed.eventsChan
}

// processLoop is the background goroutine that processes frames
func (ed *EventDetector) processLoop() {
	defer ed.wg.Done()

	for {
		select {
		case frame := <-ed.inputChan:
			// Add frame to buffer
			ed.addFrameToBuffer(frame)

			// Detect events using the detection algorithm
			events := ed.detectEvents()

			// Send events if any were detected
			if len(events) > 0 {
				select {
				case ed.eventsChan <- events:
					// Events sent successfully
				case <-ed.ctx.Done():
					return
				}
			}

		case <-ed.ctx.Done():
			return
		}
	}
}

// addFrameToBuffer adds a frame to the buffer (must be called with lock held or from processLoop)
func (ed *EventDetector) addFrameToBuffer(frame *rtapi.LobbySessionStateFrame) {
	ed.mu.Lock()
	defer ed.mu.Unlock()

	// Write to current position
	ed.frameBuffer[ed.writeIndex] = frame

	// Advance write index (wrap around)
	ed.writeIndex = (ed.writeIndex + 1) % len(ed.frameBuffer)

	// Track frame count (max is buffer size)
	if ed.frameCount < len(ed.frameBuffer) {
		ed.frameCount++
	}
}

// getFrame returns the frame at the given offset (thread-safe)
func (ed *EventDetector) getFrame(offset int) *rtapi.LobbySessionStateFrame {
	ed.mu.RLock()
	defer ed.mu.RUnlock()

	if offset > MaxFrameBufferCapacity {
		return nil
	}
	idx := (ed.writeIndex + offset + len(ed.frameBuffer)) % len(ed.frameBuffer)
	return ed.frameBuffer[idx]
}

// lastFrame returns the most recently added frame
func (ed *EventDetector) lastFrame() *rtapi.LobbySessionStateFrame {
	ed.mu.RLock()
	defer ed.mu.RUnlock()

	if ed.frameCount == 0 {
		return nil
	}
	idx := ed.lastFrameIndex()
	return ed.frameBuffer[idx]
}

// lastFrameIndex returns the index of the most recently written frame
func (ed *EventDetector) lastFrameIndex() int {
	// No lock needed - this is called from methods that already hold the lock
	return (ed.writeIndex - 1 + len(ed.frameBuffer)) % len(ed.frameBuffer)
}

// detectEvents analyzes frames in the ring buffer and returns detected events
func (ed *EventDetector) detectEvents() []*rtapi.LobbySessionEvent {
	var newEvents []*rtapi.LobbySessionEvent
	// Use the newest frame available in the buffer
	if len(ed.frameBuffer) == 0 {
		return nil
	}

	for _, s := range ed.sensors {
		event := s.AddFrame(ed.getFrame(0))
		if event != nil {
			newEvents = append(newEvents, event)
		}
	}

	for _, fn := range [...]detectionFunction{
		ed.detectPostMatchEvent,
	} {
		if events := fn(ed.lastFrameIndex()); events != nil {
			newEvents = append(newEvents, events...)
		}
	}

	return newEvents
}
