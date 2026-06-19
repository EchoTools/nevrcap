package events

import (
	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"google.golang.org/protobuf/proto"
)

// DiscPossessionSensor detects disc possession changes
type DiscPossessionSensor struct {
	prevPossessorSlot int32
	initialized       bool
}

// NewDiscPossessionSensor creates a new DiscPossessionSensor
func NewDiscPossessionSensor() *DiscPossessionSensor {
	return &DiscPossessionSensor{
		prevPossessorSlot: -1,
	}
}

// AddFrame processes a frame and returns a DiscPossessionChanged event if detected
func (s *DiscPossessionSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	currentSlot := findPossessorSlot(frame.GetSession())

	if !s.initialized {
		s.prevPossessorSlot = currentSlot
		s.initialized = true
		return nil
	}

	if currentSlot != s.prevPossessorSlot {
		prevSlot := s.prevPossessorSlot
		s.prevPossessorSlot = currentSlot
		return &telemetry.LobbySessionEvent{
			Event: &telemetry.LobbySessionEvent_DiscPossessionChanged{
				DiscPossessionChanged: &telemetry.DiscPossessionChanged{
					PlayerSlot:         currentSlot,
					PreviousPlayerSlot: prevSlot,
				},
			},
		}
	}

	return nil
}

// DiscThrownSensor detects when the disc is thrown using LastThrowInfo
type DiscThrownSensor struct {
	prevLastThrow *enginev1.LastThrowInfo
	prevPossessor int32
}

// NewDiscThrownSensor creates a new DiscThrownSensor
func NewDiscThrownSensor() *DiscThrownSensor {
	return &DiscThrownSensor{
		prevPossessor: -1,
	}
}

// AddFrame processes a frame and returns a DiscThrown event if detected
func (s *DiscThrownSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	session := frame.GetSession()
	lastThrow := session.GetLastThrow()
	currentPossessor := findPossessorSlot(session)

	// Track possession for attributing throws
	if s.prevPossessor == -1 {
		s.prevPossessor = currentPossessor
	}

	if lastThrow == nil {
		s.prevLastThrow = nil
		s.prevPossessor = currentPossessor
		return nil
	}

	// Detect new throw by comparing with previous
	if s.prevLastThrow == nil || !lastThrowEqual(s.prevLastThrow, lastThrow) {
		throwerSlot := s.prevPossessor
		if throwerSlot == -1 {
			throwerSlot = currentPossessor
		}

		s.prevLastThrow = lastThrow
		s.prevPossessor = currentPossessor

		return &telemetry.LobbySessionEvent{
			Event: &telemetry.LobbySessionEvent_DiscThrown{
				DiscThrown: &telemetry.DiscThrown{
					PlayerSlot:   throwerSlot,
					ThrowDetails: lastThrow,
				},
			},
		}
	}

	s.prevPossessor = currentPossessor
	return nil
}

// DiscCaughtSensor detects when a player catches the disc
type DiscCaughtSensor struct {
	prevPossessorSlot int32
	initialized       bool
}

// NewDiscCaughtSensor creates a new DiscCaughtSensor
func NewDiscCaughtSensor() *DiscCaughtSensor {
	return &DiscCaughtSensor{
		prevPossessorSlot: -1,
	}
}

// AddFrame processes a frame and returns a DiscCaught event if detected
func (s *DiscCaughtSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	currentSlot := findPossessorSlot(frame.GetSession())

	if !s.initialized {
		s.prevPossessorSlot = currentSlot
		s.initialized = true
		return nil
	}

	// A catch occurs when possession changes from no one (-1) to someone,
	// or from one player to another (not the same player)
	if currentSlot != -1 && s.prevPossessorSlot != currentSlot {
		// Only emit catch if there was a transition (disc was free or with someone else)
		if s.prevPossessorSlot == -1 || s.prevPossessorSlot != currentSlot {
			s.prevPossessorSlot = currentSlot
			return &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_DiscCaught{
					DiscCaught: &telemetry.DiscCaught{
						PlayerSlot: currentSlot,
					},
				},
			}
		}
	}

	s.prevPossessorSlot = currentSlot
	return nil
}

// findPossessorSlot finds the slot of the player who has possession, returns -1 if none
func findPossessorSlot(session *enginev1.SessionResponse) int32 {
	for _, team := range session.GetTeams() {
		for _, player := range team.GetPlayers() {
			if player.GetHasPossession() {
				return player.GetSlotNumber()
			}
		}
	}
	return -1
}

// lastThrowEqual compares two LastThrowInfo objects for equality using
// proto.Equal to ensure all 13 fields are compared.
func lastThrowEqual(a, b *enginev1.LastThrowInfo) bool {
	return proto.Equal(a, b)
}
