package conversion

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	spatialv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/spatial/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/echotools/tape/pkg/codec"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Team-index encoding in PlayerState.flags (see mapping.go mapPlayers): bits 5-6
// hold the team index (0=blue, 1=orange, 2=spectator). Bits 0-4 are boolean
// player flags.
const (
	flagStunned       = 1 << 0
	flagInvulnerable  = 1 << 1
	flagBlocking      = 1 << 2
	flagPossession    = 1 << 3
	flagEmotePlaying  = 1 << 4
	teamIndexShift    = 5
	teamIndexMask     = 0x3
	spectatorTeamIdx  = 2
	possessionNoneVal = -1
)

// gameStatusReverse maps a v2 GameStatus enum back to its v1 string. It is the
// inverse of gameStatusMap; GAME_STATUS_UNSPECIFIED maps to the empty string
// (the value forward-mapped from "" or any unrecognized status).
var gameStatusReverse = buildReverse(gameStatusMap, capturepb.GameStatus_GAME_STATUS_UNSPECIFIED, "")

// matchTypeReverse maps a v2 MatchType enum back to its v1 string. Inverse of
// matchTypeMap; MATCH_TYPE_UNSPECIFIED maps to the empty string.
var matchTypeReverse = buildReverse(matchTypeMap, capturepb.MatchType_MATCH_TYPE_UNSPECIFIED, "")

// pauseStateReverse maps a v2 PauseState enum back to a canonical v1
// paused_state string. The forward pauseStateMap is many-to-one
// ("" and "unpaused" both map to NOT_PAUSED; "paused" and "paused_requested"
// both map to PAUSED), so the reverse picks the canonical non-empty spelling.
// The "paused_requested" narrowing is not recoverable (see SCHEMA-GAPS BUG-5).
var pauseStateReverse = map[capturepb.PauseState]string{
	capturepb.PauseState_PAUSE_STATE_NOT_PAUSED:       "unpaused",
	capturepb.PauseState_PAUSE_STATE_PAUSED:           "paused",
	capturepb.PauseState_PAUSE_STATE_UNPAUSING:        "unpausing",
	capturepb.PauseState_PAUSE_STATE_AUTOPAUSE_REPLAY: "autopause_replay",
}

// buildReverse inverts a string->enum map into enum->string, then forces the
// zero enum to map to the supplied zero string (the forward map's default).
func buildReverse[E comparable](forward map[string]E, zeroEnum E, zeroStr string) map[E]string {
	rev := make(map[E]string, len(forward))
	for str, enum := range forward {
		// Prefer a non-empty spelling if multiple strings share an enum.
		if existing, ok := rev[enum]; ok && existing != "" && str == "" {
			continue
		}
		rev[enum] = str
	}
	rev[zeroEnum] = zeroStr
	return rev
}

// quaternionToAxes converts a quaternion back to forward/left/up basis vectors.
// It is the inverse of quaternionFromAxes: that function builds the rotation
// matrix whose columns are [left, up, forward] and converts it to a quaternion,
// so here we expand the quaternion to that same rotation matrix and read the
// columns back out. Because the matrix is quadratic in the quaternion, the sign
// ambiguity (q and -q) does not affect the recovered basis.
func quaternionToAxes(q *spatialv1.Quat) (fwd, left, up []float64) {
	x := float64(q.GetX())
	y := float64(q.GetY())
	z := float64(q.GetZ())
	w := float64(q.GetW())

	// Rotation matrix from a (possibly unnormalized) quaternion. Columns:
	// col0=left, col1=up, col2=forward.
	m00 := 1 - 2*(y*y+z*z)
	m01 := 2 * (x*y - w*z)
	m02 := 2 * (x*z + w*y)
	m10 := 2 * (x*y + w*z)
	m11 := 1 - 2*(x*x+z*z)
	m12 := 2 * (y*z - w*x)
	m20 := 2 * (x*z - w*y)
	m21 := 2 * (y*z + w*x)
	m22 := 1 - 2*(x*x+y*y)

	left = []float64{m00, m10, m20}
	up = []float64{m01, m11, m21}
	fwd = []float64{m02, m12, m22}
	return fwd, left, up
}

