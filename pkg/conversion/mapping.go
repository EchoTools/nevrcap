package conversion

import (
	"encoding/binary"
	"math"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	spatialv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/spatial/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"

	"github.com/echotools/tape/pkg/events"
)

// gameStatusMap maps v1 string game_status to v2 GameStatus enum.
var gameStatusMap = map[string]capturepb.GameStatus{
	"pre_match":         capturepb.GameStatus_GAME_STATUS_PRE_MATCH,
	"round_start":       capturepb.GameStatus_GAME_STATUS_ROUND_START,
	"playing":           capturepb.GameStatus_GAME_STATUS_PLAYING,
	"score":             capturepb.GameStatus_GAME_STATUS_SCORE,
	"round_over":        capturepb.GameStatus_GAME_STATUS_ROUND_OVER,
	"post_match":        capturepb.GameStatus_GAME_STATUS_POST_MATCH,
	"pre_sudden_death":  capturepb.GameStatus_GAME_STATUS_PRE_SUDDEN_DEATH,
	"sudden_death":      capturepb.GameStatus_GAME_STATUS_SUDDEN_DEATH,
	"post_sudden_death": capturepb.GameStatus_GAME_STATUS_POST_SUDDEN_DEATH,
}

// matchTypeMap maps v1 string match_type to v2 MatchType enum.
var matchTypeMap = map[string]capturepb.MatchType{
	"Echo_Arena":            capturepb.MatchType_MATCH_TYPE_ARENA,
	"Echo_Arena_Private":    capturepb.MatchType_MATCH_TYPE_PRIVATE,
	"Echo_Arena_Tournament": capturepb.MatchType_MATCH_TYPE_TOURNAMENT,
	"Echo_Combat":           capturepb.MatchType_MATCH_TYPE_COMBAT,
	"Social_2.0":            capturepb.MatchType_MATCH_TYPE_SOCIAL_PUBLIC,
	"Social_2.0_Private":    capturepb.MatchType_MATCH_TYPE_SOCIAL_PRIVATE,
	"Echo_Arena_FFA":        capturepb.MatchType_MATCH_TYPE_FFA,
}

// pauseStateMap maps v1 PauseState.paused_state strings to v2 PauseState enum.
var pauseStateMap = map[string]capturepb.PauseState{
	"":         capturepb.PauseState_PAUSE_STATE_NOT_PAUSED,
	"unpaused": capturepb.PauseState_PAUSE_STATE_NOT_PAUSED,
	"paused":   capturepb.PauseState_PAUSE_STATE_PAUSED,
	// Intentional data narrowing: v2 proto has no PAUSE_STATE_REQUESTED;
	// "paused_requested" is treated as PAUSED.
	"paused_requested": capturepb.PauseState_PAUSE_STATE_PAUSED,
	"unpausing":        capturepb.PauseState_PAUSE_STATE_UNPAUSING,
	"autopause_replay": capturepb.PauseState_PAUSE_STATE_AUTOPAUSE_REPLAY,
}

// teamStringToRole maps v1 team name strings to v2 Role enum.
var teamStringToRoleMap = map[string]capturepb.Role{
	"blue":   capturepb.Role_ROLE_BLUE_TEAM,
	"orange": capturepb.Role_ROLE_ORANGE_TEAM,
}

func teamStringToRole(team string) capturepb.Role {
	if r, ok := teamStringToRoleMap[team]; ok {
		return r
	}
	return capturepb.Role_ROLE_UNSPECIFIED
}

// goalTypeStringToEnum maps v1 goal_type strings to v2 GoalType enum.
var goalTypeStringMap = map[string]capturepb.GoalType{
	"INSIDE SHOT":      capturepb.GoalType_GOAL_TYPE_INSIDE_SHOT,
	"LONG SHOT":        capturepb.GoalType_GOAL_TYPE_LONG_SHOT,
	"BOUNCE SHOT":      capturepb.GoalType_GOAL_TYPE_BOUNCE_SHOT,
	"LONG BOUNCE SHOT": capturepb.GoalType_GOAL_TYPE_LONG_BOUNCE_SHOT,
	"SELF GOAL":        capturepb.GoalType_GOAL_TYPE_SELF_GOAL,
}

