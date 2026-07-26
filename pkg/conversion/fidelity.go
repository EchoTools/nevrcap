package conversion

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/echotools/tape/pkg/codec"
	"github.com/echotools/tape/pkg/fidelity"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fidelity.go — per-FILE round-trip verification: did THIS recording survive
// echoreplay -> v2 -> echoreplay, completely?
//
// This is ordinary library code, not a test helper, on purpose. The question it
// answers is whether an irreplaceable recording can be deleted in favour of its
// converted form, so the check has to be callable at conversion time and its
// verdict has to be a structured receipt. A check that only runs inside `go
// test` is deletable, skippable, and can be walked past with a strict=false
// flag — all three of which already happened to the fidelity checks in this
// package.
//
// The comparison itself is pkg/fidelity: a descriptor-driven differ that walks
// every field of every message, so nothing here enumerates fields by hand.
// What this file owns is POLICY:
//
//   - which messages an .echoreplay frame line actually carries (VerifiedFrameFields),
//   - how to pair repeated elements across a reconstruction that reorders and
//     omits them (echoListKeys),
//   - which float loss mechanisms are physical rather than a defect (echoTolerance),
//   - and KnownUnpreserved: the single allowlist of accepted loss.

// Two float tolerances, by error mechanism:
//
//   - magTol bounds positions, velocities, the display clock, shoulder input,
//     packet loss and every other float64 leaf. These are a plain
//     float64->float32 narrowing. At the ±40 coordinate range a float32 ULP is
//     ~4 µm, so observed error is ~1e-5; the bound is generous.
//   - orientTol bounds forward/left/up basis vectors. v2 stores orientation as a
//     quaternion (a proper rotation). The source basis is written to ~3 decimals
//     (e.g. -0.461), so it is slightly non-orthonormal; the quaternion captures
//     the nearest true rotation and the round-trip returns the *orthonormalized*
//     basis. That residual — not float32 storage — sets the bound. This loss is
//     introduced by the v2 forward mapping (mapping.go), not by reconstruction.
//
// Neither tolerance applies to a `float` (32-bit) leaf: bone_t/bone_o are
// already float32 in the source, nothing narrows them, and they round-trip
// exactly. Giving them a tolerance would be a licence to be wrong.
const (
	magTol    = 1e-3
	orientTol = 5e-3
)

