package events

import (
	"testing"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// testSensor is a test helper that implements the Sensor interface with callback support
type testSensor struct {
	onAddFrame func(*rtapi.LobbySessionStateFrame) *rtapi.LobbySessionEvent
}

func (m *testSensor) AddFrame(frame *rtapi.LobbySessionStateFrame) *rtapi.LobbySessionEvent {
	if m.onAddFrame != nil {
		return m.onAddFrame(frame)
	}
	return nil
}

// TestEmptyFrameBufferBug validates the bug where len(ed.frameBuffer) is checked
// instead of ed.frameCount. This test will fail with the buggy code because
// len(ed.frameBuffer) returns the capacity (non-zero) even when no frames have
// been added yet (frameCount == 0), causing lastFrame() to potentially return nil
// unexpectedly.
func TestEmptyFrameBufferBug(t *testing.T) {
	// Create a new detector with synchronous processing to make testing easier
	detector := New(
		WithSynchronousProcessing(),
		WithFrameBufferSize(5), // Buffer has capacity 5
	)
	defer detector.Stop()

	// At this point:
	// - len(detector.frameBuffer) == 5 (capacity)
	// - detector.frameCount == 0 (no frames added yet)

	// Verify initial state
	if len(detector.frameBuffer) == 0 {
		t.Error("Expected frameBuffer to have non-zero capacity")
	}
	if detector.frameCount != 0 {
		t.Errorf("Expected frameCount to be 0, got %d", detector.frameCount)
	}

	// The bug: detectEvents checks len(ed.frameBuffer) which is 5, not 0
	// So it won't early return and will try to use lastFrame()
	// But lastFrame() correctly checks frameCount and returns nil

	// Call detectEvents directly to test the bug
	eventBuffer := make([]*rtapi.LobbySessionEvent, 0)

	// This should not panic or cause issues
	// With the bug: it will call sensor.AddFrame(nil) because lastFrame() returns nil
	// With the fix: it will early return because frameCount == 0
	result := detector.detectEvents(eventBuffer)

	if len(result) != 0 {
		t.Errorf("Expected no events from empty buffer, got %d events", len(result))
	}

	// Verify lastFrame() returns nil when frameCount is 0
	if detector.lastFrame() != nil {
		t.Error("Expected lastFrame() to return nil when frameCount is 0")
	}
}

// TestEmptyFrameBufferWithSensor tests that sensors receive nil when the buffer is empty
// This demonstrates the actual bug impact
func TestEmptyFrameBufferWithSensor(t *testing.T) {
	// Create a custom sensor that tracks what frame it receives
	var frameReceivedByDetector bool
	var receivedFrame *rtapi.LobbySessionStateFrame

	customSensor := &testSensor{
		onAddFrame: func(frame *rtapi.LobbySessionStateFrame) *rtapi.LobbySessionEvent {
			frameReceivedByDetector = true
			receivedFrame = frame
			return nil
		},
	}

	// Create detector with the custom sensor and synchronous processing
	detector := New(
		WithSynchronousProcessing(),
		WithFrameBufferSize(5),
		WithSensors(customSensor),
	)
	defer detector.Stop()

	// Call detectEvents on empty buffer
	eventBuffer := make([]*rtapi.LobbySessionEvent, 0)
	detector.detectEvents(eventBuffer)

	// With the bug: sensor.AddFrame() gets called with nil because len(frameBuffer) != 0
	// With the fix: sensor.AddFrame() doesn't get called at all because frameCount == 0

	if frameReceivedByDetector {
		t.Error("Sensor should not be called when buffer is empty (frameCount == 0)")
		if receivedFrame == nil {
			t.Error("BUG CONFIRMED: Sensor received nil frame when it shouldn't have been called at all")
		}
	}
}

// TestFrameBufferAfterAddingFrame verifies normal operation after adding a frame
func TestFrameBufferAfterAddingFrame(t *testing.T) {
	var receivedFrame *rtapi.LobbySessionStateFrame

	customSensor := &testSensor{
		onAddFrame: func(frame *rtapi.LobbySessionStateFrame) *rtapi.LobbySessionEvent {
			receivedFrame = frame
			return nil
		},
	}

	detector := New(
		WithSynchronousProcessing(),
		WithFrameBufferSize(5),
		WithSensors(customSensor),
	)
	defer detector.Stop()

	// Add a frame
	testFrame := &rtapi.LobbySessionStateFrame{
		FrameIndex: 42,
		Timestamp:  timestamppb.Now(),
		Session: &apigame.SessionResponse{
			GameStatus: "playing",
		},
	}

	detector.addFrameToBuffer(testFrame)

	// Now frameCount should be 1
	if detector.frameCount != 1 {
		t.Errorf("Expected frameCount to be 1, got %d", detector.frameCount)
	}

	// lastFrame should return our frame
	lastFrame := detector.lastFrame()
	if lastFrame == nil {
		t.Error("Expected lastFrame() to return the added frame, got nil")
	} else if lastFrame.FrameIndex != 42 {
		t.Errorf("Expected frame index 42, got %d", lastFrame.FrameIndex)
	}

	// Now detectEvents should work correctly
	eventBuffer := make([]*rtapi.LobbySessionEvent, 0)
	detector.detectEvents(eventBuffer)

	// Sensor should have received the frame
	if receivedFrame == nil {
		t.Error("Sensor should have received a frame")
	} else if receivedFrame.FrameIndex != 42 {
		t.Errorf("Sensor should have received frame with index 42, got %d", receivedFrame.FrameIndex)
	}
}
