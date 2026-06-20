package events

import (
	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
)

// playerStatSnapshot holds the stat values for a player
type playerStatSnapshot struct {
	goals         int32
	saves         int32
	stuns         int32
	passes        int32
	catches       int32
	steals        int32
	blocks        int32
	interceptions int32
	assists       int32
	shotsTaken    int32
	points        int32
}

func snapshotFromStats(stats *enginev1.PlayerStats) playerStatSnapshot {
	if stats == nil {
		return playerStatSnapshot{}
	}
	return playerStatSnapshot{
		goals:         stats.GetGoals(),
		saves:         stats.GetSaves(),
		stuns:         stats.GetStuns(),
		passes:        stats.GetPasses(),
		catches:       stats.GetCatches(),
		steals:        stats.GetSteals(),
		blocks:        stats.GetBlocks(),
		interceptions: stats.GetInterceptions(),
		assists:       stats.GetAssists(),
		shotsTaken:    stats.GetShotsTaken(),
		points:        stats.GetPoints(),
	}
}

// StatEventSensor detects all stat-based events for players
type StatEventSensor struct {
	prevStats map[int32]playerStatSnapshot // keyed by slot number
	// Queue of pending events (since we can only return one at a time)
	pendingEvents []*telemetry.LobbySessionEvent
	// Track previous possessor for steal attribution
	prevPossessorSlot int32
	initialized       bool
}

// NewStatEventSensor creates a new StatEventSensor
func NewStatEventSensor() *StatEventSensor {
	return &StatEventSensor{
		prevStats:         make(map[int32]playerStatSnapshot),
		pendingEvents:     make([]*telemetry.LobbySessionEvent, 0),
		prevPossessorSlot: -1,
		initialized:       false,
	}
}

// AddFrame processes a frame and returns stat events if detected
func (s *StatEventSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	// Return any pending events first
	if len(s.pendingEvents) > 0 {
		event := s.pendingEvents[0]
		s.pendingEvents = s.pendingEvents[1:]
		return event
	}

	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	// Find current possessor before processing stats
	currentPossessorSlot := findPossessorSlotFromSession(frame.GetSession())

	// Collect all stat changes
	for _, team := range frame.GetSession().GetTeams() {
		for _, player := range team.GetPlayers() {
			slot := player.GetSlotNumber()
			current := snapshotFromStats(player.GetStats())
			prev, existed := s.prevStats[slot]

			if existed {
				// Check for stat increases and generate events
				s.checkStatChanges(slot, prev, current, s.prevPossessorSlot)
			}

			s.prevStats[slot] = current
		}
	}

	// Update previous possessor for next frame
	if !s.initialized {
		s.initialized = true
	}
	s.prevPossessorSlot = currentPossessorSlot

	// Return first pending event if any were generated
	if len(s.pendingEvents) > 0 {
		event := s.pendingEvents[0]
		s.pendingEvents = s.pendingEvents[1:]
		return event
	}

	return nil
}

// Reset clears internal state for a new session.
func (s *StatEventSensor) Reset() {
	s.prevStats = make(map[int32]playerStatSnapshot)
	s.pendingEvents = nil
	s.prevPossessorSlot = -1
	s.initialized = false
}

// findPossessorSlotFromSession finds the slot of the player who has possession, returns -1 if none
func findPossessorSlotFromSession(session *enginev1.SessionResponse) int32 {
	for _, team := range session.GetTeams() {
		for _, player := range team.GetPlayers() {
			if player.GetHasPossession() {
				return player.GetSlotNumber()
			}
		}
	}
	return -1
}

