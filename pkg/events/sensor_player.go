package events

import (
	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
)

// PlayerJoinSensor detects when players join the session
type PlayerJoinSensor struct {
	previousPlayers map[int32]playerInfo // keyed by slot number
	pendingEvents   []*telemetry.LobbySessionEvent
}

// NewPlayerJoinSensor creates a new PlayerJoinSensor
func NewPlayerJoinSensor() *PlayerJoinSensor {
	return &PlayerJoinSensor{
		previousPlayers: make(map[int32]playerInfo),
	}
}

// AddFrame processes a frame and returns a PlayerJoined event if detected.
// Must be called repeatedly with the same frame until nil is returned to
// drain all events when multiple players join in a single frame.
func (s *PlayerJoinSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	// Drain pending events before processing a new frame.
	if len(s.pendingEvents) > 0 {
		event := s.pendingEvents[0]
		s.pendingEvents = s.pendingEvents[1:]
		return event
	}

	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	currentPlayers := extractPlayersMap(frame.GetSession())

	// Find all new players (in current but not in previous).
	for slot, info := range currentPlayers {
		if _, existed := s.previousPlayers[slot]; !existed {
			s.pendingEvents = append(s.pendingEvents, &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_PlayerJoined{
					PlayerJoined: &telemetry.PlayerJoined{
						Player: info.player,
						Role:   determinePlayerRole(info.player, info.teamIdx),
					},
				},
			})
		}
	}

	s.previousPlayers = currentPlayers

	// Return first pending event if any were generated.
	if len(s.pendingEvents) > 0 {
		event := s.pendingEvents[0]
		s.pendingEvents = s.pendingEvents[1:]
		return event
	}

	return nil
}

// Reset clears internal state for a new session.
func (s *PlayerJoinSensor) Reset() {
	s.previousPlayers = make(map[int32]playerInfo)
	s.pendingEvents = nil
}

// PlayerLeaveSensor detects when players leave the session
type PlayerLeaveSensor struct {
	previousPlayers map[int32]playerInfo
	pendingEvents   []*telemetry.LobbySessionEvent
}

// NewPlayerLeaveSensor creates a new PlayerLeaveSensor
func NewPlayerLeaveSensor() *PlayerLeaveSensor {
	return &PlayerLeaveSensor{
		previousPlayers: make(map[int32]playerInfo),
	}
}

// AddFrame processes a frame and returns a PlayerLeft event if detected.
// Must be called repeatedly with the same frame until nil is returned to
// drain all events when multiple players leave in a single frame.
func (s *PlayerLeaveSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	// Drain pending events before processing a new frame.
	if len(s.pendingEvents) > 0 {
		event := s.pendingEvents[0]
		s.pendingEvents = s.pendingEvents[1:]
		return event
	}

	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	currentPlayers := extractPlayersMap(frame.GetSession())

	// Find all missing players (in previous but not in current).
	for slot, info := range s.previousPlayers {
		if _, exists := currentPlayers[slot]; !exists {
			s.pendingEvents = append(s.pendingEvents, &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_PlayerLeft{
					PlayerLeft: &telemetry.PlayerLeft{
						PlayerSlot:  slot,
						DisplayName: info.player.GetDisplayName(),
					},
				},
			})
		}
	}

	s.previousPlayers = currentPlayers

	// Return first pending event if any were generated.
	if len(s.pendingEvents) > 0 {
		event := s.pendingEvents[0]
		s.pendingEvents = s.pendingEvents[1:]
		return event
	}

	return nil
}

// Reset clears internal state for a new session.
func (s *PlayerLeaveSensor) Reset() {
	s.previousPlayers = make(map[int32]playerInfo)
	s.pendingEvents = nil
}

// PlayerTeamSwitchSensor detects when players switch teams
type PlayerTeamSwitchSensor struct {
	previousPlayers map[int32]playerInfo
	pendingEvents   []*telemetry.LobbySessionEvent
}

// NewPlayerTeamSwitchSensor creates a new PlayerTeamSwitchSensor
func NewPlayerTeamSwitchSensor() *PlayerTeamSwitchSensor {
	return &PlayerTeamSwitchSensor{
		previousPlayers: make(map[int32]playerInfo),
	}
}

