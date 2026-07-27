package events

import (
	"testing"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
)

// TestSensorReset verifies that every stateful sensor implements Resettable
// and that calling Reset() returns it to a clean initial state so no stale
// events leak across sessions.
func TestSensorReset(t *testing.T) {
	t.Run("PlayerJoinSensor", func(t *testing.T) {
		s := NewPlayerJoinSensor()
		frame := frameWithPlayers(0, 1)
		drainSensor(s, frame)

		s.Reset()

		// After reset, re-feeding the same frame should emit joins again
		// because previousPlayers was cleared.
		events := drainSensor(s, frame)
		if len(events) != 2 {
			t.Fatalf("expected 2 PlayerJoined after reset, got %d", len(events))
		}
	})

	t.Run("PlayerLeaveSensor", func(t *testing.T) {
		s := NewPlayerLeaveSensor()
		frame := frameWithPlayers(0, 1)
		drainSensor(s, frame)

		s.Reset()

		// After reset, feeding empty frame should NOT emit leaves because
		// previousPlayers was cleared — sensor thinks nobody was there.
		events := drainSensor(s, frameWithPlayers())
		if len(events) != 0 {
			t.Fatalf("expected 0 PlayerLeft after reset, got %d", len(events))
		}
	})

	t.Run("PlayerTeamSwitchSensor", func(t *testing.T) {
		s := NewPlayerTeamSwitchSensor()
		frame := frameWithPlayers(0)
		drainSensor(s, frame)

		s.Reset()

		// After reset, same player should not trigger a switch.
		events := drainSensor(s, frame)
		if len(events) != 0 {
			t.Fatalf("expected 0 events after reset, got %d", len(events))
		}
	})

	t.Run("EmoteSensor", func(t *testing.T) {
		s := NewEmoteSensor()
		frame := frameWithEmote(0, true)
		drainSensor(s, frame)

		s.Reset()

		// After reset, same emote should fire again.
		events := drainSensor(s, frame)
		if len(events) != 1 {
			t.Fatalf("expected 1 EmotePlayed after reset, got %d", len(events))
		}
	})

	t.Run("DiscPossessionSensor", func(t *testing.T) {
		s := NewDiscPossessionSensor()
		frame := frameWithPossession(2)
		drainSensor(s, frame)

		s.Reset()

		// After reset, initialized should be false; first frame is silent.
		events := drainSensor(s, frame)
		if len(events) != 0 {
			t.Fatalf("expected 0 events after reset (re-init), got %d", len(events))
		}
	})

	t.Run("DiscThrownSensor", func(t *testing.T) {
		s := NewDiscThrownSensor()
		frame := frameWithThrow("Player1", 10.0)
		drainSensor(s, frame)

		s.Reset()

		// After reset, same throw should fire again.
		events := drainSensor(s, frame)
		if len(events) != 1 {
			t.Fatalf("expected 1 DiscThrown after reset, got %d", len(events))
		}
	})

	t.Run("DiscCaughtSensor", func(t *testing.T) {
		s := NewDiscCaughtSensor()
		frame := frameWithPossession(2)
		drainSensor(s, frame)

		s.Reset()

		// After reset, initialized is false; first frame silent.
		events := drainSensor(s, frame)
		if len(events) != 0 {
			t.Fatalf("expected 0 events after reset (re-init), got %d", len(events))
		}
	})

	t.Run("ScoreboardSensor", func(t *testing.T) {
		s := NewScoreboardSensor()
		frame := &telemetry.LobbySessionStateFrame{
			Session: &enginev1.SessionResponse{
				BluePoints: 3,
			},
		}
		drainSensor(s, frame)

		s.Reset()

		// After reset, initialized is false, so the next frame seeds again —
		// a new session must record its opening scoreboard.
		events := drainSensor(s, frame)
		if len(events) != 1 {
			t.Fatalf("expected 1 seed event after reset (re-init), got %d", len(events))
		}
		if got := events[0].GetScoreboardUpdated().GetBluePoints(); got != 3 {
			t.Errorf("seed blue_points = %d, want 3", got)
		}
	})

	t.Run("GoalScoredSensor", func(t *testing.T) {
		s := NewGoalScoredSensor()
		frame := &telemetry.LobbySessionStateFrame{
			Session: &enginev1.SessionResponse{
				LastScore: &enginev1.LastScore{
					PersonScored: "Player1",
					DiscSpeed:    15.0,
				},
			},
		}
		drainSensor(s, frame)

		s.Reset()

		// After reset, same score should fire again.
		events := drainSensor(s, frame)
		if len(events) != 1 {
			t.Fatalf("expected 1 GoalScored after reset, got %d", len(events))
		}
	})

	t.Run("RoundStartSensor", func(t *testing.T) {
		s := NewRoundStartSensor()
		drainSensor(s, createGameStateFrame("score", 0, 0))
		events := drainSensor(s, createGameStateFrame(GameStatusPlaying, 0, 0))
		if len(events) != 1 {
			t.Fatalf("expected 1 RoundStarted, got %d", len(events))
		}

		s.Reset()

		// After reset, prevGameStatus is "" so feeding score then playing
		// should fire again.
		drainSensor(s, createGameStateFrame("score", 0, 0))
		events = drainSensor(s, createGameStateFrame(GameStatusPlaying, 0, 0))
		if len(events) != 1 {
			t.Fatalf("expected 1 RoundStarted after reset, got %d", len(events))
		}
	})

	t.Run("RoundEndSensor", func(t *testing.T) {
		s := NewRoundEndSensor()
		drainSensor(s, createGameStateFrame(GameStatusPlaying, 0, 0))
		events := drainSensor(s, createGameStateFrame(GameStatusRoundOver, 1, 0))
		if len(events) != 1 {
			t.Fatalf("expected 1 RoundEnded, got %d", len(events))
		}

		s.Reset()

		// After reset, initialized is false; first frame silent.
		events = drainSensor(s, createGameStateFrame(GameStatusPlaying, 0, 0))
		if len(events) != 0 {
			t.Fatalf("expected 0 events after reset (re-init), got %d", len(events))
		}
	})

	t.Run("MatchEndSensor", func(t *testing.T) {
		s := NewMatchEndSensor()
		drainSensor(s, createGameStateFrame(GameStatusPlaying, 0, 0))
		drainSensor(s, createGameStateFrame(GameStatusPostMatch, 0, 0))

		s.Reset()

		// After reset, prevGameStatus is ""; feeding post_match should fire.
		drainSensor(s, createGameStateFrame(GameStatusPlaying, 0, 0))
		events := drainSensor(s, createGameStateFrame(GameStatusPostMatch, 0, 0))
		if len(events) != 1 {
			t.Fatalf("expected 1 MatchEnded after reset, got %d", len(events))
		}
	})

	t.Run("PauseSensor", func(t *testing.T) {
		s := NewPauseSensor()
		drainSensor(s, &telemetry.LobbySessionStateFrame{
			Session: &enginev1.SessionResponse{
				Pause: &enginev1.PauseState{PausedState: "none"},
			},
		})
		events := drainSensor(s, &telemetry.LobbySessionStateFrame{
			Session: &enginev1.SessionResponse{
				Pause: &enginev1.PauseState{PausedState: "paused"},
			},
		})
		if len(events) != 1 {
			t.Fatalf("expected 1 RoundPaused, got %d", len(events))
		}

		s.Reset()

		// After reset, prevPauseState is ""; feeding paused should fire.
		events = drainSensor(s, &telemetry.LobbySessionStateFrame{
			Session: &enginev1.SessionResponse{
				Pause: &enginev1.PauseState{PausedState: "paused"},
			},
		})
		if len(events) != 1 {
			t.Fatalf("expected 1 RoundPaused after reset, got %d", len(events))
		}
	})

	t.Run("StatEventSensor", func(t *testing.T) {
		s := NewStatEventSensor()
		frame1 := frameWithStats(0, 0)
		drainSensor(s, frame1)

		frame2 := frameWithStats(0, 1) // 1 goal
		events := drainSensor(s, frame2)
		if len(events) != 1 {
			t.Fatalf("expected 1 stat event, got %d", len(events))
		}

		s.Reset()

		// After reset, first frame is init again (no events).
		events = drainSensor(s, frame2)
		if len(events) != 0 {
			t.Fatalf("expected 0 events after reset (re-init), got %d", len(events))
		}
	})
}