func goalTypeStringToEnum(goalType string) capturepb.GoalType {
	if gt, ok := goalTypeStringMap[goalType]; ok {
		return gt
	}
	return capturepb.GoalType_GOAL_TYPE_UNSPECIFIED
}

// MapHeader converts a v1 TelemetryHeader to a v2 CaptureHeader.
func MapHeader(v1 *telemetryv1.TelemetryHeader) *capturepb.CaptureHeader {
	if v1 == nil {
		return nil
	}

	header := &capturepb.CaptureHeader{
		CaptureId:     v1.GetCaptureId(),
		CreatedAt:     v1.GetCreatedAt(),
		FormatVersion: 2,
		Metadata:      v1.GetMetadata(),
	}

	// If metadata contains session_id, populate EchoArenaHeader.
	if meta := v1.GetMetadata(); meta != nil {
		if sessionID, ok := meta["session_id"]; ok {
			eaHeader := &capturepb.EchoArenaHeader{
				SessionId: sessionID,
			}
			if mapName, ok := meta["map_name"]; ok {
				eaHeader.MapName = mapName
			}
			if matchType, ok := meta["match_type"]; ok {
				eaHeader.MatchType = matchTypeMap[matchType]
			}
			if clientName, ok := meta["client_name"]; ok {
				eaHeader.ClientName = clientName
			}
			header.GameHeader = &capturepb.CaptureHeader_EchoArena{
				EchoArena: eaHeader,
			}
		}
	}

	return header
}

// MapHeaderFromSession creates a CaptureHeader from a v1 TelemetryHeader
// enriched with data from the first SessionResponse frame.
func MapHeaderFromSession(v1hdr *telemetryv1.TelemetryHeader, session *enginev1.SessionResponse) *capturepb.CaptureHeader {
	header := MapHeader(v1hdr)
	if header == nil {
		return nil
	}

	if session == nil {
		return header
	}

	// Build or enrich EchoArenaHeader from session data.
	eaHeader := header.GetEchoArena()
	if eaHeader == nil {
		eaHeader = &capturepb.EchoArenaHeader{}
		header.GameHeader = &capturepb.CaptureHeader_EchoArena{
			EchoArena: eaHeader,
		}
	}

	if eaHeader.SessionId == "" {
		eaHeader.SessionId = session.GetSessionId()
	}
	if eaHeader.MapName == "" {
		eaHeader.MapName = session.GetMapName()
	}
	if eaHeader.MatchType == capturepb.MatchType_MATCH_TYPE_UNSPECIFIED {
		eaHeader.MatchType = matchTypeMap[session.GetMatchType()]
	}
	if eaHeader.ClientName == "" {
		eaHeader.ClientName = session.GetClientName()
	}
	eaHeader.PrivateMatch = session.GetPrivateMatch()
	eaHeader.TournamentMatch = session.GetTournamentMatch()
	eaHeader.TotalRoundCount = session.GetTotalRoundCount()

	if roster := buildInitialRoster(session.GetTeams()); len(roster) > 0 {
		eaHeader.InitialRoster = roster
	}

	return header
}

// buildInitialRoster builds the v2 initial roster from the session's teams.
// Team order is preserved (team 0 = blue, team 1 = orange); any other team
// index, or a jersey number of -1, marks a spectator.
func buildInitialRoster(teams []*enginev1.Team) []*capturepb.PlayerInfo {
	var roster []*capturepb.PlayerInfo
	for teamIdx, team := range teams {
		for _, m := range team.GetPlayers() {
			roster = append(roster, &capturepb.PlayerInfo{
				Slot:          m.GetSlotNumber(),
				AccountNumber: m.GetAccountNumber(),
				DisplayName:   m.GetDisplayName(),
				Role:          rosterRole(teamIdx, m.GetJerseyNumber()),
				JerseyNumber:  m.GetJerseyNumber(),
				Level:         m.GetLevel(),
			})
		}
	}
	return roster
}