// AddFrame processes a frame and returns a PlayerSwitchedTeam event if detected.
// Must be called repeatedly with the same frame until nil is returned to
// drain all events when multiple players switch teams in a single frame.
func (s *PlayerTeamSwitchSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	// Drain pending events before processing a new frame.
	if len(s.pendingEvents) > 0 {
		event := s.pendingEvents[0]
		s.pendingEvents = s.pendingEvents[1:]
		return event
	}

	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	currentPlayers := extractPlayersMap(frame.GetSession())

	// Check for team switches (same slot, different team).
	for slot, currInfo := range currentPlayers {
		if prevInfo, existed := s.previousPlayers[slot]; existed {
			prevRole := determinePlayerRole(prevInfo.player, prevInfo.teamIdx)
			currRole := determinePlayerRole(currInfo.player, currInfo.teamIdx)
			if prevRole != currRole {
				s.pendingEvents = append(s.pendingEvents, &telemetry.LobbySessionEvent{
					Event: &telemetry.LobbySessionEvent_PlayerSwitchedTeam{
						PlayerSwitchedTeam: &telemetry.PlayerSwitchedTeam{
							PlayerSlot: slot,
							NewRole:    currRole,
							PrevRole:   prevRole,
						},
					},
				})
			}
		}
	}

	s.previousPlayers = currentPlayers

	// Return first pending event if any were generated.
	if len(s.pendingEvents) > 0 {
		event := s.pendingEvents[0]
		s.pendingEvents = s.pendingEvents[1:]
		return event
	}

	return nil
}

// Reset clears internal state for a new session.
func (s *PlayerTeamSwitchSensor) Reset() {
	s.previousPlayers = make(map[int32]playerInfo)
	s.pendingEvents = nil
}

// EmoteSensor detects when players play emotes
type EmoteSensor struct {
	previousEmoteStates map[int32]bool // keyed by slot number
	pendingEvents       []*telemetry.LobbySessionEvent
}

// NewEmoteSensor creates a new EmoteSensor
func NewEmoteSensor() *EmoteSensor {
	return &EmoteSensor{
		previousEmoteStates: make(map[int32]bool),
	}
}

// AddFrame processes a frame and returns an EmotePlayed event if detected.
// Must be called repeatedly with the same frame until nil is returned to
// drain all events when multiple emotes start in a single frame.
func (s *EmoteSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	// Drain pending events before processing a new frame.
	if len(s.pendingEvents) > 0 {
		event := s.pendingEvents[0]
		s.pendingEvents = s.pendingEvents[1:]
		return event
	}

	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	for _, team := range frame.GetSession().GetTeams() {
		for _, player := range team.GetPlayers() {
			slot := player.GetSlotNumber()
			isPlaying := player.GetIsEmotePlaying()
			wasPlaying := s.previousEmoteStates[slot]

			// Detect transition from not playing to playing.
			if isPlaying && !wasPlaying {
				s.pendingEvents = append(s.pendingEvents, &telemetry.LobbySessionEvent{
					Event: &telemetry.LobbySessionEvent_EmotePlayed{
						EmotePlayed: &telemetry.EmotePlayed{
							PlayerSlot: slot,
							Emote:      telemetry.EmotePlayed_EMOTE_TYPE_PRIMARY,
						},
					},
				})
			}
			s.previousEmoteStates[slot] = isPlaying
		}
	}

	// Return first pending event if any were generated.
	if len(s.pendingEvents) > 0 {
		event := s.pendingEvents[0]
		s.pendingEvents = s.pendingEvents[1:]
		return event
	}

	return nil
}

// Reset clears internal state for a new session.
func (s *EmoteSensor) Reset() {
	s.previousEmoteStates = make(map[int32]bool)
	s.pendingEvents = nil
}

// playerInfo holds a team member and the index of the team it belongs to.
type playerInfo struct {
	player  *enginev1.TeamMember
	teamIdx int // 0 = blue, 1 = orange
}

// extractPlayersMap extracts all players from a session into a map keyed by slot,
// preserving which team array each player belongs to.
func extractPlayersMap(session *enginev1.SessionResponse) map[int32]playerInfo {
	players := make(map[int32]playerInfo)
	for teamIdx, team := range session.GetTeams() {
		for _, player := range team.GetPlayers() {
			players[player.GetSlotNumber()] = playerInfo{
				player:  player,
				teamIdx: teamIdx,
			}
		}
	}
	return players
}

// determinePlayerRole determines a player's role from the team index.
// teamIdx 0 = blue, 1 = orange; spectators are detected by jersey number -1.
func determinePlayerRole(player *enginev1.TeamMember, teamIdx int) telemetry.Role {
	if player == nil {
		return telemetry.Role_ROLE_UNSPECIFIED
	}

	// Spectators have jersey number -1
	if player.GetJerseyNumber() == -1 {
		return telemetry.Role_ROLE_SPECTATOR
	}

	if teamIdx == 0 {
		return telemetry.Role_ROLE_BLUE_TEAM
	}
	return telemetry.Role_ROLE_ORANGE_TEAM
}
