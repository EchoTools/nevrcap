package events

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

const DefaultFrameBufferCapacity = 10

// Option configures the AsyncDetector
type Option func(*AsyncDetector)

// WithInputChannelSize sets the size of the input channel
func WithInputChannelSize(size int) Option {
	return func(ed *AsyncDetector) {
		ed.inputChan = make(chan *rtapi.LobbySessionStateFrame, size)
	}
}

// WithEventsChannelSize sets the size of the events channel
func WithEventsChannelSize(size int) Option {
	return func(ed *AsyncDetector) {
		ed.eventsChan = make(chan []*rtapi.LobbySessionEvent, size)
	}
}

// WithFrameBufferSize sets the size of the frame buffer
func WithFrameBufferSize(size int) Option {
	return func(ed *AsyncDetector) {
		ed.frameBuffer = make([]*rtapi.LobbySessionStateFrame, size)
	}
}

// WithSensors adds sensors to the detector
func WithSensors(sensors ...Sensor) Option {
	return func(ed *AsyncDetector) {
		ed.sensors = append(ed.sensors, sensors...)
	}
}

// WithSynchronousProcessing enables synchronous processing of frames
func WithSynchronousProcessing() Option {
	return func(ed *AsyncDetector) {
		ed.synchronous = true
	}
}

// AsyncDetector detects post_match events
type AsyncDetector struct {
	previousGameStatusFrame *rtapi.LobbySessionStateFrame

	// Ring buffer for frames
	frameBuffer []*rtapi.LobbySessionStateFrame
	writeIndex  int // Current write position
	frameCount  int // Number of frames currently in buffer

	sensors []Sensor

	// Channel-based processing
	inputChan  chan *rtapi.LobbySessionStateFrame
	eventsChan chan []*rtapi.LobbySessionEvent
	resetChan  chan struct{}
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	stopOnce   sync.Once

	// Reusable buffer for events to reduce allocations
	eventBuffer []*rtapi.LobbySessionEvent

	synchronous bool
}

var _ Detector = (*AsyncDetector)(nil)

// New creates a new event detector with goroutine-based processing
func New(opts ...Option) *AsyncDetector {
	ctx, cancel := context.WithCancel(context.Background())
	ed := &AsyncDetector{
		inputChan:   make(chan *rtapi.LobbySessionStateFrame, 100),
		eventsChan:  make(chan []*rtapi.LobbySessionEvent, 10),
		resetChan:   make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
		frameBuffer: make([]*rtapi.LobbySessionStateFrame, DefaultFrameBufferCapacity),
		eventBuffer: make([]*rtapi.LobbySessionEvent, 0, 10),
	}

	for _, opt := range opts {
		opt(ed)
	}

	ed.Start()
	return ed
}

// Start launches the background processing goroutine
func (ed *AsyncDetector) Start() {
	ed.wg.Add(1)
	go ed.processLoop()
}

// Stop gracefully shuts down the event detector
func (ed *AsyncDetector) Stop() {
	ed.stopOnce.Do(func() {
		ed.cancel()
		ed.wg.Wait()
		close(ed.eventsChan)
	})
}

// Reset clears the event detector state
func (ed *AsyncDetector) Reset() {
	select {
	case ed.resetChan <- struct{}{}:
	case <-ed.ctx.Done():
	}
}

// ProcessFrame writes a frame to the processing channel (non-blocking)
func (ed *AsyncDetector) ProcessFrame(frame *rtapi.LobbySessionStateFrame) {
	if ed.synchronous {
		ed.processFrameSync(frame)
		return
	}

	select {
	case ed.inputChan <- frame:
		// Frame sent successfully
	case <-ed.ctx.Done():
		// Detector is stopping, ignore frame
	default:
		// Channel full, drop frame (could also block or log)
	}
}

func (ed *AsyncDetector) processFrameSync(frame *rtapi.LobbySessionStateFrame) {
	// Add frame to buffer
	ed.addFrameToBuffer(frame)

	// Detect events using the detection algorithm
	ed.eventBuffer = ed.eventBuffer[:0]
	ed.eventBuffer = ed.detectEvents(ed.eventBuffer)

	// Send events if any were detected
	if len(ed.eventBuffer) > 0 {
		// Copy events to avoid race conditions with the reused buffer
		eventsToSend := make([]*rtapi.LobbySessionEvent, len(ed.eventBuffer))
		copy(eventsToSend, ed.eventBuffer)

		// In synchronous mode, use non-blocking send to avoid blocking ProcessFrame.
		// This ensures ProcessFrame completes immediately in the caller's goroutine.
		// Events are dropped if the channel is full, which is acceptable since
		// synchronous mode prioritizes immediate processing over guaranteed delivery.
		select {
		case ed.eventsChan <- eventsToSend:
			// Events sent successfully
		case <-ed.ctx.Done():
			// Detector is stopping
			return
		default:
			// Channel is full, drop events rather than blocking.
			// This maintains the synchronous processing guarantee.
		}
	}
}

