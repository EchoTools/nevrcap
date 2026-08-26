package processing

import (
	"encoding/json"
	"runtime"
	"sync"
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/echotools/tape/v4/pkg/events"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestFrameProcessor tests the high-performance frame processing
func TestFrameProcessor(t *testing.T) {
	processor := New()

	// Create test session data
	sessionData := createTestSessionData(t)
	userBonesData := createTestUserBonesData(t)

	// Process first frame
	frame1, err := processor.ProcessAndDetectEvents(sessionData, userBonesData, time.Now())
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
	frame2, err := processor.ProcessAndDetectEvents(modifiedSessionData, userBonesData, time.Now().Add(time.Millisecond))
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
	session := &enginev1.SessionResponse{
		SessionId:        "test-session",
		GameStatus:       "running",
		BluePoints:       0,
		OrangePoints:     0,
		BlueRoundScore:   0,
		OrangeRoundScore: 0,
		Teams:            []*enginev1.Team{},
	}

	data, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("Failed to marshal test session data: %v", err)
	}
	return data
}

func createModifiedSessionData(t *testing.T) []byte {
	session := &enginev1.SessionResponse{
		SessionId:        "test-session",
		GameStatus:       "running",
		BluePoints:       1, // Changed score
		OrangePoints:     0,
		BlueRoundScore:   1, // Changed score
		OrangeRoundScore: 0,
		Teams:            []*enginev1.Team{},
	}

	data, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("Failed to marshal modified session data: %v", err)
	}
	return data
}

func createTestUserBonesData(t *testing.T) []byte {
	userBones := &enginev1.PlayerBonesResponse{
		UserBones: []*enginev1.UserBones{},
		ErrCode:   0,
	}

	data, err := json.Marshal(userBones)
	if err != nil {
		t.Fatalf("Failed to marshal test user bones data: %v", err)
	}
	return data
}

func createTestFrame(t *testing.T) *telemetry.LobbySessionStateFrame {
	sessionResponse := &enginev1.SessionResponse{
		SessionId:        "test-session",
		GameStatus:       "running",
		BluePoints:       0,
		OrangePoints:     0,
		BlueRoundScore:   0,
		OrangeRoundScore: 0,
		Teams:            []*enginev1.Team{},
	}

	bonesResponse := &enginev1.PlayerBonesResponse{
		UserBones: []*enginev1.UserBones{},
		ErrCode:   0,
	}

	return &telemetry.LobbySessionStateFrame{
		FrameIndex:  0,
		Timestamp:   timestamppb.Now(),
		Events:      []*telemetry.LobbySessionEvent{},
		Session:     sessionResponse,
		PlayerBones: bonesResponse,
	}
}

func TestFrameProcessor_InvalidJSON(t *testing.T) {
	processor := New()

	// Invalid session data
	_, err := processor.ProcessAndDetectEvents([]byte("{invalid-json"), nil, time.Now())
	if err == nil {
		t.Error("Expected error for invalid session JSON, got nil")
	}

	// Valid session, invalid bones
	sessionData := createTestSessionData(t)
	_, err = processor.ProcessAndDetectEvents(sessionData, []byte("{invalid-bones"), time.Now())
	if err == nil {
		t.Error("Expected error for invalid bones JSON, got nil")
	}
}

type mockDetector struct {
	processedFrames []*telemetry.LobbySessionStateFrame
	eventsChan      chan []*telemetry.LobbySessionEvent
}

func (m *mockDetector) ProcessFrame(frame *telemetry.LobbySessionStateFrame) {
	m.processedFrames = append(m.processedFrames, frame)
}

func (m *mockDetector) EventsChan() <-chan []*telemetry.LobbySessionEvent {
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
		eventsChan: make(chan []*telemetry.LobbySessionEvent),
	}

	processor := NewWithDetector(mock)

	sessionData := createTestSessionData(t)
	userBonesData := createTestUserBonesData(t)

	// Process a frame
	_, err := processor.ProcessAndDetectEvents(sessionData, userBonesData, time.Now())
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

