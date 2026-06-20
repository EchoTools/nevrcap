package processing

import (
	"testing"
	"time"
)

func TestProcessAndDetectEvents_NoBones(t *testing.T) {
	processor := New()

	sessionData := createTestSessionData(t)
	// Empty bones data should result in nil PlayerBones on the frame.
	frame, err := processor.ProcessAndDetectEvents(sessionData, nil, time.Now())
	if err != nil {
		t.Fatalf("ProcessAndDetectEvents: %v", err)
	}

	if frame.PlayerBones != nil {
		t.Error("expected nil PlayerBones when no bones data provided")
	}
}

func TestProcessAndDetectEvents_EmptyBones(t *testing.T) {
	processor := New()

	sessionData := createTestSessionData(t)
	// Empty byte slice should also result in nil PlayerBones.
	frame, err := processor.ProcessAndDetectEvents(sessionData, []byte{}, time.Now())
	if err != nil {
		t.Fatalf("ProcessAndDetectEvents: %v", err)
	}

	if frame.PlayerBones != nil {
		t.Error("expected nil PlayerBones when bones data is empty")
	}
}

func TestProcessAndDetectEvents_WithBones(t *testing.T) {
	processor := New()

	sessionData := createTestSessionData(t)
	bonesData := createTestUserBonesData(t)

	frame, err := processor.ProcessAndDetectEvents(sessionData, bonesData, time.Now())
	if err != nil {
		t.Fatalf("ProcessAndDetectEvents: %v", err)
	}

	if frame.PlayerBones == nil {
		t.Error("expected non-nil PlayerBones when valid bones data provided")
	}
}