// poseToVectors reverses poseFromVectors: it returns the position and the
// forward/left/up basis. fwd/left/up are nil when the pose carries no
// orientation (matching poseFromVectors, which omits orientation when the
// source basis was absent).
func poseToVectors(pose *spatialv1.Pose) (pos, fwd, left, up []float64) {
	if p := pose.GetPosition(); p != nil {
		pos = []float64{float64(p.GetX()), float64(p.GetY()), float64(p.GetZ())}
	}
	if q := pose.GetOrientation(); q != nil {
		fwd, left, up = quaternionToAxes(q)
	}
	return pos, fwd, left, up
}

// bytesToFloat32Slice reverses float32SliceToBytes: it unpacks little-endian
// float32 values. Bones are float32 in both v1 and v2, so this is exact.
func bytesToFloat32Slice(b []byte) []float32 {
	n := len(b) / 4
	if n == 0 {
		return nil
	}
	out := make([]float32, n)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

// vec3ToSlice converts a spatial Vec3 to a v1 []float64, or nil when absent.
func vec3ToSlice(v *spatialv1.Vec3) []float64 {
	if v == nil {
		return nil
	}
	return []float64{float64(v.GetX()), float64(v.GetY()), float64(v.GetZ())}
}

// SessionReconstructor rebuilds v1 SessionResponse frames from a v2 capture —
// the reverse of FrameMapper. Session-constant data comes from the v2 header;
// per-frame identity, loadout, grab, and team-role come from the replayed
// Session; the rest is read straight off each EchoArenaFrame.
//
// Fields with no v2 home (see SCHEMA-GAPS.md) are NOT fabricated: they are left
// at their zero value. The round-trip BAC measures and reports them.
type SessionReconstructor struct {
	header   *capturepb.CaptureHeader
	ea       *capturepb.EchoArenaHeader
	session  *Session
	baseTime time.Time
	frames   []*capturepb.Frame
}

// NewSessionReconstructor reads a full v2 capture from r and prepares a
// reconstructor. The reader is consumed; the caller retains ownership of
// closing it.
func NewSessionReconstructor(r *codec.Reader) (*SessionReconstructor, error) {
	hdr, err := r.ReadHeader()
	if err != nil {
		return nil, fmt.Errorf("conversion.NewSessionReconstructor: read header: %w", err)
	}

	var frames []*capturepb.Frame
	for {
		// SEC-001 accumulation guard (belt to the reader's braces): never
		// grow the slice past the reader's frame budget. Raising/disabling
		// the budget on the reader (codec.WithMaxFrameCount,
		// codec.WithoutLimits) flows through here.
		if max := r.Limits().MaxFrameCount; max > 0 && int64(len(frames)) >= max {
			return nil, fmt.Errorf("conversion.NewSessionReconstructor: %d frames accumulated: %w (budget %d)", len(frames), codec.ErrMaxFrameCount, max)
		}
		frame, err := r.ReadFrame()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("conversion.NewSessionReconstructor: read frame %d: %w", len(frames), err)
		}
		frames = append(frames, frame)
	}

	return &SessionReconstructor{
		header:   hdr,
		ea:       hdr.GetEchoArena(),
		session:  NewSession(hdr.GetEchoArena(), frames),
		frames:   frames,
		baseTime: hdr.GetCreatedAt().AsTime(),
	}, nil
}

// FrameCount returns the number of frames available to reconstruct.
func (rc *SessionReconstructor) FrameCount() int { return len(rc.frames) }

// ReconstructFrame rebuilds the v1 LobbySessionStateFrame (timestamp + session +
// bones) for the given frame ordinal. It returns nil for an out-of-range index.
func (rc *SessionReconstructor) ReconstructFrame(i int) *telemetryv1.LobbySessionStateFrame {
	if i < 0 || i >= len(rc.frames) {
		return nil
	}
	f := rc.frames[i]
	ea := f.GetEchoArena()

	ts := rc.baseTime.Add(time.Duration(f.GetTimestampOffsetMs()) * time.Millisecond)
	out := &telemetryv1.LobbySessionStateFrame{
		FrameIndex: f.GetFrameIndex(),
		Timestamp:  timestamppb.New(ts),
		Session:    rc.reconstructSession(i, ea),
	}
	if bones := rc.reconstructBones(ea); bones != nil {
		out.PlayerBones = bones
	}
	return out
}