// rosterRole maps a team index and jersey number to a v2 Role using the shared
// events.ClassifyRole source of truth, so the InitialRoster role can never
// diverge from the PlayerJoined role resolved in pkg/events. A jersey number of
// -1, or any team index other than blue (0) or orange (1), marks a spectator.
func rosterRole(teamIdx int, jersey int32) capturepb.Role {
	switch events.ClassifyRole(teamIdx, jersey) {
	case events.RoleClassBlue:
		return capturepb.Role_ROLE_BLUE_TEAM
	case events.RoleClassOrange:
		return capturepb.Role_ROLE_ORANGE_TEAM
	default:
		return capturepb.Role_ROLE_SPECTATOR
	}
}

// FrameMapper holds state that persists across frames during conversion.
type FrameMapper struct {
	BaseTime    time.Time
	RoundNumber int32
}

// Reset clears the mapper's accumulated state for a new conversion session.
func (m *FrameMapper) Reset() {
	m.RoundNumber = 0
}

// MapFrame converts a v1 LobbySessionStateFrame to a v2 Frame using the
// mapper's accumulated state. It updates RoundNumber when a RoundStarted
// event is present on the frame.
func (m *FrameMapper) MapFrame(v1f *telemetryv1.LobbySessionStateFrame) *capturepb.Frame {
	// Scan events for RoundStarted to update round number.
	for _, evt := range v1f.GetEvents() {
		if rs, ok := evt.GetEvent().(*telemetryv1.LobbySessionEvent_RoundStarted); ok {
			m.RoundNumber = rs.RoundStarted.GetRoundNumber()
		}
	}

	return mapFrame(v1f, m.BaseTime, m.RoundNumber)
}

// MapFrame converts a v1 LobbySessionStateFrame to a v2 Frame.
// baseTime is the capture creation time, used to compute timestamp_offset_ms.
func MapFrame(v1f *telemetryv1.LobbySessionStateFrame, baseTime time.Time) *capturepb.Frame {
	return mapFrame(v1f, baseTime, 0)
}

func mapFrame(v1f *telemetryv1.LobbySessionStateFrame, baseTime time.Time, roundNumber int32) *capturepb.Frame {
	if v1f == nil {
		return nil
	}

	frameTime := v1f.GetTimestamp().AsTime()
	// Clamp to zero if frame predates baseTime — prevents uint32 wrap from
	// a negative Sub() result.
	diffMs := max(frameTime.Sub(baseTime).Milliseconds(), 0)
	offsetMs := uint32(diffMs) //nolint:gosec // duration fits uint32 for game sessions

	frame := &capturepb.Frame{
		FrameIndex:        v1f.GetFrameIndex(),
		TimestampOffsetMs: offsetMs,
	}

	session := v1f.GetSession()
	if session == nil {
		return frame
	}

	ea := &capturepb.EchoArenaFrame{
		GameStatus:   gameStatusMap[session.GetGameStatus()],
		GameClock:    float32(session.GetGameClock()),
		BluePoints:   session.GetBluePoints(),
		OrangePoints: session.GetOrangePoints(),
	}

	// Map pause state.
	if pause := session.GetPause(); pause != nil {
		if ps, ok := pauseStateMap[pause.GetPausedState()]; ok {
			ea.PauseState = ps
		}
	}

	// Map disc.
	if disc := session.GetDisc(); disc != nil {
		ea.Disc = mapDisc(disc)
	}

	// Map players from teams.
	ea.Players = mapPlayers(session.GetTeams())

	// Find disc holder.
	for _, team := range session.GetTeams() {
		for _, p := range team.GetPlayers() {
			if p.GetHasPossession() {
				slot := p.GetSlotNumber()
				ea.DiscHolderSlot = &slot
			}
		}
	}

	// Map VR root.
	if playerRoot := session.GetPlayer(); playerRoot != nil {
		ea.VrRoot = mapPlayerRoot(playerRoot)
	}

	// Map player bones.
	if bones := v1f.GetPlayerBones(); bones != nil {
		ea.PlayerBones = mapPlayerBones(bones)
	}

	// Map events from v1.
	ea.Events = mapEvents(v1f.GetEvents())

	// Set round number from conversion state.
	ea.RoundNumber = roundNumber

	frame.Payload = &capturepb.Frame_EchoArena{EchoArena: ea}
	return frame
}

