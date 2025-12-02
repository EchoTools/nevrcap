package processing

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestFrameProcessor tests the high-performance frame processing
func TestFrameProcessor(t *testing.T) {
	processor := New()

	// Create test session data
	sessionData := createTestSessionData(t)
	userBonesData := createTestUserBonesData(t)

	// Process first frame
	frame1, err := processor.ProcessFrame(sessionData, userBonesData, time.Now())
	if err != nil {
		t.Fatalf("Failed to process first frame: %v", err)
	}

	if frame1.FrameIndex != 0 {
		t.Errorf("Expected frame index 0, got %d", frame1.FrameIndex)
	}

	if len(frame1.Events) != 0 {
		t.Errorf("Expected no events for first frame, got %d", len(frame1.Events))
	}

	// Modify session data to trigger events
	modifiedSessionData := createModifiedSessionData(t)

	// Process second frame
	frame2, err := processor.ProcessFrame(modifiedSessionData, userBonesData, time.Now().Add(time.Millisecond))
	if err != nil {
		t.Fatalf("Failed to process second frame: %v", err)
	}

	if frame2.FrameIndex != 1 {
		t.Errorf("Expected frame index 1, got %d", frame2.FrameIndex)
	}

	// Note: Event detection depends on having actual differences in game state
	// For now, we just verify the frame was processed correctly
	t.Logf("Second frame processed with %d events", len(frame2.Events))
}

// Helper functions

func createTestSessionData(t *testing.T) []byte {
	session := &apigame.SessionResponse{
		SessionId:        "test-session",
		GameStatus:       "running",
		BluePoints:       0,
		OrangePoints:     0,
		BlueRoundScore:   0,
		OrangeRoundScore: 0,
		Teams:            []*apigame.Team{},
	}

	data, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("Failed to marshal test session data: %v", err)
	}
	return data
}

func createModifiedSessionData(t *testing.T) []byte {
	session := &apigame.SessionResponse{
		SessionId:        "test-session",
		GameStatus:       "running",
		BluePoints:       1, // Changed score
		OrangePoints:     0,
		BlueRoundScore:   1, // Changed score
		OrangeRoundScore: 0,
		Teams:            []*apigame.Team{},
	}

	data, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("Failed to marshal modified session data: %v", err)
	}
	return data
}

func createTestUserBonesData(t *testing.T) []byte {
	userBones := &apigame.PlayerBonesResponse{
		UserBones: []*apigame.UserBones{},
		ErrCode:   0,
	}

	data, err := json.Marshal(userBones)
	if err != nil {
		t.Fatalf("Failed to marshal test user bones data: %v", err)
	}
	return data
}

func createTestFrame(t *testing.T) *rtapi.LobbySessionStateFrame {
	sessionResponse := &apigame.SessionResponse{
		SessionId:        "test-session",
		GameStatus:       "running",
		BluePoints:       0,
		OrangePoints:     0,
		BlueRoundScore:   0,
		OrangeRoundScore: 0,
		Teams:            []*apigame.Team{},
	}

	bonesResponse := &apigame.PlayerBonesResponse{
		UserBones: []*apigame.UserBones{},
		ErrCode:   0,
	}

	return &rtapi.LobbySessionStateFrame{
		FrameIndex:  0,
		Timestamp:   timestamppb.Now(),
		Events:      []*rtapi.LobbySessionEvent{},
		Session:     sessionResponse,
		PlayerBones: bonesResponse,
	}
}

func TestFrameProcessor_InvalidJSON(t *testing.T) {
	processor := New()

	// Invalid session data
	_, err := processor.ProcessFrame([]byte("{invalid-json"), nil, time.Now())
	if err == nil {
		t.Error("Expected error for invalid session JSON, got nil")
	}

	// Valid session, invalid bones
	sessionData := createTestSessionData(t)
	_, err = processor.ProcessFrame(sessionData, []byte("{invalid-bones"), time.Now())
	if err == nil {
		t.Error("Expected error for invalid bones JSON, got nil")
	}
}

type mockDetector struct {
	processedFrames []*rtapi.LobbySessionStateFrame
	eventsChan      chan []*rtapi.LobbySessionEvent
}

func (m *mockDetector) ProcessFrame(frame *rtapi.LobbySessionStateFrame) {
	m.processedFrames = append(m.processedFrames, frame)
}

func (m *mockDetector) EventsChan() <-chan []*rtapi.LobbySessionEvent {
	return m.eventsChan
}

func (m *mockDetector) Reset() {
	m.processedFrames = nil
}

func (m *mockDetector) Stop() {
	close(m.eventsChan)
}

func TestFrameProcessor_Delegation(t *testing.T) {
	mock := &mockDetector{
		eventsChan: make(chan []*rtapi.LobbySessionEvent),
	}

	processor := NewWithDetector(mock)

	sessionData := createTestSessionData(t)
	userBonesData := createTestUserBonesData(t)

	// Process a frame
	_, err := processor.ProcessFrame(sessionData, userBonesData, time.Now())
	if err != nil {
		t.Fatalf("ProcessFrame failed: %v", err)
	}

	// Verify detector received it
	if len(mock.processedFrames) != 1 {
		t.Errorf("Expected 1 processed frame, got %d", len(mock.processedFrames))
	}

	// Verify Reset delegation
	processor.Reset()
	if len(mock.processedFrames) != 0 {
		t.Error("Expected processed frames to be cleared after Reset")
	}

	// Verify Stop delegation (channel closed)
	processor.Stop()
	select {
	case _, ok := <-mock.EventsChan():
		if ok {
			t.Error("Expected events channel to be closed")
		}
	default:
		// Should be closed immediately
		_, ok := <-mock.EventsChan()
		if ok {
			t.Error("Expected events channel to be closed")
		}
	}
}