// reconstructSession rebuilds the per-frame SessionResponse from an
// EchoArenaFrame plus the replayed Session at frame ordinal i.
func (rc *SessionReconstructor) reconstructSession(i int, ea *capturepb.EchoArenaFrame) *enginev1.SessionResponse {
	s := &enginev1.SessionResponse{
		// Session constants (header).
		SessionId:       rc.ea.GetSessionId(),
		SessionIp:       rc.ea.GetSessionIp(),
		ClientName:      rc.ea.GetClientName(),
		MapName:         rc.ea.GetMapName(),
		MatchType:       matchTypeReverse[rc.ea.GetMatchType()],
		PrivateMatch:    rc.ea.GetPrivateMatch(),
		TournamentMatch: rc.ea.GetTournamentMatch(),
		TotalRoundCount: rc.ea.GetTotalRoundCount(),
	}
	if ea == nil {
		return s
	}

	// Per-frame scalars.
	s.GameStatus = gameStatusReverse[ea.GetGameStatus()]
	s.GameClock = float64(ea.GetGameClock())
	s.BluePoints = ea.GetBluePoints()
	s.OrangePoints = ea.GetOrangePoints()
	s.LeftShoulderPressed = float64(ea.GetLeftShoulderPressed())
	s.RightShoulderPressed = float64(ea.GetRightShoulderPressed())
	s.LeftShoulderPressed2 = float64(ea.GetLeftShoulderPressed_2())
	s.RightShoulderPressed2 = float64(ea.GetRightShoulderPressed_2())

	// Round scores survive only as event samples; carry the last
	// ScoreboardUpdated forward (any value before the first sample is not
	// recoverable — see BUGS.md, the score sensor emits no seed at frame 0).
	score := rc.session.ScoreAt(i)
	s.BlueRoundScore = score.BlueRoundScore
	s.OrangeRoundScore = score.OrangeRoundScore

	// The display clock is derived, not carried. It changes every frame while
	// ScoreboardUpdated only fires on a score change, so reading it from the
	// event sample left it stale between goals and empty before the first one.
	s.GameClockDisplay = gameClockDisplay(ea.GetGameClock())

	// Pause: only the enum survives; reconstruct the canonical paused_state
	// string. Sub-fields (teams, timers) have no v2 home.
	s.Pause = &enginev1.PauseState{PausedState: pauseStateReverse[ea.GetPauseState()]}

	// Echo Combat payload state. mapping.go only emits PayloadState when some
	// payload value is non-zero, so this stays nil for every arena capture.
	if payload := ea.GetPayload(); payload != nil {
		s.PayloadMultiplier = float64(payload.GetMultiplier())
		s.PayloadCheckpoint = payload.GetCheckpoint()
		s.PayloadDistance = float64(payload.GetDistance())
		s.PayloadDefenders = payload.GetDefenders()
		s.PayloadSpeed = float64(payload.GetSpeed())
	}

	if disc := ea.GetDisc(); disc != nil {
		s.Disc = reconstructDisc(disc)
	}
	if vr := ea.GetVrRoot(); vr != nil {
		s.Player = reconstructPlayerRoot(vr)
	}

	s.Teams = rc.reconstructTeams(i, ea)
	s.Possession = reconstructPossession(ea, s.Teams)

	return s
}

// gameClockDisplay renders the engine's "MM:SS.CC" clock string from the
// per-frame game clock.
//
// The engine truncates to centiseconds rather than rounding, and zero-pads
// minutes. Verified exact on 13,840 real frames across two recordings — all
// 1023 of testdata/sample.echoreplay and all 12,817 of a January 2026 capture —
// computed from the float32 the tape actually stores, so the float64->float32
// narrowing is already accounted for.
//
// Truncation is unstable within ~5e-6 of a centisecond boundary, which is the
// measured round-trip error on game_clock. Neither recording hit that window;
// the full-corpus run is what would surface it.
func gameClockDisplay(clock float32) string {
	if clock < 0 {
		clock = 0
	}
	// Multiply in float64 to match how the engine's value was formatted; doing
	// it in float32 loses precision the truncation then amplifies.
	centis := int(float64(clock) * 100)
	return fmt.Sprintf("%02d:%02d.%02d", centis/6000, (centis%6000)/100, centis%100)
}