// scoreFrame builds a frame whose distinct blue score makes the scoreboard
// sensor emit a ScoreboardUpdated event on every successive value, mirroring
// pkg/events/sync_detector_drop_test.go:13.
func scoreFrame(bluePoints int32) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			GameStatus: events.GameStatusPlaying,
			BluePoints: bluePoints,
		},
	}
}

// pollUntil polls cond until it returns true or the deadline passes. The repo
// has no Eventually helper and the addendum forbids time.Sleep in tests, so
// this is the bounded, sleep-free wait used to observe the async detector's
// background goroutine.
func pollUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s", timeout)
		}
		runtime.Gosched()
	}
}

// TestProcessor_DroppedFramesReceipt is the saturation test for the loss
// receipt: it drives the default processor (New()) and floods its input
// channel with more frames than the async loop can drain, then asserts the
// caller-visible Processor.DroppedFrames() reports the loss. This goes through
// the Processor's public surface (DetectEvents + DroppedFrames), not the
// AsyncDetector directly. DetectEvents is used rather than
// ProcessAndDetectEvents because the latter mutates frameIndex without a lock
// and the flood is deliberately concurrent.
func TestProcessor_DroppedFramesReceipt(t *testing.T) {
	p := New()
	defer p.Stop()

	const (
		producers    = 8
		perProducer  = 25000
		channelDepth = 100 // events.New() input channel capacity (events.go:121)
	)

	// Each producer hammers DetectEvents in a tight loop. Aggregate producer
	// rate vastly exceeds the single consumer's one-frame-per-iteration drain,
	// so the buffered input channel (capacity channelDepth) fills and the
	// non-blocking send in events.go:207 drops every subsequent frame until the
	// consumer frees a slot. 8 x 25k sends saturate a 100-slot channel with
	// overwhelming probability and keep the counter non-zero for the duration.
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range producers {
		wg.Go(func() {
			<-start
			frame := &telemetry.LobbySessionStateFrame{Timestamp: timestamppb.Now()}
			for range perProducer {
				p.DetectEvents(frame)
			}
		})
	}
	close(start)
	wg.Wait()

	if got := p.DroppedFrames(); got == 0 {
		t.Fatal("expected the processor loss receipt to report dropped frames once the input channel saturates")
	}
}

// TestProcessor_DroppedEventsReceipt fills the events channel through the
// Processor: a sensor-equipped detector wrapped by NewWithDetector emits one
// ScoreboardUpdated batch per distinct score, and with the events channel
// never drained the async loop's non-blocking send (events.go:292-303) drops
// every batch past the buffer. The counter must be visible through
// Processor.DroppedEvents(). New() itself cannot reach this path — its default
// detector (events.New()) carries no sensors, so it never emits events and the
// events channel can never fill.
func TestProcessor_DroppedEventsReceipt(t *testing.T) {
	p := NewWithDetector(events.NewWithDefaultSensors())
	defer p.Stop()

	// Seed the scoreboard sensor so frame 0 does not emit; then feed distinct
	// scores without ever draining the events channel (cap 10).
	p.DetectEvents(scoreFrame(0))
	for i := int32(1); i <= 50; i++ {
		p.DetectEvents(scoreFrame(i))
	}

	pollUntil(t, 5*time.Second, func() bool { return p.DroppedEvents() > 0 })

	if got := p.DroppedEvents(); got == 0 {
		t.Fatal("expected the processor loss receipt to report dropped event batches once the events channel fills")
	}
}

// TestProcessor_DropCountersZeroForNonCountingDetector pins the unsupported
// contract: a custom Detector that does not implement the drop counters
// reports zero rather than panicking or misreporting. The mockDetector above
// satisfies events.Detector but has no DroppedFrames/DroppedEvents methods.
func TestProcessor_DropCountersZeroForNonCountingDetector(t *testing.T) {
	mock := &mockDetector{
		eventsChan: make(chan []*telemetry.LobbySessionEvent),
	}
	p := NewWithDetector(mock)
	defer p.Stop()

	if got := p.DroppedFrames(); got != 0 {
		t.Fatalf("expected DroppedFrames()==0 for a detector that does not count drops, got %d", got)
	}
	if got := p.DroppedEvents(); got != 0 {
		t.Fatalf("expected DroppedEvents()==0 for a detector that does not count drops, got %d", got)
	}
}