func mapDisc(disc *enginev1.Disc) *capturepb.DiscState {
	ds := &capturepb.DiscState{
		BounceCount: uint32(disc.GetBounceCount()), //nolint:gosec // bounce count is non-negative
	}

	pos := disc.GetPosition()
	fwd := disc.GetForward()
	left := disc.GetLeft()
	up := disc.GetUp()
	if len(pos) >= 3 {
		ds.Pose = poseFromVectors(pos, fwd, left, up)
	}

	vel := disc.GetVelocity()
	if len(vel) >= 3 {
		ds.Velocity = &spatialv1.Vec3{
			X: float32(vel[0]),
			Y: float32(vel[1]),
			Z: float32(vel[2]),
		}
	}

	return ds
}

// poseFromVectors builds a Pose from position and orientation vectors.
// Orientation is converted from forward/left/up to quaternion.
func poseFromVectors(pos, fwd, left, up []float64) *spatialv1.Pose {
	pose := &spatialv1.Pose{
		Position: &spatialv1.Vec3{
			X: float32(pos[0]),
			Y: float32(pos[1]),
			Z: float32(pos[2]),
		},
	}

	if len(fwd) >= 3 && len(left) >= 3 && len(up) >= 3 {
		pose.Orientation = quaternionFromAxes(fwd, left, up)
	}

	return pose
}

// quaternionFromAxes converts forward/left/up basis vectors to a quaternion.
// Uses the standard rotation matrix to quaternion conversion.
func quaternionFromAxes(fwd, left, up []float64) *spatialv1.Quat {
	// The rotation matrix columns are: left, up, forward (right-handed).
	// m00=left[0]  m01=up[0]  m02=fwd[0]
	// m10=left[1]  m11=up[1]  m12=fwd[1]
	// m20=left[2]  m21=up[2]  m22=fwd[2]
	m00, m01, m02 := left[0], up[0], fwd[0]
	m10, m11, m12 := left[1], up[1], fwd[1]
	m20, m21, m22 := left[2], up[2], fwd[2]

	trace := m00 + m11 + m22
	var qw, qx, qy, qz float64

	if trace > 0 {
		s := math.Sqrt(trace+1.0) * 2 // s=4*qw
		qw = 0.25 * s
		qx = (m21 - m12) / s
		qy = (m02 - m20) / s
		qz = (m10 - m01) / s
	} else if m00 > m11 && m00 > m22 {
		s := math.Sqrt(1.0+m00-m11-m22) * 2 // s=4*qx
		qw = (m21 - m12) / s
		qx = 0.25 * s
		qy = (m01 + m10) / s
		qz = (m02 + m20) / s
	} else if m11 > m22 {
		s := math.Sqrt(1.0+m11-m00-m22) * 2 // s=4*qy
		qw = (m02 - m20) / s
		qx = (m01 + m10) / s
		qy = 0.25 * s
		qz = (m12 + m21) / s
	} else {
		s := math.Sqrt(1.0+m22-m00-m11) * 2 // s=4*qz
		qw = (m10 - m01) / s
		qx = (m02 + m20) / s
		qy = (m12 + m21) / s
		qz = 0.25 * s
	}

	return &spatialv1.Quat{
		X: float32(qx),
		Y: float32(qy),
		Z: float32(qz),
		W: float32(qw),
	}
}

