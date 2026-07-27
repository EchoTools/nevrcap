package conversion_test

import (
	"archive/zip"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/echotools/tape/pkg/codec"
	"github.com/echotools/tape/pkg/conversion"
)

// Two float tolerances, by error mechanism:
//
//   - magTol bounds positions, velocities, the display clock, shoulder input,
//     and packet loss. These are a plain float64->float32 narrowing. At the ±40
//     coordinate range a float32 ULP is ~4 µm, so observed error is ~1e-5; the
//     bound is generous.
//   - orientTol bounds forward/left/up basis vectors. v2 stores orientation as a
//     quaternion (a proper rotation). The source basis is written to ~3 decimals
//     (e.g. -0.461), so it is slightly non-orthonormal; the quaternion captures
//     the nearest true rotation and the round-trip returns the *orthonormalized*
//     basis. That residual — not float32 storage — sets the bound. This loss is
//     introduced by the v2 forward mapping (mapping.go), not by reconstruction.
const (
	magTol    = 1e-3
	orientTol = 5e-3
)

// tally accumulates the round-trip comparison: recoverable mismatches (which
// must be zero for the gate to pass) and findings (fields v2 does not preserve,
// measured and reported but not asserted — see SCHEMA-GAPS.md).
type tally struct {
	exactMismatch  map[string]int
	exactExample   map[string]string
	floatOverTol   map[string]int
	floatExample   map[string]string
	finding        map[string]int
	findingExample map[string]string

	frames            int
	playerFrames      int
	maxMagErr         float64
	maxMagErrField    string
	maxOrientErr      float64
	maxOrientErrField string
}

func newTally() *tally {
	return &tally{
		exactMismatch:  map[string]int{},
		exactExample:   map[string]string{},
		floatOverTol:   map[string]int{},
		floatExample:   map[string]string{},
		finding:        map[string]int{},
		findingExample: map[string]string{},
	}
}

func (ta *tally) exact(field string, equal bool, example string) {
	if equal {
		return
	}
	ta.exactMismatch[field]++
	if ta.exactExample[field] == "" {
		ta.exactExample[field] = example
	}
}

// mag records a magnitude-class float (position/velocity/clock/shoulder/loss).
func (ta *tally) mag(field string, a, b float64) {
	d := math.Abs(a - b)
	if d > ta.maxMagErr {
		ta.maxMagErr = d
		ta.maxMagErrField = field
	}
	if d > magTol {
		ta.floatOverTol[field]++
		if ta.floatExample[field] == "" {
			ta.floatExample[field] = fmt.Sprintf("orig=%v recon=%v diff=%g", a, b, d)
		}
	}
}

func (ta *tally) magVec(field string, a, b []float64) {
	if len(a) != len(b) {
		ta.exact(field+".len", false, fmt.Sprintf("orig len=%d recon len=%d", len(a), len(b)))
		return
	}
	for i := range a {
		ta.mag(field, a[i], b[i])
	}
}

// orientVec records an orientation-class basis vector (forward/left/up).
func (ta *tally) orientVec(field string, a, b []float64) {
	if len(a) != len(b) {
		ta.exact(field+".len", false, fmt.Sprintf("orig len=%d recon len=%d", len(a), len(b)))
		return
	}
	for i := range a {
		d := math.Abs(a[i] - b[i])
		if d > ta.maxOrientErr {
			ta.maxOrientErr = d
			ta.maxOrientErrField = field
		}
		if d > orientTol {
			ta.floatOverTol[field]++
			if ta.floatExample[field] == "" {
				ta.floatExample[field] = fmt.Sprintf("orig=%v recon=%v diff=%g", a[i], b[i], d)
			}
		}
	}
}

func (ta *tally) found(field string, present bool, example string) {
	if !present {
		return
	}
	ta.finding[field]++
	if ta.findingExample[field] == "" {
		ta.findingExample[field] = example
	}
}