// KnownUnpreserved is the explicit allowlist of field paths that are KNOWN not
// to survive echoreplay -> v2 -> echoreplay today. It is seeded from what is
// actually observed on the committed sample and on real chi1 recordings — not
// from the schema, and not from guesswork.
//
// A difference on a path in this map is reported but does not fail. A
// difference on ANY other path FAILS: a gate that cannot fail on the loss it
// detects is not a gate.
//
// Keys are field PATHS in the differ's own path language, and a key covers
// EXACTLY the path it names — plus that path's #presence / #count forms, which
// are the same field failing in a different way. It does NOT cover sub-fields:
// "SessionResponse.last_throw has no v2 home" excuses last_throw going missing,
// and excuses nothing about a reconstruction that returns a WRONG VALUE for
// last_throw.arm_speed. Subtree absorption made 52 of 138 SessionResponse paths
// unfailable; per Andrew's ruling ("errors on ... any fields/differences in the
// round trip source and target") each sub-field now needs its own ruling and
// its own entry, or it fails. See BUGS.md FIDELITY-002.
//
// This map is the mechanism by which a future fix is proven: when a field gains
// a v2 home and round-trips, DELETE its entry — the gate then holds that field
// losslessly forever after. When this map is empty, v2 is a true superset of
// engine.v1.SessionResponse and BUGS.md FIDELITY-001 can close.
//
// Do NOT add an entry to silence a new failure without a BUGS.md citation and
// Andrew's call: a new key here is a new documented hole in the archive format.
var KnownUnpreserved = fidelity.Allowlist{
	// BUGS.md DIRECTIVE 2026-06-29, still-OPEN: "rules_changed_by /
	// rules_changed_at — no v2 home." Observed on sample (orig="Milkyway",
	// rules_changed_at=573360097) and chi1 (orig="[INVALID]").
	"SessionResponse.rules_changed_by": "BUGS.md FIDELITY-001 / DIRECTIVE still-open: rules_changed_by has no v2 home",
	"SessionResponse.rules_changed_at": "BUGS.md FIDELITY-001 / DIRECTIVE still-open: rules_changed_at has no v2 home",

	// BUGS.md DIRECTIVE still-open: "per-frame last_throw and last_score (incl.
	// scorer/assist names — v2 GoalScored is slot-only) — v2 only carries
	// throws/goals as events." Observed present-in-orig, nil-in-recon on every
	// frame of both files. Subtree entries: the whole message has no v2 home,
	// so every sub-field of it is the same documented hole.
	"SessionResponse.last_throw": "BUGS.md FIDELITY-001 / DIRECTIVE still-open: per-frame last_throw is only carried as an event",
	"SessionResponse.last_score": "BUGS.md FIDELITY-001 / DIRECTIVE still-open: per-frame last_score is only carried as an event",

	// BUGS.md DIRECTIVE still-open: "per-frame game_clock_display +
	// blue/orange_round_score — only event-sampled (ScoreboardUpdated); BUG: the
	// score sensor seeds frame 0 silently (no event), so any pre-first-change
	// value is unrecoverable." Observed: orig="11:02.80"/"10:00.00", recon="".
	// NOTE: blue_round_score / orange_round_score are deliberately NOT
	// allowlisted — the gate holds them.
	"SessionResponse.game_clock_display": "BUGS.md FIDELITY-001 / DIRECTIVE still-open: game_clock_display is only event-sampled; score sensor does not seed frame 0",

	// BUGS.md DIRECTIVE still-open: "team_name (string) — roster stores Role
	// enum only." Observed on every team of every frame in both files.
	"SessionResponse.teams[].team_name": "BUGS.md FIDELITY-001 / DIRECTIVE still-open: team_name has no v2 home (roster stores Role enum only)",

	// BUGS.md DIRECTIVE still-open: "per-frame team.stats / player.stats;
	// empty-team structural case." Observed on every team and every player of
	// both files; the empty (spectator) team is dropped, so orig=3 teams ->
	// recon=2. Subtree entries for the same reason as last_throw: the stats
	// message has no v2 home at all.
	"SessionResponse.teams[].stats":              "BUGS.md FIDELITY-001 / DIRECTIVE still-open: per-frame team.stats has no v2 home",
	"SessionResponse.teams[].players[].stats":    "BUGS.md FIDELITY-001 / DIRECTIVE still-open: per-frame player.stats has no v2 home",
	"SessionResponse.teams[]#count":              "BUGS.md FIDELITY-001 / DIRECTIVE still-open: empty-team structural case — teams with no players are not reconstructed",
	"SessionResponse.teams[].players[]#presence": "BUGS.md FIDELITY-001 / DIRECTIVE still-open: empty-team structural case — teams with no players are not reconstructed",
}

// VerifiedFrameFields classifies EVERY field of telemetry.v1
// LobbySessionStateFrame, which is what the echoreplay reader produces from one
// frame line. A field is either verified by the round-trip comparison or
// explicitly recorded as not being file content — there is no third category,
// and TestFrameFieldsAreClassified fails the day a field appears that is in
// neither map. That is how "everything is compared" stays true after someone
// edits the proto.
var VerifiedFrameFields = map[string]string{
	"timestamp":    "frame line field 0 — compared as its own schema root",
	"session":      "frame line field 1 — compared as the SessionResponse root",
	"player_bones": "frame line field 2 — compared as the PlayerBonesResponse root",
}

// NotFileContent are LobbySessionStateFrame fields that no .echoreplay line
// carries: they are assigned or derived by the reader/converter, so comparing
// them would measure this repository's own enrichment, not the recording.
var NotFileContent = map[string]string{
	"frame_index": "assigned by the reader from line order; not present in the file",
	"events":      "produced by pkg/events detection during conversion; not present in the file",
}

// echoTolerance picks a comparison tolerance from the loss MECHANISM, by field
// kind and name, not from a hand-listed set of paths.
func echoTolerance(fd protoreflect.FieldDescriptor, _ string) float64 {
	if fd.Kind() != protoreflect.DoubleKind {
		return 0 // exact: float32 leaves do not narrow, and nothing else may drift
	}
	switch fd.Name() {
	case "forward", "left", "up", "vr_forward", "vr_left", "vr_up":
		return orientTol
	}
	return magTol
}

// echoListKeys pairs repeated message elements by identity instead of index.
//
// This is alignment, never field selection. Reconstruction omits empty teams
// and may order teams differently, so teams[1] is not the same team on both
// sides; comparing by index would report every field of every player as
// different and bury the real loss.
var echoListKeys = map[string]fidelity.KeyFunc{
	"engine.v1.SessionResponse.teams":          teamSlotSetKey,
	"engine.v1.Team.players":                   slotNumberKey,
	"engine.v1.PlayerBonesResponse.user_bones": playerIndexKey,
}

