package conversion

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestVocabularyCountsWhatTheTablesLose is the VOCAB-001 guard: an engine string
// no table knows becomes *_UNSPECIFIED and renders back as "", so the value is
// gone. The counter is what makes that loss visible.
func TestVocabularyCountsWhatTheTablesLose(t *testing.T) {
	t.Parallel()

	mapper := &FrameMapper{BaseTime: time.Now(), vocab: &vocabulary{}}

	frame := func(status, pause, goal string) *telemetryv1.LobbySessionStateFrame {
		return &telemetryv1.LobbySessionStateFrame{
			Timestamp: timestamppb.New(mapper.BaseTime),
			Session: &enginev1.SessionResponse{
				GameStatus: status,
				Pause:      &enginev1.PauseState{PausedState: pause},
			},
			Events: []*telemetryv1.LobbySessionEvent{{
				Event: &telemetryv1.LobbySessionEvent_GoalScored{
					GoalScored: &telemetryv1.GoalScored{
						ScoreDetails: &enginev1.LastScore{GoalType: goal},
					},
				},
			}},
		}
	}

	// Two frames carrying the same unknown status, one carrying known values.
	mapper.MapFrame(frame("quantum_overtime", "paused", "SLAM DUNK"))
	mapper.MapFrame(frame("quantum_overtime", "hyperpaused", "MOON SHOT"))
	mapper.MapFrame(frame("playing", "paused", "SLAM DUNK"))

	got := mapper.Unmapped()
	want := []UnmappedValue{
		{Field: "game_status", Value: "quantum_overtime", Count: 2},
		{Field: "goal_type", Value: "MOON SHOT", Count: 1},
		{Field: "paused_state", Value: "hyperpaused", Count: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d unmapped values, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("unmapped[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestVocabularyIgnoresTheEmptyString pins the deliberate exclusion. game_status
// is empty in 10 of 25 measured dal1 captures and round-trips correctly
// (unmapped -> UNSPECIFIED -> ""), so counting it would report a loss that is
// not real on nearly half the corpus.
func TestVocabularyIgnoresTheEmptyString(t *testing.T) {
	t.Parallel()

	mapper := &FrameMapper{BaseTime: time.Now(), vocab: &vocabulary{}}
	mapper.MapFrame(&telemetryv1.LobbySessionStateFrame{
		Timestamp: timestamppb.New(mapper.BaseTime),
		Session:   &enginev1.SessionResponse{GameStatus: ""},
	})

	if got := mapper.Unmapped(); len(got) != 0 {
		t.Errorf("empty game_status recorded as unmapped: %+v", got)
	}
}

// TestVocabularyIsSilentOnAKnownCapture checks the counter does not cry wolf:
// the committed sample uses only vocabulary the tables already cover, so a
// clean conversion must report nothing.
func TestVocabularyIsSilentOnAKnownCapture(t *testing.T) {
	t.Parallel()

	const src = "../../testdata/sample.echoreplay"
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no sample echoreplay: %v", err)
	}

	result, err := ConvertFile(src, filepath.Join(t.TempDir(), "out.tape"))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(result.UnmappedValues) != 0 {
		t.Errorf("committed sample reported unmapped values: %+v", result.UnmappedValues)
	}
}

// TestNilVocabularyRecordsNothing covers the exported entry points, which have
// nowhere to report and pass nil. They must map identically, not panic.
func TestNilVocabularyRecordsNothing(t *testing.T) {
	t.Parallel()

	var v *vocabulary
	if got := v.gameStatus("quantum_overtime"); got != capturepb.GameStatus_GAME_STATUS_UNSPECIFIED {
		t.Errorf("gameStatus = %v, want UNSPECIFIED", got)
	}
	if got := v.matchType("Echo_Arena_Private"); got != capturepb.MatchType_MATCH_TYPE_PRIVATE {
		t.Errorf("matchType = %v, want PRIVATE", got)
	}
	if got := v.sorted(); got != nil {
		t.Errorf("sorted() = %+v, want nil", got)
	}
}
