package events

import (
	"testing"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
)

// BUG-2: GoalScored v1 event has PersonScored and AssistScored as display names.
// The mapper in mapping.go (converters.go) maps this to v2 GoalScored which has
// ScorerSlot and AssistSlot (slot indices) instead. Since the mapper cannot
// resolve display names to slot numbers, ScorerSlot and AssistSlot remain at
// their zero values. This test verifies the v1 sensor output.
//
// Note: This tests the sensor's output at the v1 level. The v1 GoalScored event
// carries ScoreDetails (a LastScore) with PersonScored/AssistScored display names.
// The v2 mapping drops these names (tested in mapping_audit_test.go).
func TestGoalScoredSensor_ScoreDetailsPreserved(t *testing.T) {
	sensor := NewGoalScoredSensor()

	// First frame: nothing
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{},
	}
	sensor.AddFrame(frame1)

	// Second frame: goal scored with full details
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			LastScore: &enginev1.LastScore{
				PersonScored:   "PlayerOne",
				AssistScored:   "PlayerTwo",
				DiscSpeed:      22.5,
				DistanceThrown: 12.3,
				PointAmount:    2,
				Team:           "blue",
				GoalType:       "LONG SHOT",
			},
		},
	}
	event := sensor.AddFrame(frame2)

	if event == nil {
		t.Fatal("expected GoalScored event")
	}

	goal := event.GetGoalScored()
	if goal == nil {
		t.Fatalf("expected GoalScored, got %T", event.Event)
	}

	sd := goal.GetScoreDetails()
	if sd == nil {
		t.Fatal("expected ScoreDetails")
	}

	if sd.GetPersonScored() != "PlayerOne" {
		t.Errorf("PersonScored = %q, want %q", sd.GetPersonScored(), "PlayerOne")
	}
	if sd.GetAssistScored() != "PlayerTwo" {
		t.Errorf("AssistScored = %q, want %q", sd.GetAssistScored(), "PlayerTwo")
	}
	if sd.GetDiscSpeed() != 22.5 {
		t.Errorf("DiscSpeed = %f, want 22.5", sd.GetDiscSpeed())
	}
	if sd.GetDistanceThrown() != 12.3 {
		t.Errorf("DistanceThrown = %f, want 12.3", sd.GetDistanceThrown())
	}
	if sd.GetPointAmount() != 2 {
		t.Errorf("PointAmount = %d, want 2", sd.GetPointAmount())
	}
	if sd.GetTeam() != "blue" {
		t.Errorf("Team = %q, want %q", sd.GetTeam(), "blue")
	}
}

// PlayerJoinSensor drain loop through detectEvents.
// This verifies the detector's drain loop (frame=nil pattern) works with
// the PlayerJoinSensor when multiple players join in the same frame.
func TestPlayerJoinSensor_MultipleJoinsThroughDetector(t *testing.T) {
	sensor := NewPlayerJoinSensor()

	// First frame: no players (initializes the sensor)
	frame1 := &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{{}},
		},
	}
	sensor.AddFrame(frame1)

	// Simulate the detectEvents drain loop from events.go:
	//   frame := ed.lastFrame()
	//   for {
	//       event := s.AddFrame(frame)
	//       if event == nil { break }
	//       dst = append(dst, event)
	//       frame = nil  // drain mode
	//   }

	// Frame 2: three players join simultaneously
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{
				{
					Players: []*enginev1.TeamMember{
						{SlotNumber: 1, DisplayName: "Alice", JerseyNumber: 0},
						{SlotNumber: 2, DisplayName: "Bob", JerseyNumber: 1},
						{SlotNumber: 3, DisplayName: "Carol", JerseyNumber: 2},
					},
				},
			},
		},
	}

	var events []*telemetry.LobbySessionEvent
	f := frame2
	for {
		event := sensor.AddFrame(f)
		if event == nil {
			break
		}
		events = append(events, event)
		f = nil // drain mode (same pattern as detectEvents)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 PlayerJoined events via drain loop, got %d", len(events))
	}

	names := make(map[string]bool)
	for _, e := range events {
		joined := e.GetPlayerJoined()
		if joined == nil {
			t.Fatalf("expected PlayerJoined, got %T", e.Event)
		}
		names[joined.Player.GetDisplayName()] = true
	}

	for _, want := range []string{"Alice", "Bob", "Carol"} {
		if !names[want] {
			t.Errorf("missing PlayerJoined event for %s", want)
		}
	}
}