var (
	descOnce      sync.Once
	fdTeamPlayers protoreflect.FieldDescriptor
	fdSlotNumber  protoreflect.FieldDescriptor
	fdPlayerIndex protoreflect.FieldDescriptor
)

func initDescriptors() {
	descOnce.Do(func() {
		team := (&enginev1.Team{}).ProtoReflect().Descriptor()
		fdTeamPlayers = team.Fields().ByName("players")
		member := (&enginev1.TeamMember{}).ProtoReflect().Descriptor()
		fdSlotNumber = member.Fields().ByName("slot_number")
		bones := (&enginev1.UserBones{}).ProtoReflect().Descriptor()
		fdPlayerIndex = bones.Fields().ByName("player_index")
	})
}

// teamSlotSetKey identifies a team by the sorted slot numbers of its players,
// which is the only identity a team has that survives the round trip (team_name
// does not).
func teamSlotSetKey(m protoreflect.Message) string {
	initDescriptors()
	players := m.Get(fdTeamPlayers).List()
	slots := make([]int, 0, players.Len())
	for i := 0; i < players.Len(); i++ {
		slots = append(slots, int(players.Get(i).Message().Get(fdSlotNumber).Int()))
	}
	sort.Ints(slots)
	var b strings.Builder
	for i, s := range slots {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(s))
	}
	return b.String()
}

func slotNumberKey(m protoreflect.Message) string {
	initDescriptors()
	return strconv.FormatInt(m.Get(fdSlotNumber).Int(), 10)
}

func playerIndexKey(m protoreflect.Message) string {
	initDescriptors()
	return strconv.FormatInt(m.Get(fdPlayerIndex).Int(), 10)
}

// --- schemas -----------------------------------------------------------------

var (
	schemaOnce                                  sync.Once
	sessionSchema, bonesSchema, timestampSchema *fidelity.Schema
	schemaErr                                   error
)

func echoSchemas() (session, bones, ts *fidelity.Schema, err error) {
	schemaOnce.Do(func() {
		opts := fidelity.Options{ListKeys: echoListKeys, Tolerance: echoTolerance}
		sessionSchema, schemaErr = fidelity.New(&enginev1.SessionResponse{}, opts)
		if schemaErr != nil {
			return
		}
		bonesSchema, schemaErr = fidelity.New(&enginev1.PlayerBonesResponse{}, opts)
		if schemaErr != nil {
			return
		}
		timestampSchema, schemaErr = fidelity.New(&timestamppb.Timestamp{}, opts)
	})
	return sessionSchema, bonesSchema, timestampSchema, schemaErr
}

// EchoReplaySchemas returns the comparison plans used to verify an .echoreplay
// round trip: the three messages a frame line carries.
func EchoReplaySchemas() ([]*fidelity.Schema, error) {
	s, b, ts, err := echoSchemas()
	if err != nil {
		return nil, err
	}
	return []*fidelity.Schema{ts, s, b}, nil
}

// VerifyOptions configures VerifyEchoReplayRoundTrip.
type VerifyOptions struct {
	// Allowlist overrides KnownUnpreserved. A nil Allowlist means "no loss is
	// acceptable" — which is what a caller deciding whether to delete an
	// original should be asking.
	Allowlist fidelity.Allowlist
	// WorkDir holds the intermediate .tape and reconstructed .echoreplay. If
	// empty a temporary directory is created and removed.
	WorkDir string
}

// There is deliberately NO option to skip a lane.
//
// There was one — SkipKeyScan, "so a caller that has already scanned the file
// does not pay twice". With it set, a file carrying 1023 discarded JSON keys
// returned FIDELITY PASS and the receipt still printed "keyscan=0/0 frames
// (exhaustive) unknown-keys=0": a claim of exhaustive coverage over a scan that
// never ran. The saving was one pass over the bytes; the cost was a verdict
// that authorizes deleting an irreplaceable recording on the strength of work
// nobody did. The knob is gone, the scan is unconditional, and
// fidelity.KeyScanResult.Ran makes a skipped scan visible and fatal if some
// future caller ever assembles a Verdict by hand.

// The intermediate artifacts a verification produces inside its WorkDir. They
// are named so a caller that supplies a WorkDir can inspect or reuse them —
// notably `tapedeck verify`, which runs its event checks against the tape the
// verification already wrote instead of converting the recording a second time.
const (
	VerifyTapeName           = "verify.tape"
	VerifyReconstructionName = "verify-recon.echoreplay"
)