// EventsChan returns the channel for receiving detected events
func (ed *AsyncDetector) EventsChan() <-chan []*rtapi.LobbySessionEvent {
	return ed.eventsChan
}

// processLoop is the background goroutine that processes frames
func (ed *AsyncDetector) processLoop() {
	defer ed.wg.Done()

	for {
		select {
		case <-ed.resetChan:
			ed.writeIndex = 0
			ed.frameCount = 0
			ed.previousGameStatusFrame = nil
			for i := range ed.frameBuffer {
				ed.frameBuffer[i] = nil
			}

		case frame := <-ed.inputChan:
			// Add frame to buffer
			ed.addFrameToBuffer(frame)

			// Detect events using the detection algorithm
			ed.eventBuffer = ed.eventBuffer[:0]
			ed.eventBuffer = ed.detectEvents(ed.eventBuffer)

			// Send events if any were detected
			if len(ed.eventBuffer) > 0 {
				// Copy events to avoid race conditions with the reused buffer
				eventsToSend := make([]*rtapi.LobbySessionEvent, len(ed.eventBuffer))
				copy(eventsToSend, ed.eventBuffer)

				select {
				case ed.eventsChan <- eventsToSend:
					// Events sent successfully
				case <-ed.ctx.Done():
					// Context cancelled, drain inputChan and exit
					ed.drainInputChan()
					return
				}
			}

		case <-ed.ctx.Done():
			// Context cancelled, drain inputChan before exiting
			ed.drainInputChan()
			return
		}
	}
}

// drainInputChan drains any remaining frames from inputChan to prevent resource leaks
func (ed *AsyncDetector) drainInputChan() {
	for {
		select {
		case <-ed.inputChan:
			// Discard frame
		default:
			// Channel is empty
			return
		}
	}
}

// addFrameToBuffer adds a frame to the buffer
func (ed *AsyncDetector) addFrameToBuffer(frame *rtapi.LobbySessionStateFrame) {
	// Write to current position
	ed.frameBuffer[ed.writeIndex] = frame

	// Advance write index (wrap around)
	ed.writeIndex = (ed.writeIndex + 1) % len(ed.frameBuffer)

	// Track frame count (max is buffer size)
	if ed.frameCount < len(ed.frameBuffer) {
		ed.frameCount++
	}
}

// getFrame returns the frame at the given offset (0 = most recent, 1 = previous, etc.)
func (ed *AsyncDetector) getFrame(offset int) *rtapi.LobbySessionStateFrame {
	if offset >= ed.frameCount {
		return nil
	}
	idx := (ed.writeIndex - 1 - offset + len(ed.frameBuffer)) % len(ed.frameBuffer)
	return ed.frameBuffer[idx]
}

// lastFrame returns the most recently added frame
func (ed *AsyncDetector) lastFrame() *rtapi.LobbySessionStateFrame {
	if ed.frameCount == 0 {
		return nil
	}
	idx := ed.lastFrameIndex()
	return ed.frameBuffer[idx]
}

// lastFrameIndex returns the index of the most recently written frame
func (ed *AsyncDetector) lastFrameIndex() int {
	return (ed.writeIndex - 1 + len(ed.frameBuffer)) % len(ed.frameBuffer)
}

// detectEvents analyzes frames in the ring buffer and returns detected events
func (ed *AsyncDetector) detectEvents(dst []*rtapi.LobbySessionEvent) []*rtapi.LobbySessionEvent {
	// Use the newest frame available in the buffer
	if ed.frameCount == 0 {
		return dst
	}

	for _, s := range ed.sensors {
		event := s.AddFrame(ed.lastFrame())
		if event != nil {
			dst = append(dst, event)
		}
	}

	for _, fn := range [...]detectionFunction{
		ed.detectPostMatchEvent,
	} {
		dst = fn(ed.lastFrameIndex(), dst)
	}

	return dst
}