// reconstructTeams groups the flat v2 player list back into v1 teams by the
// team index encoded in flags, preserving player order within each team (which
// is the original order, since mapPlayers appended team-by-team). Empty teams
// present in the source have no v2 representation and are not reconstructed.
func (rc *SessionReconstructor) reconstructTeams(i int, ea *capturepb.EchoArenaFrame) []*enginev1.Team {
	roster := rc.session.RosterAt(i)
	loadout := rc.session.LoadoutAt(i)
	grab := rc.session.GrabAt(i)

	// Group players by team index, tracking encounter order of team indices.
	byTeam := map[int32][]*enginev1.TeamMember{}
	var order []int32
	for _, ps := range ea.GetPlayers() {
		teamIdx := int32(ps.GetFlags()>>teamIndexShift) & teamIndexMask
		if _, seen := byTeam[teamIdx]; !seen {
			order = append(order, teamIdx)
		}
		byTeam[teamIdx] = append(byTeam[teamIdx], reconstructMember(ps, roster, loadout, grab))
	}

	// Emit teams in ascending team-index order (blue, orange, spectator).
	slicesSortInt32(order)
	teams := make([]*enginev1.Team, 0, len(order))
	for _, teamIdx := range order {
		members := byTeam[teamIdx]
		team := &enginev1.Team{Players: members}
		// Team possession is true when any member holds the disc.
		for _, m := range members {
			if m.GetHasPossession() {
				team.HasPossession = true
				break
			}
		}
		teams = append(teams, team)
	}
	return teams
}

// reconstructMember rebuilds a v1 TeamMember from a v2 PlayerState plus the
// per-frame identity/loadout/grab looked up by slot.
func reconstructMember(ps *capturepb.PlayerState, roster map[int32]*capturepb.PlayerInfo, loadout map[int32]Loadout, grab map[int32]Grab) *enginev1.TeamMember {
	slot := ps.GetSlot()
	flags := ps.GetFlags()

	m := &enginev1.TeamMember{
		SlotNumber:      slot,
		Ping:            int32(ps.GetPing()), //nolint:gosec // ping fits int32
		PacketLossRatio: float64(ps.GetPacketLossRatio()),
		IsStunned:       flags&flagStunned != 0,
		IsInvulnerable:  flags&flagInvulnerable != 0,
		IsBlocking:      flags&flagBlocking != 0,
		HasPossession:   flags&flagPossession != 0,
		IsEmotePlaying:  flags&flagEmotePlaying != 0,
	}

	if info, ok := roster[slot]; ok {
		m.AccountNumber = info.GetAccountNumber()
		m.DisplayName = info.GetDisplayName()
		m.JerseyNumber = info.GetJerseyNumber()
		m.Level = info.GetLevel()
	}
	if ld, ok := loadout[slot]; ok {
		m.Weapon = ld.Weapon
		m.Ordnance = ld.Ordnance
		m.TacMod = ld.TacMod
	}
	if g, ok := grab[slot]; ok {
		m.LeftHoldingOnto = g.Left
		m.RightHoldingOnto = g.Right
	}

	if head := ps.GetHead(); head != nil {
		m.Head = poseToBodyPart(head)
	}
	if body := ps.GetBody(); body != nil {
		m.Body = poseToBodyPart(body)
	}
	if lh := ps.GetLeftHand(); lh != nil {
		m.LeftHand = poseToHandPart(lh)
	}
	if rh := ps.GetRightHand(); rh != nil {
		m.RightHand = poseToHandPart(rh)
	}
	m.Velocity = vec3ToSlice(ps.GetVelocity())

	return m
}

func poseToBodyPart(pose *spatialv1.Pose) *enginev1.BodyPart {
	pos, fwd, left, up := poseToVectors(pose)
	return &enginev1.BodyPart{Position: pos, Forward: fwd, Left: left, Up: up}
}

func poseToHandPart(pose *spatialv1.Pose) *enginev1.HandPart {
	pos, fwd, left, up := poseToVectors(pose)
	return &enginev1.HandPart{Pos: pos, Forward: fwd, Left: left, Up: up}
}