func mapPlayers(teams []*enginev1.Team) []*capturepb.PlayerState {
	var players []*capturepb.PlayerState
	for teamIdx, team := range teams {
		for _, member := range team.GetPlayers() {
			ps := &capturepb.PlayerState{
				Slot: member.GetSlotNumber(),
				Ping: uint32(member.GetPing()), //nolint:gosec // ping is non-negative
			}

			// Map head pose.
			if head := member.GetHead(); head != nil {
				ps.Head = bodyPartToPose(head)
			}

			// Map body pose.
			if body := member.GetBody(); body != nil {
				ps.Body = bodyPartToPose(body)
			}

			// Map hand poses.
			if lh := member.GetLeftHand(); lh != nil {
				ps.LeftHand = handPartToPose(lh)
			}
			if rh := member.GetRightHand(); rh != nil {
				ps.RightHand = handPartToPose(rh)
			}

			// Map velocity.
			vel := member.GetVelocity()
			if len(vel) >= 3 {
				ps.Velocity = &spatialv1.Vec3{
					X: float32(vel[0]),
					Y: float32(vel[1]),
					Z: float32(vel[2]),
				}
			}

			// Pack boolean flags.
			var flags uint32
			if member.GetIsStunned() {
				flags |= 1 << 0
			}
			if member.GetIsInvulnerable() {
				flags |= 1 << 1
			}
			if member.GetIsBlocking() {
				flags |= 1 << 2
			}
			if member.GetHasPossession() {
				flags |= 1 << 3
			}
			if member.GetIsEmotePlaying() {
				flags |= 1 << 4
			}
			// Encode team index in bits 5-6 (0=blue, 1=orange, 2=spectator).
			flags |= uint32(teamIdx&0x3) << 5
			ps.Flags = flags

			players = append(players, ps)
		}
	}
	return players
}

func bodyPartToPose(bp *enginev1.BodyPart) *spatialv1.Pose {
	pos := bp.GetPosition()
	fwd := bp.GetForward()
	left := bp.GetLeft()
	up := bp.GetUp()

	if len(pos) < 3 {
		return nil
	}

	return poseFromVectors(pos, fwd, left, up)
}

func handPartToPose(hp *enginev1.HandPart) *spatialv1.Pose {
	pos := hp.GetPos()
	fwd := hp.GetForward()
	left := hp.GetLeft()
	up := hp.GetUp()

	if len(pos) < 3 {
		return nil
	}

	return poseFromVectors(pos, fwd, left, up)
}

func mapPlayerRoot(pr *enginev1.PlayerRoot) *spatialv1.Pose {
	pos := pr.GetVrPosition()
	fwd := pr.GetVrForward()
	left := pr.GetVrLeft()
	up := pr.GetVrUp()

	if len(pos) < 3 {
		return nil
	}

	return poseFromVectors(pos, fwd, left, up)
}

func mapPlayerBones(bones *enginev1.PlayerBonesResponse) []*capturepb.PlayerBones {
	userBones := bones.GetUserBones()
	if len(userBones) == 0 {
		return nil
	}

	result := make([]*capturepb.PlayerBones, 0, len(userBones))
	for _, ub := range userBones {
		pb := &capturepb.PlayerBones{
			Slot: ub.GetPlayerIndex(),
		}

		// Convert float32 slices to packed little-endian bytes.
		if transforms := ub.GetBoneT(); len(transforms) > 0 {
			pb.Transforms = float32SliceToBytes(transforms)
		}
		if orientations := ub.GetBoneO(); len(orientations) > 0 {
			pb.Orientations = float32SliceToBytes(orientations)
		}

		result = append(result, pb)
	}
	return result
}