// VerifyEchoReplayRoundTrip converts one .echoreplay to v2, reconstructs it
// back, and compares the reconstruction against the original EXHAUSTIVELY:
// every frame, every field of every message, no sampling.
//
// It streams both files frame by frame and holds one frame per side at a time,
// so a 100k-frame recording costs no more memory than a 100-frame one. (The
// conversion and reconstruction steps themselves still materialize, which is a
// property of ConvertFile/ReconstructFile, not of this comparison.)
//
// The returned Verdict is the structured result: source identity, coverage,
// every differing field path with counts and an example, and a single
// pass/fail. An error is returned only when the verification could not be
// performed at all; a file that fails to round-trip is a Verdict with
// Pass()==false, not an error.
func VerifyEchoReplayRoundTrip(src string, opts VerifyOptions) (verdict *fidelity.Verdict, err error) {
	sessionS, bonesS, tsS, err := echoSchemas()
	if err != nil {
		return nil, err
	}

	v := &fidelity.Verdict{}
	if idErr := v.Identify(src); idErr != nil {
		return nil, fmt.Errorf("identify %s: %w", src, idErr)
	}

	work := opts.WorkDir
	if work == "" {
		work, err = os.MkdirTemp("", "tape-fidelity-")
		if err != nil {
			return nil, err
		}
		defer func() { err = errors.Join(err, os.RemoveAll(work)) }()
	}
	tapePath := filepath.Join(work, VerifyTapeName)
	reconPath := filepath.Join(work, VerifyReconstructionName)

	if _, convErr := ConvertFile(src, tapePath); convErr != nil {
		return nil, fmt.Errorf("convert %s: %w", src, convErr)
	}
	if _, recErr := ReconstructFile(tapePath, reconPath); recErr != nil {
		return nil, fmt.Errorf("reconstruct %s: %w", src, recErr)
	}

	ks, scanErr := fidelity.ScanEchoReplayKeys(src)
	if scanErr != nil {
		return nil, fmt.Errorf("key-set guard on %s: %w", src, scanErr)
	}
	v.KeyScan = ks

	tsC := tsS.NewComparator()
	sessionC := sessionS.NewComparator()
	bonesC := bonesS.NewComparator()

	origR, err := codec.NewEchoReplayReader(src)
	if err != nil {
		return nil, fmt.Errorf("open original: %w", err)
	}
	defer func() { err = errors.Join(err, origR.Close()) }()
	reconR, err := codec.NewEchoReplayReader(reconPath)
	if err != nil {
		return nil, fmt.Errorf("open reconstruction: %w", err)
	}
	defer func() { err = errors.Join(err, reconR.Close()) }()

	for {
		a, aerr := readNextFrame(origR)
		b, berr := readNextFrame(reconR)
		if aerr != nil {
			v.Errors = append(v.Errors, fmt.Sprintf("read original frame %d: %v", v.FramesOriginal, aerr))
		}
		if berr != nil {
			v.Errors = append(v.Errors, fmt.Sprintf("read reconstruction frame %d: %v", v.FramesRecon, berr))
		}
		if a == nil && b == nil {
			break
		}
		if a != nil {
			v.FramesOriginal++
		}
		if b != nil {
			v.FramesRecon++
		}
		if a == nil || b == nil {
			// One side ran out. The frame counts already record it; there is
			// nothing to compare it against.
			continue
		}
		label := "frame " + strconv.Itoa(v.FramesOriginal-1)
		tsC.Compare(a.GetTimestamp(), b.GetTimestamp(), label)
		sessionC.Compare(a.GetSession(), b.GetSession(), label)
		bonesC.Compare(a.GetPlayerBones(), b.GetPlayerBones(), label)
	}
	v.SkippedFrames = int(origR.SkippedFrames()) + int(reconR.SkippedFrames())

	for _, c := range []*fidelity.Comparator{tsC, sessionC, bonesC} {
		v.Roots = append(v.Roots, fidelity.RootReport{
			Root:        c.RootName(),
			Compared:    c.Compared(),
			SchemaPaths: c.SchemaSize(),
			Reached:     c.Reached(),
			Unreached:   c.Unreached(),
			Diffs:       c.Diffs(),
		})
	}

	allow := opts.Allowlist
	v.Classify(allow)
	// Last, and only here: every lane has now run. Before this call the verdict
	// fails closed, so an abort on any path above can never be mistaken for a
	// clean result.
	v.MarkComplete()
	return v, nil
}

// readNextFrame returns the next frame, or nil at end of file.
func readNextFrame(r *codec.EchoReplay) (*telemetryv1.LobbySessionStateFrame, error) {
	f, err := r.ReadFrame()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, err
	}
	return f, nil
}
