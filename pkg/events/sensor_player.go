package events

import (
	apigame "github.com/echotools/nevr-common/v4/gen/go/apigame/v1"
	"github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"
)

// PlayerJoinSensor detects when players join the session
type PlayerJoinSensor struct {
	previousPlayers map[int32]*apigame.TeamMember // keyed by slot number
}

// NewPlayerJoinSensor creates a new PlayerJoinSensor
func NewPlayerJoinSensor() *PlayerJoinSensor {
	return &PlayerJoinSensor{
		previousPlayers: make(map[int32]*apigame.TeamMember),
	}
}

// AddFrame processes a frame and returns a PlayerJoined event if detected
func (s *PlayerJoinSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	currentPlayers := extractPlayersMap(frame.GetSession())

	// Find new players (in current but not in previous)
	for slot, player := range currentPlayers {
		if _, existed := s.previousPlayers[slot]; !existed {
			// Update state before returning
			s.previousPlayers = currentPlayers
			return &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_PlayerJoined{
					PlayerJoined: &telemetry.PlayerJoined{
						Player: player,
						Role:   determinePlayerRole(player),
					},
				},
			}
		}
	}

	s.previousPlayers = currentPlayers
	return nil
}

// PlayerLeaveSensor detects when players leave the session
type PlayerLeaveSensor struct {
	previousPlayers map[int32]*apigame.TeamMember
}

// NewPlayerLeaveSensor creates a new PlayerLeaveSensor
func NewPlayerLeaveSensor() *PlayerLeaveSensor {
	return &PlayerLeaveSensor{
		previousPlayers: make(map[int32]*apigame.TeamMember),
	}
}

// AddFrame processes a frame and returns a PlayerLeft event if detected
func (s *PlayerLeaveSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	currentPlayers := extractPlayersMap(frame.GetSession())

	// Find missing players (in previous but not in current)
	for slot, player := range s.previousPlayers {
		if _, exists := currentPlayers[slot]; !exists {
			// Update state before returning
			s.previousPlayers = currentPlayers
			return &telemetry.LobbySessionEvent{
				Event: &telemetry.LobbySessionEvent_PlayerLeft{
					PlayerLeft: &telemetry.PlayerLeft{
						PlayerSlot:  slot,
						DisplayName: player.GetDisplayName(),
					},
				},
			}
		}
	}

	s.previousPlayers = currentPlayers
	return nil
}

// PlayerTeamSwitchSensor detects when players switch teams
type PlayerTeamSwitchSensor struct {
	previousPlayers map[int32]*apigame.TeamMember
}

// NewPlayerTeamSwitchSensor creates a new PlayerTeamSwitchSensor
func NewPlayerTeamSwitchSensor() *PlayerTeamSwitchSensor {
	return &PlayerTeamSwitchSensor{
		previousPlayers: make(map[int32]*apigame.TeamMember),
	}
}

// AddFrame processes a frame and returns a PlayerSwitchedTeam event if detected
func (s *PlayerTeamSwitchSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	currentPlayers := extractPlayersMap(frame.GetSession())

	// Check for team switches (same slot, different team)
	for slot, currentPlayer := range currentPlayers {
		if prevPlayer, existed := s.previousPlayers[slot]; existed {
			prevRole := determinePlayerRole(prevPlayer)
			currRole := determinePlayerRole(currentPlayer)
			if prevRole != currRole {
				s.previousPlayers = currentPlayers
				return &telemetry.LobbySessionEvent{
					Event: &telemetry.LobbySessionEvent_PlayerSwitchedTeam{
						PlayerSwitchedTeam: &telemetry.PlayerSwitchedTeam{
							PlayerSlot: slot,
							NewRole:    currRole,
							PrevRole:   prevRole,
						},
					},
				}
			}
		}
	}

	s.previousPlayers = currentPlayers
	return nil
}

// EmoteSensor detects when players play emotes
type EmoteSensor struct {
	previousEmoteStates map[int32]bool // keyed by slot number
}

// NewEmoteSensor creates a new EmoteSensor
func NewEmoteSensor() *EmoteSensor {
	return &EmoteSensor{
		previousEmoteStates: make(map[int32]bool),
	}
}

// AddFrame processes a frame and returns an EmotePlayed event if detected
func (s *EmoteSensor) AddFrame(frame *telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent {
	if frame == nil || frame.GetSession() == nil {
		return nil
	}

	for _, team := range frame.GetSession().GetTeams() {
		for _, player := range team.GetPlayers() {
			slot := player.GetSlotNumber()
			isPlaying := player.GetIsEmotePlaying()
			wasPlaying := s.previousEmoteStates[slot]

			// Detect transition from not playing to playing
			if isPlaying && !wasPlaying {
				s.previousEmoteStates[slot] = isPlaying
				return &telemetry.LobbySessionEvent{
					Event: &telemetry.LobbySessionEvent_EmotePlayed{
						EmotePlayed: &telemetry.EmotePlayed{
							PlayerSlot: slot,
							Emote:      telemetry.EmotePlayed_EMOTE_TYPE_PRIMARY,
						},
					},
				}
			}
			s.previousEmoteStates[slot] = isPlaying
		}
	}

	return nil
}

// extractPlayersMap extracts all players from a session into a map keyed by slot
func extractPlayersMap(session *apigame.SessionResponse) map[int32]*apigame.TeamMember {
	players := make(map[int32]*apigame.TeamMember)
	for _, team := range session.GetTeams() {
		for _, player := range team.GetPlayers() {
			players[player.GetSlotNumber()] = player
		}
	}
	return players
}

// determinePlayerRole determines a player's role based on their jersey number and slot
func determinePlayerRole(player *apigame.TeamMember) telemetry.Role {
	if player == nil {
		return telemetry.Role_ROLE_UNSPECIFIED
	}

	jerseyNumber := player.GetJerseyNumber()

	// Spectators have jersey number -1
	if jerseyNumber == -1 {
		return telemetry.Role_ROLE_SPECTATOR
	}

	// Blue team: jersey 0-4, Orange team: jersey 5-9 (or similar pattern)
	// In Echo Arena, blue team players have even slot offsets, orange team have odd
	// Actually, teams are determined by which team array they're in, but we use jersey as heuristic
	slot := player.GetSlotNumber()

	// Players 0-3 are typically blue, 4-7 are typically orange
	if slot < 4 {
		return telemetry.Role_ROLE_BLUE_TEAM
	}
	return telemetry.Role_ROLE_ORANGE_TEAM
}
