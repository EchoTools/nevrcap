package codec

import (
	"archive/zip"
	"os"
	"testing"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestFixBooleanInt32Fields verifies that the EchoVR game engine's habit of
// emitting the *_team_restart_request fields as JSON booleans (false/true) is
// coerced to the int32 values (0/1) the generated proto expects.
//
// Without this fix, protojson.Unmarshal rejects the boolean value with
// "invalid value for int32 field blue_team_restart_request: false" and every
// frame in a Spark-format .echoreplay fails to parse, yielding 0 frames and a
// misleading "read first frame for header: EOF".
func TestFixBooleanInt32Fields(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "blue false",
			input:    `{"blue_team_restart_request":false}`,
			expected: `{"blue_team_restart_request":0}`,
		},
		{
			name:     "blue true",
			input:    `{"blue_team_restart_request":true}`,
			expected: `{"blue_team_restart_request":1}`,
		},
		{
			name:     "orange false",
			input:    `{"orange_team_restart_request":false}`,
			expected: `{"orange_team_restart_request":0}`,
		},
		{
			name:     "orange true",
			input:    `{"orange_team_restart_request":true}`,
			expected: `{"orange_team_restart_request":1}`,
		},
		{
			name:     "both present",
			input:    `{"blue_team_restart_request":false,"orange_team_restart_request":true}`,
			expected: `{"blue_team_restart_request":0,"orange_team_restart_request":1}`,
		},
		{
			name:     "already numeric is untouched",
			input:    `{"blue_team_restart_request":0,"orange_team_restart_request":1}`,
			expected: `{"blue_team_restart_request":0,"orange_team_restart_request":1}`,
		},
		{
			name:     "numeric nonzero untouched",
			input:    `{"orange_team_restart_request":2}`,
			expected: `{"orange_team_restart_request":2}`,
		},
		{
			name:     "no field present is untouched",
			input:    `{"game_status":"playing","game_clock":3.91}`,
			expected: `{"game_status":"playing","game_clock":3.91}`,
		},
		{
			name:     "embedded in larger object",
			input:    `{"sessionip":"108.237.150.148","blue_team_restart_request":false,"orange_team_restart_request":false,"game_status":"playing"}`,
			expected: `{"sessionip":"108.237.150.148","blue_team_restart_request":0,"orange_team_restart_request":0,"game_status":"playing"}`,
		},
		{
			name:     "left_shoulder_pressed double false",
			input:    `{"left_shoulder_pressed":false}`,
			expected: `{"left_shoulder_pressed":0}`,
		},
		{
			name:     "left_shoulder_pressed2 distinct from base name",
			input:    `{"left_shoulder_pressed":false,"left_shoulder_pressed2":true}`,
			expected: `{"left_shoulder_pressed":0,"left_shoulder_pressed2":1}`,
		},
		{
			name:     "right shoulder fields",
			input:    `{"right_shoulder_pressed":false,"right_shoulder_pressed2":false}`,
			expected: `{"right_shoulder_pressed":0,"right_shoulder_pressed2":0}`,
		},
		{
			name:     "shoulder already numeric float untouched",
			input:    `{"left_shoulder_pressed":0.0,"right_shoulder_pressed":1.0}`,
			expected: `{"left_shoulder_pressed":0.0,"right_shoulder_pressed":1.0}`,
		},
		{
			name:     "genuine boolean fields are left alone",
			input:    `{"possession":false,"blocking":false,"stunned":true,"private_match":true,"tournament_match":false,"contested":false,"is_emote_playing":false,"invulnerable":false}`,
			expected: `{"possession":false,"blocking":false,"stunned":true,"private_match":true,"tournament_match":false,"contested":false,"is_emote_playing":false,"invulnerable":false}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FixBooleanInt32Fields([]byte(tt.input))
			if string(result) != tt.expected {
				t.Errorf("FixBooleanInt32Fields() = %q, want %q", string(result), tt.expected)
			}
		})
	}
}

// TestFixBooleanInt32FieldsRoundTripsThroughProtojson is the real acceptance
// test: a session JSON with boolean restart_request fields (as the Spark
// recorder emits) must unmarshal successfully after the fix.
func TestFixBooleanInt32FieldsRoundTripsThroughProtojson(t *testing.T) {
	raw := []byte(`{"blue_team_restart_request":false,"orange_team_restart_request":true,"left_shoulder_pressed":false,"right_shoulder_pressed2":true,"game_status":"playing"}`)

	um := &protojson.UnmarshalOptions{DiscardUnknown: true}

	// Sanity: raw boolean form must fail (this is the bug we are fixing).
	if err := um.Unmarshal(raw, &enginev1.SessionResponse{}); err == nil {
		t.Fatal("expected raw boolean restart_request to fail unmarshal, but it succeeded; proto schema may have changed")
	}

	fixed := FixBooleanInt32Fields(raw)
	sr := &enginev1.SessionResponse{}
	if err := um.Unmarshal(fixed, sr); err != nil {
		t.Fatalf("after FixBooleanInt32Fields, unmarshal failed: %v", err)
	}
	if sr.GetBlueTeamRestartRequest() != 0 {
		t.Errorf("blue restart = %d, want 0", sr.GetBlueTeamRestartRequest())
	}
	if sr.GetOrangeTeamRestartRequest() != 1 {
		t.Errorf("orange restart = %d, want 1", sr.GetOrangeTeamRestartRequest())
	}
}

// TestEchoReplayReader_BooleanRestartRequest is the regression test for the
// "read first frame for header: EOF" failure on Spark-format .echoreplay files:
// a session line whose restart_request fields are JSON booleans must now be
// read as a valid frame through BOTH the ReadFrames and ReadFrameTo paths,
// rather than being silently skipped.
func TestEchoReplayReader_BooleanRestartRequest(t *testing.T) {
	tmpFile := t.TempDir() + "/boolean_restart.echoreplay"

	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("boolean_restart")
	if err != nil {
		t.Fatal(err)
	}
	// Spark-format line: timestamp \t session-json (booleans), no bones.
	w.Write([]byte(`2026/06/11 02:41:46.838` + "\t" +
		`{"session_id":"S1","blue_team_restart_request":false,"orange_team_restart_request":true,"game_status":"playing"}` + "\n"))
	zw.Close()
	f.Close()

	// Path 1: ReadFrames.
	reader, err := NewEchoReplayReader(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create reader: %v", err)
	}
	frames, err := reader.ReadFrames()
	if err != nil {
		t.Fatalf("ReadFrames failed: %v", err)
	}
	if reader.SkippedFrames() != 0 {
		t.Errorf("expected 0 skipped frames, got %d", reader.SkippedFrames())
	}
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	if got := frames[0].Session.GetOrangeTeamRestartRequest(); got != 1 {
		t.Errorf("orange restart = %d, want 1", got)
	}
	if got := frames[0].Session.GetBlueTeamRestartRequest(); got != 0 {
		t.Errorf("blue restart = %d, want 0", got)
	}
	reader.Close()

	// Path 2: ReadFrameTo (the allocation-reuse hot path).
	reader2, err := NewEchoReplayReader(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create reader2: %v", err)
	}
	defer reader2.Close()
	frame := &telemetry.LobbySessionStateFrame{}
	ok, err := reader2.ReadFrameTo(frame)
	if err != nil {
		t.Fatalf("ReadFrameTo failed: %v", err)
	}
	if !ok {
		t.Fatal("ReadFrameTo returned ok=false on a valid frame")
	}
	if got := frame.Session.GetOrangeTeamRestartRequest(); got != 1 {
		t.Errorf("ReadFrameTo orange restart = %d, want 1", got)
	}
}