// teammateSets maps each slot to the sorted set of slots on its team (including
// itself). Comparing these between orig and recon proves team membership
// round-trips independent of team-array indexing or omitted empty teams.
func teammateSets(s *enginev1.SessionResponse) map[int32][]int32 {
	out := map[int32][]int32{}
	for _, team := range s.GetTeams() {
		var slots []int32
		for _, m := range team.GetPlayers() {
			slots = append(slots, m.GetSlotNumber())
		}
		slices.Sort(slots)
		for _, m := range team.GetPlayers() {
			out[m.GetSlotNumber()] = slots
		}
	}
	return out
}

func slotMembers(s *enginev1.SessionResponse) map[int32]*enginev1.TeamMember {
	out := map[int32]*enginev1.TeamMember{}
	for _, team := range s.GetTeams() {
		for _, m := range team.GetPlayers() {
			out[m.GetSlotNumber()] = m
		}
	}
	return out
}

func int32SliceEqual(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func float32SliceEqual(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// compareSession walks one frame's original vs reconstructed SessionResponse.
func compareSession(ta *tally, orig, recon *enginev1.SessionResponse) {
	// Session constants + per-frame scalars (exact).
	ta.exact("session_id", orig.GetSessionId() == recon.GetSessionId(), orig.GetSessionId())
	ta.exact("session_ip", orig.GetSessionIp() == recon.GetSessionIp(), orig.GetSessionIp())
	ta.exact("client_name", orig.GetClientName() == recon.GetClientName(),
		fmt.Sprintf("%q vs %q", orig.GetClientName(), recon.GetClientName()))
	ta.exact("map_name", orig.GetMapName() == recon.GetMapName(), orig.GetMapName())
	ta.exact("match_type", orig.GetMatchType() == recon.GetMatchType(), fmt.Sprintf("%q vs %q", orig.GetMatchType(), recon.GetMatchType()))
	ta.exact("private_match", orig.GetPrivateMatch() == recon.GetPrivateMatch(), "")
	ta.exact("tournament_match", orig.GetTournamentMatch() == recon.GetTournamentMatch(), "")
	ta.exact("total_round_count", orig.GetTotalRoundCount() == recon.GetTotalRoundCount(), "")
	ta.exact("game_status", orig.GetGameStatus() == recon.GetGameStatus(), fmt.Sprintf("%q vs %q", orig.GetGameStatus(), recon.GetGameStatus()))
	ta.exact("blue_points", orig.GetBluePoints() == recon.GetBluePoints(), "")
	ta.exact("orange_points", orig.GetOrangePoints() == recon.GetOrangePoints(), "")
	ta.exact("pause.paused_state", orig.GetPause().GetPausedState() == recon.GetPause().GetPausedState(),
		fmt.Sprintf("%q vs %q", orig.GetPause().GetPausedState(), recon.GetPause().GetPausedState()))
	ta.exact("possession", int32SliceEqual(orig.GetPossession(), recon.GetPossession()),
		fmt.Sprintf("%v vs %v", orig.GetPossession(), recon.GetPossession()))

	// Derived from game_clock during reconstruction rather than carried as an
	// event sample, so it is exact per-frame instead of stale between goals.
	ta.exact("game_clock_display", orig.GetGameClockDisplay() == recon.GetGameClockDisplay(),
		fmt.Sprintf("orig=%q recon=%q", orig.GetGameClockDisplay(), recon.GetGameClockDisplay()))

	// Per-frame floats (float32-narrowed -> magnitude tolerance).
	ta.mag("game_clock", orig.GetGameClock(), recon.GetGameClock())
	ta.mag("left_shoulder", orig.GetLeftShoulderPressed(), recon.GetLeftShoulderPressed())
	ta.mag("right_shoulder", orig.GetRightShoulderPressed(), recon.GetRightShoulderPressed())
	ta.mag("left_shoulder2", orig.GetLeftShoulderPressed2(), recon.GetLeftShoulderPressed2())
	ta.mag("right_shoulder2", orig.GetRightShoulderPressed2(), recon.GetRightShoulderPressed2())

	// Echo Combat payload. Zero on both sides for any arena capture, so this
	// guards regression rather than measuring the sample; TestReconstructPreservesPayload
	// exercises the non-zero path.
	ta.mag("payload_multiplier", orig.GetPayloadMultiplier(), recon.GetPayloadMultiplier())
	ta.exact("payload_checkpoint", orig.GetPayloadCheckpoint() == recon.GetPayloadCheckpoint(), "")
	ta.mag("payload_distance", orig.GetPayloadDistance(), recon.GetPayloadDistance())
	ta.exact("payload_defenders", orig.GetPayloadDefenders() == recon.GetPayloadDefenders(), "")
	ta.mag("payload_speed", orig.GetPayloadSpeed(), recon.GetPayloadSpeed())

	// Disc.
	od, rd := orig.GetDisc(), recon.GetDisc()
	ta.exact("disc.present", (od == nil) == (rd == nil), "")
	if od != nil && rd != nil {
		ta.magVec("disc.position", od.GetPosition(), rd.GetPosition())
		ta.orientVec("disc.forward", od.GetForward(), rd.GetForward())
		ta.orientVec("disc.left", od.GetLeft(), rd.GetLeft())
		ta.orientVec("disc.up", od.GetUp(), rd.GetUp())
		ta.magVec("disc.velocity", od.GetVelocity(), rd.GetVelocity())
		ta.exact("disc.bounce_count", od.GetBounceCount() == rd.GetBounceCount(), "")
	}

	// VR root (capture client).
	op, rp := orig.GetPlayer(), recon.GetPlayer()
	ta.exact("player_root.present", (op == nil) == (rp == nil), "")
	if op != nil && rp != nil {
		ta.magVec("vr_position", op.GetVrPosition(), rp.GetVrPosition())
		ta.orientVec("vr_forward", op.GetVrForward(), rp.GetVrForward())
		ta.orientVec("vr_left", op.GetVrLeft(), rp.GetVrLeft())
		ta.orientVec("vr_up", op.GetVrUp(), rp.GetVrUp())
	}

	compareTeamMembership(ta, orig, recon)
	measureFindings(ta, orig, recon)
}

func compareTeamMembership(ta *tally, orig, recon *enginev1.SessionResponse) {
	origMembers := slotMembers(orig)
	reconMembers := slotMembers(recon)
	origTeams := teammateSets(orig)
	reconTeams := teammateSets(recon)

	ta.exact("player_count", len(origMembers) == len(reconMembers),
		fmt.Sprintf("orig=%d recon=%d", len(origMembers), len(reconMembers)))

	for slot, om := range origMembers {
		ta.playerFrames++
		rm, ok := reconMembers[slot]
		if !ok {
			ta.exact("player.present", false, fmt.Sprintf("slot %d missing in recon", slot))
			continue
		}
		ta.exact("player.teammates", int32SliceEqual(origTeams[slot], reconTeams[slot]),
			fmt.Sprintf("slot %d: %v vs %v", slot, origTeams[slot], reconTeams[slot]))

		// Identity (exact).
		ta.exact("account_number", om.GetAccountNumber() == rm.GetAccountNumber(), fmt.Sprintf("slot %d", slot))
		ta.exact("display_name", om.GetDisplayName() == rm.GetDisplayName(), fmt.Sprintf("slot %d: %q vs %q", slot, om.GetDisplayName(), rm.GetDisplayName()))
		ta.exact("jersey_number", om.GetJerseyNumber() == rm.GetJerseyNumber(), fmt.Sprintf("slot %d", slot))
		ta.exact("level", om.GetLevel() == rm.GetLevel(), fmt.Sprintf("slot %d", slot))
		// Loadout + grab (exact).
		ta.exact("weapon", om.GetWeapon() == rm.GetWeapon(), fmt.Sprintf("slot %d: %q vs %q", slot, om.GetWeapon(), rm.GetWeapon()))
		ta.exact("ordnance", om.GetOrdnance() == rm.GetOrdnance(), "")
		ta.exact("tac_mod", om.GetTacMod() == rm.GetTacMod(), "")
		ta.exact("left_holding_onto", om.GetLeftHoldingOnto() == rm.GetLeftHoldingOnto(),
			fmt.Sprintf("slot %d: %q vs %q", slot, om.GetLeftHoldingOnto(), rm.GetLeftHoldingOnto()))
		ta.exact("right_holding_onto", om.GetRightHoldingOnto() == rm.GetRightHoldingOnto(),
			fmt.Sprintf("slot %d: %q vs %q", slot, om.GetRightHoldingOnto(), rm.GetRightHoldingOnto()))
		// Flags + ping (exact).
		ta.exact("is_stunned", om.GetIsStunned() == rm.GetIsStunned(), "")
		ta.exact("is_invulnerable", om.GetIsInvulnerable() == rm.GetIsInvulnerable(), "")
		ta.exact("is_blocking", om.GetIsBlocking() == rm.GetIsBlocking(), "")
		ta.exact("has_possession", om.GetHasPossession() == rm.GetHasPossession(), "")
		ta.exact("is_emote_playing", om.GetIsEmotePlaying() == rm.GetIsEmotePlaying(), "")
		ta.exact("ping", om.GetPing() == rm.GetPing(), fmt.Sprintf("slot %d: %d vs %d", slot, om.GetPing(), rm.GetPing()))
		// Network + spatial (tolerance).
		ta.mag("packet_loss_ratio", om.GetPacketLossRatio(), rm.GetPacketLossRatio())
		ta.magVec("velocity", om.GetVelocity(), rm.GetVelocity())
		compareBodyPart(ta, "head", om.GetHead(), rm.GetHead())
		compareBodyPart(ta, "body", om.GetBody(), rm.GetBody())
		compareHandPart(ta, "left_hand", om.GetLeftHand(), rm.GetLeftHand())
		compareHandPart(ta, "right_hand", om.GetRightHand(), rm.GetRightHand())
	}
}

func compareBodyPart(ta *tally, name string, a, b *enginev1.BodyPart) {
	ta.exact(name+".present", (a == nil) == (b == nil), "")
	if a == nil || b == nil {
		return
	}
	ta.magVec(name+".position", a.GetPosition(), b.GetPosition())
	ta.orientVec(name+".forward", a.GetForward(), b.GetForward())
	ta.orientVec(name+".left", a.GetLeft(), b.GetLeft())
	ta.orientVec(name+".up", a.GetUp(), b.GetUp())
}

func compareHandPart(ta *tally, name string, a, b *enginev1.HandPart) {
	ta.exact(name+".present", (a == nil) == (b == nil), "")
	if a == nil || b == nil {
		return
	}
	ta.magVec(name+".pos", a.GetPos(), b.GetPos())
	ta.orientVec(name+".forward", a.GetForward(), b.GetForward())
	ta.orientVec(name+".left", a.GetLeft(), b.GetLeft())
	ta.orientVec(name+".up", a.GetUp(), b.GetUp())
}

// measureFindings counts SessionResponse fields that v2 does not preserve. These
// are reported, never asserted: per SCHEMA-GAPS.md they have no v2 home (or only
// an event sample), so a non-zero count here is the round-trip's true loss.
func measureFindings(ta *tally, orig, recon *enginev1.SessionResponse) {
	ta.found("rules_changed_by", orig.GetRulesChangedBy() != recon.GetRulesChangedBy(), fmt.Sprintf("orig=%q recon=%q", orig.GetRulesChangedBy(), recon.GetRulesChangedBy()))
	ta.found("rules_changed_at", orig.GetRulesChangedAt() != recon.GetRulesChangedAt(), fmt.Sprintf("orig=%d recon=%d", orig.GetRulesChangedAt(), recon.GetRulesChangedAt()))
	ta.found("err_code", orig.GetErrCode() != recon.GetErrCode(), fmt.Sprintf("orig=%d", orig.GetErrCode()))
	ta.found("orange_team_restart_request", orig.GetOrangeTeamRestartRequest() != recon.GetOrangeTeamRestartRequest(), fmt.Sprintf("orig=%d", orig.GetOrangeTeamRestartRequest()))
	ta.found("blue_team_restart_request", orig.GetBlueTeamRestartRequest() != recon.GetBlueTeamRestartRequest(), fmt.Sprintf("orig=%d", orig.GetBlueTeamRestartRequest()))
	ta.found("blue_round_score", orig.GetBlueRoundScore() != recon.GetBlueRoundScore(), fmt.Sprintf("orig=%d recon=%d", orig.GetBlueRoundScore(), recon.GetBlueRoundScore()))
	ta.found("orange_round_score", orig.GetOrangeRoundScore() != recon.GetOrangeRoundScore(), fmt.Sprintf("orig=%d recon=%d", orig.GetOrangeRoundScore(), recon.GetOrangeRoundScore()))
	ta.found("last_throw", orig.GetLastThrow() != nil && recon.GetLastThrow() == nil, "present in orig, dropped in recon")
	ta.found("last_score", orig.GetLastScore() != nil && recon.GetLastScore() == nil, "present in orig, dropped in recon")

	// Team-level fields with no v2 home.
	teamCountFinding := len(orig.GetTeams()) != len(recon.GetTeams())
	ta.found("team_count(empty_teams)", teamCountFinding, fmt.Sprintf("orig=%d recon=%d teams", len(orig.GetTeams()), len(recon.GetTeams())))
	for _, team := range orig.GetTeams() {
		ta.found("team_name", team.GetTeamName() != "", team.GetTeamName())
		ta.found("team.stats", team.GetStats() != nil, "team stats dropped")
		for _, m := range team.GetPlayers() {
			ta.found("player.stats", m.GetStats() != nil, "player stats dropped")
		}
	}
}

// runRoundTrip performs echoreplay -> v2 -> echoreplay and compares originals to
// reconstructions, returning the tally. It fails the test only on recoverable
// mismatches; findings are reported by the caller.
func runRoundTrip(t *testing.T, src string) *tally {
	t.Helper()
	dir := t.TempDir()
	tape := filepath.Join(dir, "out.tape")
	recon := filepath.Join(dir, "recon.echoreplay")

	if _, err := conversion.ConvertFile(src, tape); err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}
	n, err := conversion.ReconstructFile(tape, recon)
	if err != nil {
		t.Fatalf("ReconstructFile: %v", err)
	}

	origFrames := readEchoReplay(t, src)

	// Guard the comparison itself: every record physically in the source must
	// have been surfaced by the reader. Without this the round-trip grades the
	// survivors — a capture that parses to zero frames yields orig=0, recon=0,
	// zero mismatches, PASS (BUGS.md READLOSS-001).
	if sourceRecords := countSourceRecords(t, src); len(origFrames) != sourceRecords {
		t.Fatalf("reader surfaced %d of %d records in %s — the comparison below "+
			"would only cover the survivors", len(origFrames), sourceRecords, src)
	}

	reconFrames := readEchoReplay(t, recon)
	if len(origFrames) != len(reconFrames) {
		t.Fatalf("frame count: orig=%d recon=%d (reconstructed %d)", len(origFrames), len(reconFrames), n)
	}

	ta := newTally()
	ta.frames = len(origFrames)
	for i := range origFrames {
		compareSession(ta, origFrames[i].GetSession(), reconFrames[i].GetSession())
		compareBones(ta, origFrames[i].GetPlayerBones(), reconFrames[i].GetPlayerBones())
	}
	return ta
}

func compareBones(ta *tally, a, b *enginev1.PlayerBonesResponse) {
	ta.exact("bones.present", (a == nil) == (b == nil), "")
	if a == nil || b == nil {
		return
	}
	ao := map[int32]*enginev1.UserBones{}
	for _, ub := range a.GetUserBones() {
		ao[ub.GetPlayerIndex()] = ub
	}
	bo := map[int32]*enginev1.UserBones{}
	for _, ub := range b.GetUserBones() {
		bo[ub.GetPlayerIndex()] = ub
	}
	ta.exact("bones.user_count", len(ao) == len(bo), fmt.Sprintf("orig=%d recon=%d", len(ao), len(bo)))
	for idx, ub := range ao {
		rb, ok := bo[idx]
		if !ok {
			ta.exact("bones.user_present", false, fmt.Sprintf("player %d missing", idx))
			continue
		}
		ta.exact("bones.transforms", float32SliceEqual(ub.GetBoneT(), rb.GetBoneT()), fmt.Sprintf("player %d", idx))
		ta.exact("bones.orientations", float32SliceEqual(ub.GetBoneO(), rb.GetBoneO()), fmt.Sprintf("player %d", idx))
	}
}

func readEchoReplay(t *testing.T, path string) []*telemetryv1.LobbySessionStateFrame {
	t.Helper()
	r, err := codec.NewEchoReplayReader(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	frames, err := r.ReadFrames()
	skipped := r.SkippedFrames()
	_ = r.Close()
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if skipped != 0 {
		t.Fatalf("reader skipped %d unparseable line(s) in %s — those records are absent "+
			"from this side of the comparison and would score as a match", skipped, path)
	}
	return frames
}

// countSourceRecords counts the records physically present in an echoreplay,
// without the codec in the path: every non-empty line across every zip member,
// or every non-empty line of the file when it is not a zip.
//
// The round-trip compares parsed originals against parsed reconstructions
// through the same reader, so anything that reader drops is missing from both
// sides and scores as a match (BUGS.md READLOSS-001). Comparing frames against
// this number is what makes reader-level loss visible: it also catches loss the
// skip counter cannot see, such as a second zip member that initScanner never
// opens.
func countSourceRecords(t *testing.T, path string) int {
	t.Helper()

	countLines := func(r io.Reader) int {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
		n := 0
		for scanner.Scan() {
			if len(scanner.Bytes()) > 0 {
				n++
			}
		}
		if err := scanner.Err(); err != nil {
			t.Fatalf("scan %s: %v", path, err)
		}
		return n
	}

	zr, err := zip.OpenReader(path)
	if err != nil {
		// Not a zip: the recorder wrote raw NDJSON.
		f, openErr := os.Open(path)
		if openErr != nil {
			t.Fatalf("open %s: %v", path, openErr)
		}
		defer f.Close() //nolint:errcheck // read-only test fixture
		return countLines(f)
	}
	defer zr.Close() //nolint:errcheck // read-only test fixture

	total := 0
	for _, member := range zr.File {
		rc, err := member.Open()
		if err != nil {
			t.Fatalf("open member %s of %s: %v", member.Name, path, err)
		}
		total += countLines(rc)
		_ = rc.Close()
	}
	return total
}

// report logs the tally. When strict, any recoverable mismatch fails the test
// (the committed-sample acceptance gate). When not strict (the external audit),
// recoverable mismatches are reported but do not fail: a richer capture can
// exercise per-frame variation that v2 stores only as a snapshot (jersey/level
// captured at join; paused_requested narrowed to PAUSED), which the sample does
// not contain. Those are reported here and called out as findings.
func report(t *testing.T, src string, ta *tally, strict bool) {
	t.Helper()
	t.Logf("ROUND-TRIP echoreplay -> v2 -> echoreplay | file=%s frames=%d player-frames=%d", src, ta.frames, ta.playerFrames)
	t.Logf("max magnitude deviation (pos/vel/clock/shoulder/loss): %g (field %s, tol %g)", ta.maxMagErr, ta.maxMagErrField, magTol)
	t.Logf("max orientation deviation (forward/left/up basis):     %g (field %s, tol %g)", ta.maxOrientErr, ta.maxOrientErrField, orientTol)

	if len(ta.exactMismatch) == 0 && len(ta.floatOverTol) == 0 {
		t.Logf("RECOVERABLE FIELDS: exact on all non-spatial, within tolerance on all spatial. 0 mismatches.")
	}
	logOrFail := t.Logf
	if strict {
		logOrFail = t.Errorf
	}
	for _, f := range sortedKeys(ta.exactMismatch) {
		logOrFail("NON-SPATIAL MISMATCH %s: %d frames/players, e.g. %s", f, ta.exactMismatch[f], ta.exactExample[f])
	}
	for _, f := range sortedKeys(ta.floatOverTol) {
		logOrFail("SPATIAL OVER TOLERANCE %s: %d, e.g. %s", f, ta.floatOverTol[f], ta.floatExample[f])
	}

	if len(ta.finding) == 0 {
		t.Logf("FINDINGS: none — this file round-trips losslessly.")
		return
	}
	t.Logf("FINDINGS (fields v2 does not preserve; reported, not asserted — see SCHEMA-GAPS.md):")
	for _, f := range sortedKeys(ta.finding) {
		t.Logf("  %-32s does NOT round-trip in %d frames/teams/players; e.g. %s", f, ta.finding[f], ta.findingExample[f])
	}
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestCountSourceRecordsSeesWhatTheReaderDrops proves the guard in runRoundTrip
// has teeth. It builds a capture whose trailing lines the reader cannot parse
// and shows the two numbers diverge — which is exactly the condition that used
// to pass silently, because both sides of the comparison were built from the
// survivors.
func TestCountSourceRecordsSeesWhatTheReaderDrops(t *testing.T) {
	sample := filepath.Join("..", "..", "testdata", "sample.echoreplay")
	if _, err := os.Stat(sample); err != nil {
		t.Skipf("no sample: %v", err)
	}

	zr, err := zip.OpenReader(sample)
	if err != nil {
		t.Fatalf("open sample: %v", err)
	}
	member, err := zr.File[0].Open()
	if err != nil {
		t.Fatalf("open member: %v", err)
	}
	body, err := io.ReadAll(member)
	_ = member.Close()
	_ = zr.Close()
	if err != nil {
		t.Fatalf("read member: %v", err)
	}

	const (
		keep    = 20
		garbage = 2
	)
	lines := bytes.SplitN(body, []byte("\r\n"), keep+1)[:keep]
	corrupt := append(bytes.Join(lines, []byte("\r\n")),
		[]byte("\r\nNOT A FRAME LINE\r\nALSO NOT A FRAME LINE\r\n")...)

	path := filepath.Join(t.TempDir(), "corrupt.echoreplay")
	if err := os.WriteFile(path, corrupt, 0o600); err != nil {
		t.Fatalf("write corrupt capture: %v", err)
	}

	if got, want := countSourceRecords(t, path), keep+garbage; got != want {
		t.Fatalf("countSourceRecords = %d, want %d", got, want)
	}

	r, err := codec.NewEchoReplayReader(path)
	if err != nil {
		t.Fatalf("open corrupt capture: %v", err)
	}
	frames, err := r.ReadFrames()
	skipped := r.SkippedFrames()
	_ = r.Close()
	if err != nil {
		t.Fatalf("read corrupt capture: %v", err)
	}

	if len(frames) != keep {
		t.Errorf("reader surfaced %d frames, want %d", len(frames), keep)
	}
	if skipped != garbage {
		t.Errorf("SkippedFrames = %d, want %d", skipped, garbage)
	}
	if len(frames) == countSourceRecords(t, path) {
		t.Error("frames equal source records on a capture with unparseable lines; " +
			"the runRoundTrip guard would not fire")
	}
}

// TestRoundTripBAC is the acceptance gate: on the committed sample, every
// recoverable SessionResponse/bones field must round-trip exactly (non-spatial)
// or within float32 tolerance (spatial). Fields with no v2 home are reported as
// findings, not asserted.
func TestRoundTripBAC(t *testing.T) {
	src := filepath.Join("..", "..", "testdata", "sample.echoreplay")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no sample: %v", err)
	}
	ta := runRoundTrip(t, src)
	report(t, src, ta, true)
}

// TestRoundTripBACAudit runs the same comparison on a larger external recording
// when TAPE_AUDIT_FILE is set (e.g. the alienq capture). It reports numbers and
// findings without failing on capture-specific recoverable gaps (see report).
func TestRoundTripBACAudit(t *testing.T) {
	src := os.Getenv("TAPE_AUDIT_FILE")
	if src == "" {
		t.Skip("set TAPE_AUDIT_FILE=/path/to.echoreplay to run the audit round-trip")
	}
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no audit file: %v", err)
	}
	ta := runRoundTrip(t, src)
	report(t, src, ta, false)
}