// float32SliceToBytes converts a []float32 to packed little-endian bytes.
func float32SliceToBytes(fs []float32) []byte {
	buf := make([]byte, len(fs)*4)
	for i, f := range fs {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// mapEvents converts v1 LobbySessionEvent list to v2 EchoEvent list.
func mapEvents(v1Events []*telemetryv1.LobbySessionEvent) []*capturepb.EchoEvent {
	if len(v1Events) == 0 {
		return nil
	}

	result := make([]*capturepb.EchoEvent, 0, len(v1Events))
	for _, v1e := range v1Events {
		if v2e := mapEvent(v1e); v2e != nil {
			result = append(result, v2e)
		}
	}
	return result
}

// mapEvent converts a single v1 LobbySessionEvent to a v2 EchoEvent.
// Both packages define the same event sub-message types with the same fields,
// so we can copy field-by-field.
func mapEvent(v1e *telemetryv1.LobbySessionEvent) *capturepb.EchoEvent {
	if v1e == nil {
		return nil
	}

	evt := &capturepb.EchoEvent{}

	switch e := v1e.GetEvent().(type) {
	case *telemetryv1.LobbySessionEvent_RoundStarted:
		evt.Event = &capturepb.EchoEvent_RoundStarted{
			RoundStarted: &capturepb.RoundStarted{
				RoundNumber: e.RoundStarted.GetRoundNumber(),
			},
		}
	case *telemetryv1.LobbySessionEvent_RoundPaused:
		evt.Event = &capturepb.EchoEvent_RoundPaused{
			RoundPaused: &capturepb.RoundPaused{},
		}
	case *telemetryv1.LobbySessionEvent_RoundUnpaused:
		evt.Event = &capturepb.EchoEvent_RoundUnpaused{
			RoundUnpaused: &capturepb.RoundUnpaused{},
		}
	case *telemetryv1.LobbySessionEvent_RoundEnded:
		evt.Event = &capturepb.EchoEvent_RoundEnded{
			RoundEnded: &capturepb.RoundEnded{
				RoundNumber: e.RoundEnded.GetRoundNumber(),
				WinningTeam: capturepb.Role(e.RoundEnded.GetWinningTeam()),
			},
		}
	case *telemetryv1.LobbySessionEvent_MatchEnded:
		evt.Event = &capturepb.EchoEvent_MatchEnded{
			MatchEnded: &capturepb.MatchEnded{
				WinningTeam: capturepb.Role(e.MatchEnded.GetWinningTeam()),
			},
		}
	case *telemetryv1.LobbySessionEvent_ScoreboardUpdated:
		evt.Event = &capturepb.EchoEvent_ScoreboardUpdated{
			ScoreboardUpdated: &capturepb.ScoreboardUpdated{
				BluePoints:       e.ScoreboardUpdated.GetBluePoints(),
				OrangePoints:     e.ScoreboardUpdated.GetOrangePoints(),
				BlueRoundScore:   e.ScoreboardUpdated.GetBlueRoundScore(),
				OrangeRoundScore: e.ScoreboardUpdated.GetOrangeRoundScore(),
				GameClockDisplay: e.ScoreboardUpdated.GetGameClockDisplay(),
			},
		}
	case *telemetryv1.LobbySessionEvent_PlayerJoined:
		pj := &capturepb.PlayerJoined{
			Role: capturepb.Role(e.PlayerJoined.GetRole()),
		}
		if tm := e.PlayerJoined.GetPlayer(); tm != nil {
			pj.Slot = tm.GetSlotNumber()
			pj.AccountNumber = uint64(tm.GetAccountNumber())
			pj.DisplayName = tm.GetDisplayName()
			pj.JerseyNumber = tm.GetJerseyNumber()
			pj.Level = tm.GetLevel()
		}
		evt.Event = &capturepb.EchoEvent_PlayerJoined{
			PlayerJoined: pj,
		}
	case *telemetryv1.LobbySessionEvent_PlayerLeft:
		evt.Event = &capturepb.EchoEvent_PlayerLeft{
			PlayerLeft: &capturepb.PlayerLeft{
				PlayerSlot:  e.PlayerLeft.GetPlayerSlot(),
				DisplayName: e.PlayerLeft.GetDisplayName(),
			},
		}
	case *telemetryv1.LobbySessionEvent_PlayerSwitchedTeam:
		evt.Event = &capturepb.EchoEvent_PlayerSwitchedTeam{
			PlayerSwitchedTeam: &capturepb.PlayerSwitchedTeam{
				PlayerSlot: e.PlayerSwitchedTeam.GetPlayerSlot(),
				NewRole:    capturepb.Role(e.PlayerSwitchedTeam.GetNewRole()),
				PrevRole:   capturepb.Role(e.PlayerSwitchedTeam.GetPrevRole()),
			},
		}
	case *telemetryv1.LobbySessionEvent_EmotePlayed:
		evt.Event = &capturepb.EchoEvent_EmotePlayed{
			EmotePlayed: &capturepb.EmotePlayed{
				PlayerSlot: e.EmotePlayed.GetPlayerSlot(),
				Emote:      capturepb.EmotePlayed_EmoteType(e.EmotePlayed.GetEmote()),
			},
		}
	case *telemetryv1.LobbySessionEvent_DiscPossessionChanged:
		playerSlot := e.DiscPossessionChanged.GetPlayerSlot()
		prevSlot := e.DiscPossessionChanged.GetPreviousPlayerSlot()
		evt.Event = &capturepb.EchoEvent_DiscPossessionChanged{
			DiscPossessionChanged: &capturepb.DiscPossessionChanged{
				PlayerSlot:         &playerSlot,
				PreviousPlayerSlot: &prevSlot,
			},
		}
	case *telemetryv1.LobbySessionEvent_DiscThrown:
		dt := &capturepb.DiscThrown{
			PlayerSlot: e.DiscThrown.GetPlayerSlot(),
		}
		if td := e.DiscThrown.GetThrowDetails(); td != nil {
			dt.ThrowDetails = &capturepb.ThrowDetails{
				ArmSpeed:                float32(td.GetArmSpeed()),
				TotalSpeed:              float32(td.GetTotalSpeed()),
				OffAxisSpinDeg:          float32(td.GetOffAxisSpinDeg()),
				WristThrowPenalty:       float32(td.GetWristThrowPenalty()),
				RotPerSec:               float32(td.GetRotPerSec()),
				PotSpeedFromRot:         float32(td.GetPotSpeedFromRot()),
				SpeedFromArm:            float32(td.GetSpeedFromArm()),
				SpeedFromMovement:       float32(td.GetSpeedFromMovement()),
				SpeedFromWrist:          float32(td.GetSpeedFromWrist()),
				WristAlignToThrowDeg:    float32(td.GetWristAlignToThrowDeg()),
				ThrowAlignToMovementDeg: float32(td.GetThrowAlignToMovementDeg()),
				OffAxisPenalty:          float32(td.GetOffAxisPenalty()),
				ThrowMovePenalty:        float32(td.GetThrowMovePenalty()),
			}
		}
		evt.Event = &capturepb.EchoEvent_DiscThrown{
			DiscThrown: dt,
		}
	case *telemetryv1.LobbySessionEvent_DiscCaught:
		evt.Event = &capturepb.EchoEvent_DiscCaught{
			DiscCaught: &capturepb.DiscCaught{
				PlayerSlot: e.DiscCaught.GetPlayerSlot(),
			},
		}
	case *telemetryv1.LobbySessionEvent_GoalScored:
		gs := &capturepb.GoalScored{}
		if sd := e.GoalScored.GetScoreDetails(); sd != nil {
			gs.DiscSpeed = float32(sd.GetDiscSpeed())
			gs.Team = teamStringToRole(sd.GetTeam())
			gs.GoalType = goalTypeStringToEnum(sd.GetGoalType())
			gs.PointAmount = sd.GetPointAmount()
			gs.DistanceThrown = float32(sd.GetDistanceThrown())
			// PersonScored and AssistScored are display names in v1;
			// ScorerSlot and AssistSlot are slot indices in v2 and cannot
			// be resolved from names alone, so they remain at zero values.
		}
		evt.Event = &capturepb.EchoEvent_GoalScored{
			GoalScored: gs,
		}
	case *telemetryv1.LobbySessionEvent_PlayerGoal:
		evt.Event = &capturepb.EchoEvent_PlayerGoal{
			PlayerGoal: &capturepb.PlayerGoal{
				PlayerSlot: e.PlayerGoal.GetPlayerSlot(),
				TotalGoals: e.PlayerGoal.GetTotalGoals(),
				Points:     e.PlayerGoal.GetPoints(),
			},
		}
	case *telemetryv1.LobbySessionEvent_PlayerSave:
		evt.Event = &capturepb.EchoEvent_PlayerSave{
			PlayerSave: &capturepb.PlayerSave{
				PlayerSlot: e.PlayerSave.GetPlayerSlot(),
				TotalSaves: e.PlayerSave.GetTotalSaves(),
			},
		}
	case *telemetryv1.LobbySessionEvent_PlayerStun:
		evt.Event = &capturepb.EchoEvent_PlayerStun{
			PlayerStun: &capturepb.PlayerStun{
				PlayerSlot: e.PlayerStun.GetPlayerSlot(),
				TotalStuns: e.PlayerStun.GetTotalStuns(),
			},
		}
	case *telemetryv1.LobbySessionEvent_PlayerPass:
		evt.Event = &capturepb.EchoEvent_PlayerPass{
			PlayerPass: &capturepb.PlayerPass{
				PlayerSlot:  e.PlayerPass.GetPlayerSlot(),
				TotalPasses: e.PlayerPass.GetTotalPasses(),
			},
		}
	case *telemetryv1.LobbySessionEvent_PlayerSteal:
		victimSlot := e.PlayerSteal.GetVictimPlayerSlot()
		evt.Event = &capturepb.EchoEvent_PlayerSteal{
			PlayerSteal: &capturepb.PlayerSteal{
				PlayerSlot:       e.PlayerSteal.GetPlayerSlot(),
				TotalSteals:      e.PlayerSteal.GetTotalSteals(),
				VictimPlayerSlot: &victimSlot,
			},
		}
	case *telemetryv1.LobbySessionEvent_PlayerBlock:
		evt.Event = &capturepb.EchoEvent_PlayerBlock{
			PlayerBlock: &capturepb.PlayerBlock{
				PlayerSlot:  e.PlayerBlock.GetPlayerSlot(),
				TotalBlocks: e.PlayerBlock.GetTotalBlocks(),
			},
		}
	case *telemetryv1.LobbySessionEvent_PlayerInterception:
		evt.Event = &capturepb.EchoEvent_PlayerInterception{
			PlayerInterception: &capturepb.PlayerInterception{
				PlayerSlot:         e.PlayerInterception.GetPlayerSlot(),
				TotalInterceptions: e.PlayerInterception.GetTotalInterceptions(),
			},
		}
	case *telemetryv1.LobbySessionEvent_PlayerAssist:
		evt.Event = &capturepb.EchoEvent_PlayerAssist{
			PlayerAssist: &capturepb.PlayerAssist{
				PlayerSlot:   e.PlayerAssist.GetPlayerSlot(),
				TotalAssists: e.PlayerAssist.GetTotalAssists(),
			},
		}
	case *telemetryv1.LobbySessionEvent_PlayerShotTaken:
		evt.Event = &capturepb.EchoEvent_PlayerShotTaken{
			PlayerShotTaken: &capturepb.PlayerShotTaken{
				PlayerSlot: e.PlayerShotTaken.GetPlayerSlot(),
				TotalShots: e.PlayerShotTaken.GetTotalShots(),
			},
		}
	case *telemetryv1.LobbySessionEvent_GenericEvent:
		evt.Event = &capturepb.EchoEvent_GenericEvent{
			GenericEvent: &capturepb.GenericEvent{
				EventType: e.GenericEvent.GetEventType(),
				Data:      e.GenericEvent.GetData(),
				Payload:   e.GenericEvent.GetPayload(),
			},
		}
	default:
		return nil
	}

	return evt
}