// checkStatChanges compares stats and queues events for any increases
func (s *StatEventSensor) checkStatChanges(slot int32, prev, current playerStatSnapshot, prevPossessorSlot int32) {
	// Goals
	if current.goals > prev.goals {
		goalCount := current.goals - prev.goals
		pointsScored := current.points - prev.points
		if pointsScored <= 0 {
			pointsScored = 2 * goalCount // Default to 2 points per goal
		}
		pointsPerGoal := pointsScored / goalCount
		remainder := pointsScored % goalCount
		for i := int32(0); i < goalCount; i++ {
			pts := pointsPerGoal
			if i == 0 {
				pts += remainder // first goal gets the remainder
			}
			s.pendingEvents = append(s.pendingEvents, &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_PlayerGoal{
					PlayerGoal: &telemetry.PlayerGoal{
						PlayerSlot: slot,
						TotalGoals: prev.goals + i + 1,
						Points:     pts,
					},
				},
			})
		}
	}

	// Saves
	if current.saves > prev.saves {
		for i := int32(0); i < current.saves-prev.saves; i++ {
			s.pendingEvents = append(s.pendingEvents, &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_PlayerSave{
					PlayerSave: &telemetry.PlayerSave{
						PlayerSlot: slot,
						TotalSaves: prev.saves + i + 1,
					},
				},
			})
		}
	}

	// Stuns
	if current.stuns > prev.stuns {
		for i := int32(0); i < current.stuns-prev.stuns; i++ {
			s.pendingEvents = append(s.pendingEvents, &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_PlayerStun{
					PlayerStun: &telemetry.PlayerStun{
						PlayerSlot: slot,
						TotalStuns: prev.stuns + i + 1,
					},
				},
			})
		}
	}

	// Passes
	if current.passes > prev.passes {
		for i := int32(0); i < current.passes-prev.passes; i++ {
			s.pendingEvents = append(s.pendingEvents, &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_PlayerPass{
					PlayerPass: &telemetry.PlayerPass{
						PlayerSlot:  slot,
						TotalPasses: prev.passes + i + 1,
					},
				},
			})
		}
	}

	// Steals
	if current.steals > prev.steals {
		for i := int32(0); i < current.steals-prev.steals; i++ {
			s.pendingEvents = append(s.pendingEvents, &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_PlayerSteal{
					PlayerSteal: &telemetry.PlayerSteal{
						PlayerSlot:       slot,
						TotalSteals:      prev.steals + i + 1,
						VictimPlayerSlot: prevPossessorSlot,
					},
				},
			})
		}
	}

	// Blocks
	if current.blocks > prev.blocks {
		for i := int32(0); i < current.blocks-prev.blocks; i++ {
			s.pendingEvents = append(s.pendingEvents, &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_PlayerBlock{
					PlayerBlock: &telemetry.PlayerBlock{
						PlayerSlot:  slot,
						TotalBlocks: prev.blocks + i + 1,
					},
				},
			})
		}
	}

	// Interceptions
	if current.interceptions > prev.interceptions {
		for i := int32(0); i < current.interceptions-prev.interceptions; i++ {
			s.pendingEvents = append(s.pendingEvents, &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_PlayerInterception{
					PlayerInterception: &telemetry.PlayerInterception{
						PlayerSlot:         slot,
						TotalInterceptions: prev.interceptions + i + 1,
					},
				},
			})
		}
	}

	// Assists
	if current.assists > prev.assists {
		for i := int32(0); i < current.assists-prev.assists; i++ {
			s.pendingEvents = append(s.pendingEvents, &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_PlayerAssist{
					PlayerAssist: &telemetry.PlayerAssist{
						PlayerSlot:   slot,
						TotalAssists: prev.assists + i + 1,
					},
				},
			})
		}
	}

	// Shots Taken
	if current.shotsTaken > prev.shotsTaken {
		for i := int32(0); i < current.shotsTaken-prev.shotsTaken; i++ {
			s.pendingEvents = append(s.pendingEvents, &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_PlayerShotTaken{
					PlayerShotTaken: &telemetry.PlayerShotTaken{
						PlayerSlot: slot,
						TotalShots: prev.shotsTaken + i + 1,
					},
				},
			})
		}
	}
}