// TestAllSensorsAreResettable verifies that every sensor type used in the
// codebase implements the Resettable interface at compile time.
func TestAllSensorsAreResettable(t *testing.T) {
	sensors := []Sensor{
		NewPlayerJoinSensor(),
		NewPlayerLeaveSensor(),
		NewPlayerTeamSwitchSensor(),
		NewEmoteSensor(),
		NewDiscPossessionSensor(),
		NewDiscThrownSensor(),
		NewDiscCaughtSensor(),
		NewScoreboardSensor(),
		NewGoalScoredSensor(),
		NewRoundStartSensor(),
		NewRoundEndSensor(),
		NewMatchEndSensor(),
		NewPauseSensor(),
		NewStatEventSensor(),
	}

	for _, s := range sensors {
		if _, ok := s.(Resettable); !ok {
			t.Errorf("%T does not implement Resettable", s)
		}
	}
}

// --- helpers ---

// drainSensor feeds the frame and drains all events the sensor returns.
func drainSensor(s Sensor, frame *telemetry.LobbySessionStateFrame) []*telemetry.LobbySessionEvent {
	var out []*telemetry.LobbySessionEvent
	f := frame
	for {
		ev := s.AddFrame(f)
		if ev == nil {
			break
		}
		out = append(out, ev)
		f = nil // subsequent calls drain pending events
	}
	return out
}

func frameWithPlayers(slots ...int32) *telemetry.LobbySessionStateFrame {
	var players []*enginev1.TeamMember
	for _, slot := range slots {
		players = append(players, &enginev1.TeamMember{
			SlotNumber:  slot,
			DisplayName: "P",
		})
	}
	return &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{
				{Players: players},
			},
		},
	}
}

func frameWithEmote(slot int32, playing bool) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{
				{Players: []*enginev1.TeamMember{
					{SlotNumber: slot, IsEmotePlaying: playing},
				}},
			},
		},
	}
}

func frameWithPossession(slot int32) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{
				{Players: []*enginev1.TeamMember{
					{SlotNumber: slot, HasPossession: true},
				}},
			},
		},
	}
}

func frameWithThrow(player string, speed float64) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			LastThrow: &enginev1.LastThrowInfo{
				ArmSpeed: speed,
			},
			Teams: []*enginev1.Team{
				{Players: []*enginev1.TeamMember{
					{SlotNumber: 0, DisplayName: player, HasPossession: true},
				}},
			},
		},
	}
}

func frameWithStats(slot int32, goals int32) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		Session: &enginev1.SessionResponse{
			Teams: []*enginev1.Team{
				{Players: []*enginev1.TeamMember{
					{
						SlotNumber: slot,
						Stats: &enginev1.PlayerStats{
							Goals: goals,
						},
					},
				}},
			},
		},
	}
}