// PlayerJoinSensor drain loop: verify that the drain pattern works through the
// actual AsyncDetector.detectEvents method with synchronous processing.
// Uses addFrameToBuffer + detectEvents directly (like processFrameSync does)
// to verify the drain loop calls AddFrame repeatedly with nil.
func TestPlayerJoinSensor_MultipleJoinsViaDetector(t *testing.T) {
	sensor := NewPlayerJoinSensor()

	initFrame := &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{{}},
		},
	}

	joinFrame := &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{
				{
					Players: []*enginev1.TeamMember{
						{SlotNumber: 1, DisplayName: "Alice", JerseyNumber: 0},
						{SlotNumber: 2, DisplayName: "Bob", JerseyNumber: 1},
						{SlotNumber: 5, DisplayName: "Dave", JerseyNumber: 5},
					},
				},
			},
		},
	}

	detector := New(
		WithSynchronousProcessing(),
		WithFrameBufferSize(10),
		WithSensors(sensor),
	)
	defer detector.Stop()

	// Add init frame (same as what processFrameSync does internally)
	detector.addFrameToBuffer(initFrame)
	eventBuf := make([]*telemetry.LobbySessionEvent, 0)
	detector.detectEvents(eventBuf) // initializes sensor, no events

	// Add join frame and detect events
	detector.addFrameToBuffer(joinFrame)
	eventBuf = make([]*telemetry.LobbySessionEvent, 0)
	result := detector.detectEvents(eventBuf)

	if len(result) != 3 {
		t.Fatalf("detectEvents returned %d events, want 3", len(result))
	}

	names := make(map[string]bool)
	for _, e := range result {
		joined := e.GetPlayerJoined()
		if joined == nil {
			t.Fatalf("expected PlayerJoined, got %T", e.Event)
		}
		names[joined.Player.GetDisplayName()] = true
	}

	for _, want := range []string{"Alice", "Bob", "Dave"} {
		if !names[want] {
			t.Errorf("missing PlayerJoined event for %s", want)
		}
	}
}

// PlayerJoinSensor drain loop through synchronous ProcessFrame with eventsChan.
// This tests the full processFrameSync path, verifying events are delivered
// via eventsChan after ProcessFrame.
func TestPlayerJoinSensor_MultipleJoinsViaProcessFrame(t *testing.T) {
	sensor := NewPlayerJoinSensor()

	detector := New(
		WithSynchronousProcessing(),
		WithFrameBufferSize(10),
		WithSensors(sensor),
	)
	defer detector.Stop()

	// Init frame: empty session
	detector.ProcessFrame(&telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{{}},
		},
	})

	// No events from init
	select {
	case events, ok := <-detector.EventsChan():
		if ok && len(events) > 0 {
			t.Fatalf("unexpected %d events from init frame", len(events))
		}
	default:
	}

	// Join frame: 3 players join
	detector.ProcessFrame(&telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{
				{
					Players: []*enginev1.TeamMember{
						{SlotNumber: 1, DisplayName: "Alice", JerseyNumber: 0},
						{SlotNumber: 2, DisplayName: "Bob", JerseyNumber: 1},
						{SlotNumber: 5, DisplayName: "Dave", JerseyNumber: 5},
					},
				},
			},
		},
	})

	// Drain from eventsChan
	var allEvents []*telemetry.LobbySessionEvent
loop:
	for {
		select {
		case events := <-detector.EventsChan():
			allEvents = append(allEvents, events...)
		default:
			break loop
		}
	}

	if len(allEvents) != 3 {
		t.Fatalf("expected 3 PlayerJoined events via eventsChan, got %d", len(allEvents))
	}

	names := make(map[string]bool)
	for _, e := range allEvents {
		joined := e.GetPlayerJoined()
		if joined == nil {
			t.Fatalf("expected PlayerJoined, got %T", e.Event)
		}
		names[joined.Player.GetDisplayName()] = true
	}

	for _, want := range []string{"Alice", "Bob", "Dave"} {
		if !names[want] {
			t.Errorf("missing PlayerJoined event for %s", want)
		}
	}
}