func reconstructDisc(disc *capturepb.DiscState) *enginev1.Disc {
	d := &enginev1.Disc{
		BounceCount: int32(disc.GetBounceCount()), //nolint:gosec // bounce count fits int32
		Velocity:    vec3ToSlice(disc.GetVelocity()),
	}
	if pose := disc.GetPose(); pose != nil {
		d.Position, d.Forward, d.Left, d.Up = poseToVectors(pose)
	}
	return d
}

func reconstructPlayerRoot(pose *spatialv1.Pose) *enginev1.PlayerRoot {
	pos, fwd, left, up := poseToVectors(pose)
	return &enginev1.PlayerRoot{VrPosition: pos, VrForward: fwd, VrLeft: left, VrUp: up}
}

// reconstructPossession rebuilds the v1 possession[] = [holderTeamIdx,
// holderIndexInTeam] array from the disc holder, or [-1, -1] when the disc is
// free. The in-team index is the holder's position within its reconstructed
// team, which matches the source ordering.
func reconstructPossession(ea *capturepb.EchoArenaFrame, teams []*enginev1.Team) []int32 {
	holderSlot, hasHolder := discHolder(ea)
	if !hasHolder {
		return []int32{possessionNoneVal, possessionNoneVal}
	}
	for teamIdx, team := range teams {
		for idxInTeam, m := range team.GetPlayers() {
			if m.GetSlotNumber() == holderSlot {
				return []int32{int32(teamIdx), int32(idxInTeam)}
			}
		}
	}
	return []int32{possessionNoneVal, possessionNoneVal}
}

// discHolder returns the slot of the disc holder for a frame. It prefers the
// explicit disc_holder_slot, falling back to the possession flag bit.
func discHolder(ea *capturepb.EchoArenaFrame) (int32, bool) {
	if ea.DiscHolderSlot != nil {
		return ea.GetDiscHolderSlot(), true
	}
	for _, ps := range ea.GetPlayers() {
		if ps.GetFlags()&flagPossession != 0 {
			return ps.GetSlot(), true
		}
	}
	return 0, false
}

// reconstructBones rebuilds the v1 PlayerBonesResponse from v2 PlayerBones.
// Bones are float32 in both formats, so the transform/orientation data is
// exact. The top-level err_code has no v2 home and is left zero.
func (rc *SessionReconstructor) reconstructBones(ea *capturepb.EchoArenaFrame) *enginev1.PlayerBonesResponse {
	pbs := ea.GetPlayerBones()
	if len(pbs) == 0 {
		return nil
	}
	userBones := make([]*enginev1.UserBones, 0, len(pbs))
	for _, pb := range pbs {
		userBones = append(userBones, &enginev1.UserBones{
			PlayerIndex: pb.GetSlot(),
			BoneT:       bytesToFloat32Slice(pb.GetTransforms()),
			BoneO:       bytesToFloat32Slice(pb.GetOrientations()),
		})
	}
	return &enginev1.PlayerBonesResponse{UserBones: userBones}
}

// slicesSortInt32 sorts a small int32 slice ascending (insertion sort; team
// counts are tiny).
func slicesSortInt32(a []int32) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

// ReconstructFile reads a v2 tape and writes a reconstructed .echoreplay file —
// the v2->echoreplay reverse path. It returns the number of frames written.
func ReconstructFile(tapePath, echoReplayPath string) (int, error) {
	reader, err := codec.NewReader(tapePath)
	if err != nil {
		return 0, fmt.Errorf("conversion.ReconstructFile: open tape: %w", err)
	}
	defer reader.Close() //nolint:errcheck // best-effort cleanup; primary errors returned below

	rc, err := NewSessionReconstructor(reader)
	if err != nil {
		return 0, err
	}

	writer, err := codec.NewEchoReplayWriter(echoReplayPath)
	if err != nil {
		return 0, fmt.Errorf("conversion.ReconstructFile: create echoreplay: %w", err)
	}

	written := 0
	for i := 0; i < rc.FrameCount(); i++ {
		frame := rc.ReconstructFrame(i)
		if err := writer.WriteFrame(frame); err != nil {
			writer.Close() //nolint:errcheck // best-effort cleanup on error path
			return written, fmt.Errorf("conversion.ReconstructFile: write frame %d: %w", i, err)
		}
		written++
	}

	if err := writer.Close(); err != nil {
		return written, fmt.Errorf("conversion.ReconstructFile: close echoreplay: %w", err)
	}
	return written, nil
}