// empty_buffer_bug verification.
// The existing TestEmptyFrameBufferBug tests pass, confirming the bug was fixed
// (the code uses frameCount instead of len(frameBuffer) for the early-return check).
// This test further verifies that the fix is correct by exercising the synchronous
// path with no frames added.
func TestEmptyBufferBug_DetectEventsSafeWithNilFrame(t *testing.T) {
	// Create a sensor that panics if called with nil frame
	var calledWithNil bool
	panicSensor := &testSensor{
		onAddFrame: func(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
			if frame == nil {
				calledWithNil = true
			}
			return nil
		},
	}

	detector := New(
		WithSynchronousProcessing(),
		WithFrameBufferSize(5),
		WithSensors(panicSensor),
	)
	defer detector.Stop()

	// Call detectEvents with empty buffer (frameCount == 0)
	eventBuf := make([]*telemetry.LobbySessionEvent, 0)
	detector.detectEvents(eventBuf)

	if calledWithNil {
		t.Error("BUG: sensor was called with nil frame when buffer is empty (frameCount == 0)")
	}

	// Verify lastFrame returns nil
	if detector.lastFrame() != nil {
		t.Error("expected lastFrame to return nil with empty buffer")
	}

	// Verify frameCount is 0
	if detector.frameCount != 0 {
		t.Errorf("frameCount = %d, want 0", detector.frameCount)
	}

	// After adding a frame, detectEvents should work normally
	detector.addFrameToBuffer(&telemetry.LobbySessionStateFrame{
		FrameIndex: 1,
		Session:    &enginev1.SessionResponse{GameStatus: "playing"},
	})

	// Should not panic
	detector.detectEvents(eventBuf)
}

// PlayerJoinSensor: verify drain loop doesn't miss events when multiple
// players from different teams join in the same frame.
func TestPlayerJoinSensor_MultipleTeamsJoin(t *testing.T) {
	sensor := NewPlayerJoinSensor()

	// Initialize
	initFrame := &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{
				{Players: []*enginev1.TeamMember{}},
				{Players: []*enginev1.TeamMember{}},
			},
		},
	}
	sensor.AddFrame(initFrame)

	// Two teams, each with players joining
	joinFrame := &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{
				{
					Players: []*enginev1.TeamMember{
						{SlotNumber: 1, DisplayName: "Blue1", JerseyNumber: 0},
					},
				},
				{
					Players: []*enginev1.TeamMember{
						{SlotNumber: 5, DisplayName: "Orange1", JerseyNumber: 5},
						{SlotNumber: 6, DisplayName: "Orange2", JerseyNumber: 6},
					},
				},
			},
		},
	}

	var events []*telemetry.LobbySessionEvent
	f := joinFrame
	for {
		event := sensor.AddFrame(f)
		if event == nil {
			break
		}
		events = append(events, event)
		f = nil
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 PlayerJoined events (cross-team), got %d", len(events))
	}

	// Check team assignments via role
	for _, e := range events {
		pj := e.GetPlayerJoined()
		slot := pj.Player.GetSlotNumber()
		if slot == 1 && pj.Role != telemetry.Role_ROLE_BLUE_TEAM {
			t.Errorf("slot %d should be BLUE_TEAM, got %v", slot, pj.Role)
		}
		if (slot == 5 || slot == 6) && pj.Role != telemetry.Role_ROLE_ORANGE_TEAM {
			t.Errorf("slot %d should be ORANGE_TEAM, got %v", slot, pj.Role)
		}
	}
}

// GoalScoredSensor: verify that GoalScored returns nil when frame has nil session.
func TestGoalScoredSensor_NilFrameReturnsNil(t *testing.T) {
	sensor := NewGoalScoredSensor()
	if event := sensor.AddFrame(nil); event != nil {
		t.Errorf("expected nil for nil frame, got %v", event)
	}
}

// GoalScoredSensor: verify no event when LastScore goes from non-nil to nil.
func TestGoalScoredSensor_LastScoreCleared(t *testing.T) {
	sensor := NewGoalScoredSensor()

	// First frame: has last score
	sensor.AddFrame(&telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			LastScore: &enginev1.LastScore{PersonScored: "P1"},
		},
	})

	// Second frame: no last score
	frame2 := &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{},
	}
	event := sensor.AddFrame(frame2)

	// When LastScore becomes nil, the sensor stores nil as prevLastScore
	// and returns nil (no new event).
	if event != nil {
		t.Errorf("expected nil when LastScore cleared, got %v", event)
	}
}

// Empty buffer: verify the detectEvents path doesn't inadvertently
// produce events when the ring buffer has frames but no sensors detected anything.
func TestDetectEvents_NoFalseEvents(t *testing.T) {
	sensor := NewPlayerJoinSensor()

	detector := New(
		WithSynchronousProcessing(),
		WithFrameBufferSize(5),
		WithSensors(sensor),
	)
	defer detector.Stop()

	// First frame: init with no players
	detector.addFrameToBuffer(&telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{{}},
		},
	})

	// Second frame: still no players
	detector.addFrameToBuffer(&telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{{}},
		},
	})

	eventBuf := make([]*telemetry.LobbySessionEvent, 0)
	result := detector.detectEvents(eventBuf)

	if len(result) != 0 {
		t.Errorf("expected 0 events when no players join, got %d", len(result))
	}
}
