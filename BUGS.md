# BUGS.md — tape

Bug + work ledger for `tape`. One entry per issue: what, where, evidence,
status. Check this before idling. (Convention: every repo gets a root BUGS.md.)

---

## OPEN — RELEASE-001: `feat/obvious-batch-2.1.0` carries a TEMP go.mod replace (pre-merge gate)

**Severity:** release-blocker for merging `feat/obvious-batch-2.1.0` to main. Not a
code bug.

**What:** The obvious-batch items (SemVer 2.1.0 header fields, `FrameEncoding`,
`GoalScored.person_scored/assist_scored`) depend on telemetry/v2 proto additions
that live in `nevr-proto` branch `feat/obvious-batch-2.1.0` and are **not yet
published to the BSR** (`buf.build/echotools/nevr-api`). So `tape/go.mod` on this
branch carries a **temporary dev-local `replace`** pointing at nevr-proto's local
`buf generate` output, marked `// TEMP pre-BSR-publish shim`. Without it the tape
wire-ups do not compile against the pinned BSR module.

**Pre-merge gate (hard invariant — main's go.mod NEVER carries this replace):**
before merging `feat/obvious-batch-2.1.0` to main, the `nevr-proto` proto changes
must be merged and BSR-published, then in tape: **remove the `replace` line and
bump the `buf.build/gen/go/echotools/nevr-api/protocolbuffers/go` require to the
newly-published nevr-api version**, and confirm `go build ./...` + `go test -race
./...` green against that BSR pin.

**Evidence:** dropping the replace on this branch fails to build with 6 compile
errors (`unknown field FormatMinor…`, `undefined: capturepb.FrameEncoding_…`,
`gs.PersonScored undefined`, etc.) because the BSR module lacks the new fields.
The local codegen harness (`nevr-proto/buf.gen.yaml`, `docs/local-codegen.md`) is
byte-parity with the BSR SDK plus these additive fields, so the golden regenerated
now will match post-publish BSR output.

---

## OPEN — CANONICAL-001: `echoreplay -> tape -> echoreplay` is not byte-identical

**Severity:** blocks treating a `.tape` as a byte-faithful replacement for its
source. Structure IS preserved; three things stop byte identity.

**How it is measured:** `pkg/conversion/canonical_test.go`. The comparison is
against the CANONICAL form of the original — the source read and rewritten
through the echoreplay writer — not the raw source bytes, so recorder drift
(Spark booleans, exponent spelling, absent `client_name`) is normalised out and
only v2 loss remains. `TestCanonicalRoundTripStructure` asserts record count,
per-record field count and key sets. `TestCanonicalRoundTripByteDelta` reports
the byte delta without failing; flip it to an assertion when this entry closes.

**Status on the committed sample:** structure identical — 1023 records, no key
added, none dropped. Byte delta 8,854,398 vs 9,502,522.

### 1. Float spelling (not data loss)

The engine writes floats as `%.8g` with trailing zeros trimmed to at least one
decimal place; protojson writes shortest-round-trip float64. So `-1.309` comes
back as `-1.309000015258789`.

**Every engine float is exactly a float32**, verified by reproducing the source
token from `float32(value)` with that format: 38,700/38,700 on the sample,
133,707/133,707 on a January 2026 capture, and 100% on three dal1 captures
(the only misses were exponent spelling, `1.5345009e24` vs `e+24`, which
`FixExponentNotation` already handles). So v2's float32 storage loses nothing
here — the writer's number formatting is the entire gap.

**Fix direction:** format floats engine-style in the echoreplay writer, as a
byte-level pass alongside `FixProtojsonUint64Encoding` / `FixExponentNotation`.
Note this is on the write hot path.

### 2. `last_throw` / `last_score` (real, known)

Not wired through v2 at either end. Differs on 100% of frames. See §4 of
`docs/format-design.md`; `last_throw` is local-player-only, so read
[[last-throw-is-local-player-only]] before wiring.

### 3. Bones payload shape is not representable (real, new)

An echoreplay line has 2 or 3 tab-separated fields, and the third has three
distinct forms in the wild. v2 collapses them:

| source line | representable in v2 |
|---|---|
| `ts\tsession` | yes |
| `ts\tsession\t` — zero-length payload | **no** |
| `ts\tsession\t{"user_bones":[],"err_code":0}` | **no** |
| `ts\tsession\t{"user_bones":[...]}` | yes |

`mapPlayerBones` returns nil when `len(userBones) == 0`, so "bones present but
empty" and "bones absent" are the same tape. Reconstruction then emits a
2-field line where the source had 3.

Measured on dal1: 132 of the first 5000 lines of
`rec_2025-08-01_18-37-52_5990EAE3…` carry an empty `user_bones`; zero-length
payloads appear in 13 of 3000-line samples across 12 files. This is what
`TestCanonicalRoundTripAudit` fails on today (`record 0: field count
canonical=3 via-tape=2`).

Note the zero-length case is also **silently dropped on read**:
`parseFrameLine` only parses `parts[2]` when non-empty, and a bones payload that
fails to parse is discarded without incrementing `SkippedFrames`
(`pkg/codec/echoreplay.go` — `if err := ...Unmarshal(bonesData, userBones); err
== nil`). So bones loss is invisible to READLOSS-002's counter.

**Fix direction:** v2 needs to distinguish the three payload forms — a
`bones_present` flag or an optional empty `PlayerBones` — and the reader should
count a bones payload it could not parse.

---

## FIXED — ROSTER-001: a 2-frame engine glitch at join poisoned jersey/level for the whole session

**What:** echovr reports `jersey_number` and `level` as 0 for roughly the first
two frames after a player joins. tape's join sensor latched the join frame, so
`PlayerJoined` captured `(0,0)` and reconstruction replayed it for every
subsequent frame.

**Measured** on `rec_2026-01-19_22-50-54.echoreplay` (12,817 frames, 9 players):

| slot | player | distinct (number, level) | detail |
|---|---|---|---|
| 0-7 | (eight players) | **1** each | constant all 12,817 frames |
| 8 | `iluvfemboys` | **2** | `(0,0)` on frames 243-244, `(1,1)` on 12,572 |

Frame 243 is exactly that player's first frame. The BAC reported 12,572
mismatches — precisely the settled-frame count.

Same signature previously recorded on alienq (slot 10: 1/50 for 18,865 frames,
0/0 for 3).

**Fix:** treat the roster fields as occasionally-changing rather than constant,
and record the correction as a delta — `PlayerInfoUpdated` (nevr-proto
`5ecc03d`), synthesized v2-side in `appendLoadoutGrabEvents` alongside
`LoadoutChanged`/`GrabChanged`, replayed over the roster by `Session.replay()`.

**Why not a heuristic.** "Discard a 0 at join and use the next value" would
fabricate: jersey 0 is legitimate — two players in that same capture have it —
so it would emit `(1,1)` on frames where the source genuinely said `(0,0)` and
break fidelity in the other direction. Recording what the engine said keeps BOTH
the glitched frames and the settled ones exact.

**Why not per-frame.** The format is delta-based; occasional changes are events.
Putting jersey/level on `PlayerState` would duplicate a session constant across
every frame to accommodate a two-frame artifact.

**Evidence:** 12,572 mismatches → **0** on the audit capture. `jersey_number`
and `level` no longer appear in the mismatch list at all. Golden unchanged
(byte-identical, sha256 `9e51e60d…`) — the committed sample has no glitched
join, so zero events fire and proto3 writes nothing.

Tests: `TestGlitchedJoinRoundTrips` (`pkg/conversion/player_info_test.go`)
reproduces the exact shape and asserts the glitched frames round-trip as `(0,0)`
and the settled ones as `(1,1)`.

**Upstream:** the engine-side read is Andrew's to patch. Worth checking whether
other `TeamMember` fields share the same post-join window, since anything else
snapshotted at join inherits the same failure.

---

## FIXED — READLOSS-001: the round-trip BAC could not detect anything the reader dropped

**Severity:** high (verification integrity). The gate reported lossless on files
from which nothing survived.

**What:** `runRoundTrip` (`pkg/conversion/roundtrip_v2_test.go`) reads the
original with `readEchoReplay(src)` and the reconstruction with
`readEchoReplay(recon)` — the **same reader**. Lines that reader silently skips
never enter `origFrames`, so they are never compared; and the reconstruction was
written by tape in tape's own dialect, so it parses 100%. The only frame-count
check is `len(origFrames) != len(reconFrames)` — survivor set against survivor
set.

Feed it a GH #31 March-2026 capture (450 lines, all rejected): `origFrames` is
empty, `reconFrames` is empty, lengths match, zero mismatches, **PASS**.

**Where:** `pkg/conversion/roundtrip_v2_test.go` `runRoundTrip`;
`pkg/codec/echoreplay.go:504-508` (`skippedFrames++; continue`).

**Status: FIXED** in `e602c0d`. Two guards in `pkg/conversion/roundtrip_v2_test.go`.

1. `readEchoReplay` now fails if the reader reported any skipped line, on either
   side of the comparison.
2. `countSourceRecords` counts records physically present in the container —
   every non-empty line across **every** zip member, or every non-empty line of
   the file when it is not a zip — with no codec in the path. `runRoundTrip`
   asserts frames-read equals that number before comparing anything. Counting
   all members also catches loss the skip counter cannot see: `initScanner`
   (`pkg/codec/echoreplay.go:137-166`) opens exactly one member, so a second
   member is invisible to the reader entirely.

**Evidence the guard bites:** `TestCountSourceRecordsSeesWhatTheReaderDrops`
builds a 22-record capture whose last 2 lines are unparseable and asserts the
numbers diverge — 22 records present, 20 frames surfaced, 2 skipped. That is the
exact condition that used to pass silently.

On the committed sample the guard confirms 1023 of 1023 records surfaced.

**Still required for the corpus run:** the third number. Records-in equals
frames-read is now checked; frames-read equals lines-written is not, so a
reconstruction that emits fewer lines than it consumed would still be caught
only by the existing `len(origFrames) != len(reconFrames)` check, which compares
parsed output rather than written lines.

---

## FIXED — CLOCK-001: `game_clock_display` never round-tripped on any frame

**Status: FIXED** in `ab0f894`.

**What:** `game_clock_display` was reconstructed from the last
`ScoreboardUpdated` event (`reconstruct.go`, via `Session.ScoreAt`). That sensor
only fires when one of the four score integers changes
(`pkg/events/sensor_scoreboard.go:46-49` — it does not look at the clock at
all), so the display string was stale between goals and empty before the first
one. Measured: **1023 of 1023** frames wrong on the committed sample
(`orig="11:02.80" recon=""`), **12,817 of 12,817** on a January 2026 capture.

**Root cause:** a per-frame-varying value stored as an event sample, which
`docs/format-design.md` §2 explicitly rules out.

**Fix:** derive it. `game_clock_display` is a pure formatting of the per-frame
`game_clock`, which already round-trips to 5e-6:

    centis  = trunc(game_clock * 100)      // engine truncates, does not round
    display = "%02d:%02d.%02d" of centis/6000, (centis%6000)/100, centis%100

Same reasoning as `possession[]` in `format-design.md` §3 — redundant, derivable,
so derive rather than store.

**Evidence:** exact on **13,840 real frames** across two recordings (1023/1023
and 12,817/12,817), computed from the float32 the tape actually stores, so the
float64→float32 narrowing is already accounted for. A variant using an epsilon
before truncation was measured worse (11 mismatches) and rejected.

`game_clock_display` moved from a reported finding to an asserted exact field in
the BAC. `TestGoldenConvert` is unchanged — only the reverse path moved.

**Residual risk:** truncation is unstable within ~5e-6 of a centisecond
boundary, which is the measured round-trip error on `game_clock`. One frame of
the 13,840 fell within 0.001cs of a boundary and still resolved correctly. The
full-corpus run is what would surface a real collision.

---

## FIXED — READLOSS-002: unparseable input lines were silently discarded

**Status: FIXED** in `56746bb`.

**What:** `EchoReplay.ReadFrame` counts a line it cannot parse and continues
(`pkg/codec/echoreplay.go:504-508`). `SkippedFrames()` exposed the count, but
`grep -rn SkippedFrames --include='*.go'` found consumers only in
`boolean_int32_test.go` — nothing in the conversion pipeline or the CLI. A
capture whose lines were all rejected converted to an empty tape and reported
success.

**Impact:** GH #31 reports real files in exactly this shape (450 lines read, 450
rejected; 6476/6476; 696/696). Converting those would have produced empty tapes
with a clean exit.

**Fix:** `ConvertResult.SkippedLines`, populated via a consumer-side
`lineSkipper` interface (only the line-oriented echoreplay reader has the
notion; the length-delimited legacy reader errors out instead). `tapedeck
convert` prints the total and names each lossy input.

Measured on a fixture of 20 good lines plus 2 garbage lines:

    converted: 1, skipped: 0, failed: 0
    total frames: 20, total events: 9
    unparsed lines: 2 — output is NOT a complete record of its source

    unparsed input:
      mixed.echoreplay: 2 line(s) unparsed, 20 frame(s) kept

Tests: `TestConvertReportsSkippedLines` (`pkg/conversion/skipped_lines_test.go`),
table-driven over partial loss, near-total loss (the GH #31 shape), and no loss.

---

## FIXED — STATS-001: `player.stats` / `team.stats` are NOT derivable from events

**Severity:** medium (fidelity). Needs a proto addition, not plumbing.

**What:** It is tempting to reconstruct `TeamMember.stats` / `Team.stats` by
replaying the `Player*` events, which carry running totals
(`PlayerGoal.total_goals`, `PlayerStun.total_stuns`, …). An accumulator for
exactly this already exists — `cmd/tapedeck/stats.go:118-170` handles
PlayerGoal/Assist/Save/Steal/Stun/Block/ShotTaken/Interception, DiscThrown,
DiscCaught, and derives possession time from `disc_holder_slot` deltas. Lifting
it into `Session.replay()` looks like the obvious fix.

**It would fabricate data.** Measured on `testdata/sample.echoreplay`:

| | engine `stats` (frame 0 → last) | derived from events |
|---|---|---|
| Milkyway `stuns` | 0 → 0 | **3** |
| Milkyway `catches` | 0 → 0 | **5** |
| Milkyway `possession_time` | **13.22** → 63.87 | 48.2 |
| sprockee `possession_time` | **7.73** → 23.00 | 15.3 |

Two independent problems:

1. The detector's notion of an event is not the engine's. The engine counts zero
   stuns and zero catches across the whole capture; the sensors emit 3 and 5.
2. The engine's counters carry a **pre-capture baseline** — `possession_time` is
   already 13.22s on the first frame, because the recording starts mid-match.
   Any accumulation from frame 0 is wrong by construction, no matter how good
   the sensors get.

**Impact:** reconstructing stats this way would turn a currently-honest
"stats dropped" finding into a populated-but-wrong field that passes a presence
check. Same failure mode as reconstructing `last_throw` from `DiscThrown` when
the source field is local-player-only.

**Status: FIXED** in nevr-proto `eb3c3fb` + tape `b4f0d94`, along the fix
direction this entry called for. Measured placement rather than guessed: the
eleven integer counters move 0.6 times per 100 frames and ride a new
`PlayerStatsUpdated` event carrying the ENGINE's values; `possession_time` moves
29.4 times per 100 frames and rides `PlayerState` per-frame.

`TeamStats` needed no field — measured derivable by summing the team's players,
exact on 7470 team-frames for all eleven counters and within 7e-5 for
possession_time.

The existing `cmd/tapedeck/stats.go` accumulator was NOT wired into
reconstruction, as this entry warned.

---

## FIXED — INDEX-001: `LoadoutChanged`/`GrabChanged` cannot appear in the footer event index

**Severity:** low (index completeness). Not a fidelity bug — the events
themselves round-trip correctly.

**What:** `telemetry.v2.EchoEvent` defines **26** oneof variants;
`telemetry.v2.EventType` defines **24** values, with none for `LoadoutChanged`
or `GrabChanged`. `classifyEvent` therefore returns `EVENT_TYPE_UNSPECIFIED` for
both, and `Writer.WriteFrame` (`pkg/codec/tape.go:96`) skips UNSPECIFIED rather
than indexing it. The events are written into frames and replayed correctly by
`Session.replay()`, but a consumer scanning `CaptureFooter.event_index` for
loadout or grab changes finds nothing.

**Where / evidence:**
- Enum: `nevr-proto/telemetry/v2/capture.proto:130-167` — 24 values.
- Oneof: `nevr-proto/telemetry/v2/echo_arena.proto:248-288` — 26 variants.
- Skip: `pkg/codec/tape.go:96` — `if eventType != EVENT_TYPE_UNSPECIFIED`.
- Pinned by `TestClassifyEventCoversEveryEchoEventVariant` and
  `TestClassifyEventGapIsStillReal` (`pkg/codec/classify_event_test.go`), which
  walk the oneof from the descriptor. A new variant added to the proto without a
  `classifyEvent` case fails there instead of silently vanishing.

**Why it exists:** these are the only two v2-native events — they have no v1
source and are synthesized by `appendLoadoutGrabEvents`
(`pkg/conversion/mapping.go:279-305`), so they were never part of the v1→v2
event-type mapping.

**Status: FIXED.** `EVENT_TYPE_LOADOUT_CHANGED`, `EVENT_TYPE_GRAB_CHANGED` and
`EVENT_TYPE_PLAYER_STATS_UPDATED` added in nevr-proto `eb3c3fb`; `classifyEvent`
wired in tape `b4f0d94`. `eventTypeGap` is now empty and the count guard asserts
27 variants / 27 mappable, so any future variant added without a case fails
there.

---

## OPEN — FIDELITY-001: v2 is a lossy projection of v1; echoreplay does not round-trip through v2

**Severity:** design-level. Blocks treating v2 as the archival/anticheat format.

**What:** `tapedeck convert` reads echoreplay/nevrcap into a v1
`LobbySessionStateFrame` (which embeds the *full* `SessionResponse`), then
`MapFrame` (`pkg/conversion/mapping.go:235`) maps v1 → v2 `EchoArenaFrame` and
writes a v2 tape. The v2 schema (`EchoArenaFrame`/`PlayerState`) has **no slot**
for: weapon, ordnance, tac_mod, packet_loss, grab state
(`left/right_holding_onto`), the `possession` array, payload data, shoulder
inputs, team-level container. Those fields are silently dropped. There is **no
v2→echoreplay or v2→v1 path**, so the loss is irreversible.

**Evidence:** struct dumps of `EchoArenaFrame`/`PlayerState` (verified
2026-06-29); audit tests in `pkg/conversion/mapping_audit_test.go` assert the
drops; `SCHEMA-GAPS.md` lists every field with proto numbers. echoreplay ↔
echoreplay (v1 codec) IS lossless — proven by `TestEchoReplayRoundTripFidelity`
(1023 frames, 0 diffs). The loss is purely the v1→v2 step.

**Root failure:** no ADR justified the v2 scope or the deletion of the v1 writer
(`1e54c6e`); no BAC/test ever guarded echoreplay→tape→echoreplay fidelity (only
a 1-frame smoke test, `TestEchoReplayCodec`); the loss is silent (no flag/warn).

**AMENDMENT (2026-07-27) — the field list above is stale.** Measured against
`nevr-proto/telemetry/v2/echo_arena.proto` rather than the 2026-06-29 struct
dumps. Of the nine items listed as having "no slot", **eight now have one**:

| Listed as dropped | Actual v2 home |
|---|---|
| `weapon`, `ordnance`, `tac_mod` | `LoadoutChanged` (`echo_arena.proto:362-367`) |
| `packet_loss` | `PlayerState.packet_loss_ratio` (`:218`) |
| grab state | `GrabChanged` (`:372-376`) |
| `possession` array | derived — `reconstructPossession` (`reconstruct.go:381`) |
| payload data | `PayloadState` (`:182-188`) |
| shoulder inputs | `EchoArenaFrame:14-17`, all four incl. `_2` |
| team-level container | **still true** — no `team_name`, no `TeamStats` |

"There is **no** v2→echoreplay or v2→v1 path, so the loss is irreversible" is
also stale: `ReconstructFile` (`pkg/conversion/reconstruct.go:478`) and
`SessionReconstructor` exist and are exercised by `TestRoundTripBAC`.

The superset work in the DIRECTIVE below landed the proto side; what remains is
read-side plumbing, tracked as RECONSTRUCT-001, not schema loss.

---

## FIXED — RECONSTRUCT-001: `client_name` and payload state written to v2, never read back

**Status: FIXED** in `e4a6cb4`.

**What:** Six fields with v2 homes were populated by the forward mapper and
silently absent from reconstruction, so `echoreplay → v2 → echoreplay` dropped
them:

- `client_name` — `EchoArenaHeader:4`, written `pkg/conversion/mapping.go:153`
- `payload_multiplier` / `_checkpoint` / `_distance` / `_defenders` / `_speed` —
  `EchoArenaFrame:13` via `PayloadState`, written `mapping.go:394-400`

`grep -c` in `reconstruct.go` returned 0 for both `ClientName` and `Payload`.

**Why it survived:** the payload block at `mapping.go:391-393` only emits
`PayloadState` when some payload value is non-zero, and every committed fixture
is an arena match where all five are 0 — no test ever exercised the path.
`client_name` had no assertion in the BAC at all.

**Evidence (RED before the fix):** `client_name = "", want "Milkyway"`;
`payload_multiplier = 0, want 1.5` (and the other four at 0).

**Fix:** `reconstructSession` now assigns `ClientName` from the header and the
five payload fields from `ea.GetPayload()`. Both are asserted by the acceptance
gate (`compareSession`) so they cannot regress.

Tests: `TestReconstructPreservesClientName`, `TestReconstructPreservesPayload`
(`pkg/conversion/reconstruct_gaps_test.go`) — the latter builds a synthetic Echo
Combat session, since no arena capture can exercise payload.

**Remaining read-side gaps (not fixed here):** `last_throw` / `last_score` are
absent at *both* ends (`mapFrame` never reads them off the session and
`reconstruct.go` never writes them). `Team.stats` / `TeamMember.stats` are never
reconstructed and — measured — **cannot be**, see STATS-001; they need a proto
home rather than an accumulator. `Session.replay()` handling 6 of 26 `EchoEvent`
variants is a real limitation for other consumers, but it is not what blocks
stats.

---

## DIRECTIVE — Andrew, 2026-06-29 — make v2 the complete, tested, superset format

Every item below needs a **test** and a **BAC** it traces to. No feature ships
untested or un-BAC'd. Work this under the project's orientation; orient first.

1. **v2 must round-trip ANY echoreplay file, losslessly.** Extend the v2 schema
   (in `nevr-proto` / `buf.build/echotools/nevr-api`) to be a true **superset**
   of `SessionResponse` — every dropped field gets a v2 home. BAC: for any
   echoreplay, `echoreplay → v2 → echoreplay` is byte/field identical.
2. **Deprecate v1. No code may depend on it.** Remove v1 as a runtime
   dependency of the conversion/codec paths once v2 is a superset.
3. **v1 → v2 importer.** A one-way importer so existing v1/nevrcap captures
   migrate into v2 without loss (v2 being a superset makes this lossless).
4. **Tests for every feature.** If a feature lacks a test, it gets one.
5. **Every feature tied to a BAC.**

**Progress (2026-06-29):**
- Full design + fidelity reference written: `docs/format-design.md`. Read it first.
- Characterized exactly what v2 drops on a real recording (`TestFieldLossAudit`):
  kinematics 100% kept; combat fields 0% in arena; real loss = grab / shoulder /
  packet-loss. Identity is 100% recoverable from events (`TestRosterRebuildAudit`).
- **`possession[]` resolved: do NOT add** — proven redundant with
  `has_possession`/`disc_holder_slot` (`TestPossessionProbe`).
- **Proto superset** on `nevr-proto` main (`da61c1c`), placed by variability:
  per-frame `packet_loss_ratio` on `PlayerState`, capture-client shoulder input
  on `EchoArenaFrame` (alongside `vr_root`); `LoadoutChanged`/`GrabChanged`
  events; combat `PayloadState` sub-message; `session_ip` in header. Published to
  BSR (`c89ff774a767`); tape pinned to `v1.36.11-20260629074123-c89ff774a767.1`.
- **Mechanical fields WIRED + tested** (`MapFrame`, tape `f9696e5`): shoulder,
  packet-loss, payload, session_ip copy exactly from v1 — proven by
  `TestSupersetFieldsPopulate` (756 shoulder + 72 packet-loss frames on the
  sample; 6992 + 17177 on alienq). Golden regenerated; deterministic ×10.
- **`LoadoutChanged`/`GrabChanged` WIRED + tested** (tape `0fa72dc`): delta
  events, replay rebuilds per-frame loadout/grab exactly (`TestLoadoutGrabReconstruct`).
- **Session layer (#34) + v2→echoreplay reconstructor + round-trip BAC DONE**
  (tape `d6afe7c`): `session.go` (`RosterAt`/`LoadoutAt`/`GrabAt`/`ScoreAt`),
  `reconstruct.go`, `TestRoundTripBAC`. echoreplay→v2→echoreplay round-trips the
  **recoverable lane EXACT on every non-spatial field, within float32 tolerance
  on spatial** (sample: 0 mismatches, max mag 5e-6, max orient 2.35e-3). This is
  the cheat-investigation lane (kinematics + identity + loadout + grab + disc +
  scores) — faithful.

**Still OPEN — to make v2 a TRUE lossless superset (the BAC names these as
not-round-tripping; Andrew's call on which matter):**
- `rules_changed_by` / `rules_changed_at` — no v2 home.
- per-frame `last_throw` and `last_score` (incl. scorer/assist **names** — v2
  `GoalScored` is slot-only) — v2 only carries throws/goals as events.
- ~~per-frame `game_clock_display`~~ — **RESOLVED 2026-07-27 (CLOCK-001 below):**
  derived from `game_clock` at reconstruction instead of event-sampled. Exact on
  13,840 real frames.
- per-frame `blue/orange_round_score` — only event-sampled (`ScoreboardUpdated`);
  **BUG: the score sensor seeds frame 0 silently (no event)**
  (`pkg/events/sensor_scoreboard.go:36-43` records state and returns nil), so any
  pre-first-change value is unrecoverable. Fix: emit a seed. STILL OPEN.
- `team_name` (string) — roster stores `Role` enum only.
- **per-frame `jersey_number`/`level`** — alienq proves they VARY per-frame
  (slot 10: 1/50 for 18865 frames, 0/0 for 3); v2 stores only a join snapshot.
  Refutes `SCHEMA-GAPS` BUG-7's constant assumption.
- `pause` sub-state (`paused_state` narrowing, `unpaused_team`, timers).
- per-frame `team.stats` / `player.stats`; empty-team structural case.
- still: fix GH #18 (silent event loss); then v1 deprecation + v1→v2 importer.

---

## DONE — FIDELITY-BASELINE: echoreplay round-trip fidelity test (the absolute baseline)

`pkg/conversion/roundtrip_baseline_test.go` — `TestEchoReplayRoundTripFidelity`.
Reads the real sample, writes it back, reads again, asserts every
`SessionResponse` field identical across all frames. Proves the v1/echoreplay
lane is lossless. PASS: 1023 frames, 0 mismatches. This is the baseline BAC the
v2-superset work must also satisfy.

---

## FIXED — SEC-001: decompression bomb — no decode-size / frame-count cap → OOM DoS from a tiny hostile capture

**Severity:** HIGH (DoS). Easiest crash to trigger from a crafted file.
**Status: FIXED** in `545f3b6` (branch `fix/sec-decompression-guards`).
All capture readers (`Reader`, `LegacyReader`, `EchoReplay`) now enforce
documented, configurable budgets (`pkg/codec/limits.go`): total decoded bytes
(default 8 GiB) and frame count (default 10M), overridable/disableable per
reader via `WithMaxDecodedBytes` / `WithMaxFrameCount` / `WithoutLimits`.
`zstd.WithDecoderMaxMemory` is set from the budget on both decoder open sites;
accumulation sites (`OpenSession`, `NewSessionReconstructor`, `ReadFrames`)
enforce the reader's frame budget again. Past budget: clear error wrapping
`ErrMaxDecodedBytes` / `ErrMaxFrameCount`.
Tests: `TestReader_SEC001_FrameCountBudget`, `TestReader_SEC001_DecodedBytesBudget`,
`TestReader_SEC001_DefaultLimitsApplied`, `TestReader_SEC001_WithoutLimitsOptOut`,
`TestLegacyReader_SEC001_FrameCountBudget`, `TestLegacyReader_SEC001_DecodedBytesBudget`,
`TestEchoReplay_SEC001_FrameCountBudget`, `TestEchoReplay_SEC001_DecodedBytesBudget`
(`pkg/codec/sec001_bomb_test.go`); `TestOpenSession_SEC001_FrameBudget`,
`TestNewSessionReconstructor_SEC001_FrameBudget`,
`TestOpenSession_SEC001_DecodedBytesBudget` (`pkg/conversion/sec001_budget_test.go`).
Fuzz: `FuzzReadEnvelope`, `FuzzReadDelimitedMessage` (`e547216`).

**What:** Both tape decoders wrap the input in a Zstd stream reader with **no**
memory or decoded-size guard, and the streaming APIs that most callers use
accumulate every frame into an unbounded slice. A few-KB, highly-compressible
`.tape`/`.nevrcap` (a repeated tiny valid envelope compresses ~1000:1) or a
high-ratio `.echoreplay` zip member decompresses to gigabytes of frames, and the
consumer heaps them until the process is OOM-killed. The only size guard present
(`MaxMessageSize`, 256 MB) bounds a **single** message; it does not bound the
**total** decompressed output or the **number** of frames.

**Where / evidence:**
- Decoders opened with no limit options:
  - `pkg/codec/tape.go:264` — `decoder, err := zstd.NewReader(file)`
  - `pkg/codec/legacy.go:70` — `decoder, err := zstd.NewReader(src)`
  (no `zstd.WithDecoderMaxMemory` / `WithDecoderMaxWindow` / decoded-size cap.)
- Unbounded frame accumulation on the read side:
  - `pkg/conversion/session.go:81` — `OpenSession`: `for { frame, err := r.ReadFrame(); ...; frames = append(frames, frame) }` (no count/size cap).
  - `pkg/conversion/reconstruct.go:159` — `NewSessionReconstructor`: identical unbounded `frames = append(...)` loop, then `NewSession` allocates 4 per-frame slices sized `len(frames)` (`session.go:63-66`).
  - `pkg/codec/echoreplay.go:589` — `EchoReplay.ReadFrames`: `frames = append(frames, frame)` until EOF.
- No test guards it: `grep -rniE 'bomb|maxdecoded|WithDecoderMax|frame.?limit'` over `pkg`/`cmd` returns nothing.

**Malformed-input scenario:** craft a `.tape` whose uncompressed stream is one
`CaptureHeader` envelope followed by millions of copies of a minimal valid
`Frame` envelope (a few bytes each). Zstd crushes the repetition to a few KB on
disk. Any downstream (nevr-anticheat / nevr-agent / `tapedeck`) that calls
`OpenSession`, `NewSessionReconstructor`, or `ReadFrames` on it allocates
gigabytes of `*Frame` and is OOM-killed. Even purely-streaming consumers pay a
CPU-DoS decoding GB from KB. Same shape via an `.echoreplay` zip whose inner
member is millions of identical session lines.

**Fix direction:** set `zstd.WithDecoderMaxMemory` and cap total decoded bytes
on the codec reader; enforce a max frame count / max total-bytes budget in the
reader (and in `OpenSession`/`NewSessionReconstructor`/`ReadFrames`), returning
an error past the budget. Budget should be a documented, non-hardcoded limit.

---

## FIXED — SEC-002: 256 MB single allocation from a ~5-byte length prefix (allocate-before-verify)

**Severity:** MEDIUM (memory-spike DoS; amplifies SEC-001).
**Status: FIXED** in `aa9f215` (branch `fix/sec-decompression-guards`).
Both readers now use shared `readMessageBody` (`pkg/codec/limits.go`): at most
1 MiB (`maxEagerMessageAlloc`, cited against measured sample message sizes) is
allocated before any bytes arrive; larger messages grow the buffer only as
bytes are actually read, so a truncated giant length prefix costs a bounded
allocation plus a clean error. Measured: 268,445,480 bytes allocated before the
fix from a 5-byte stream; under the 64 MiB test threshold after.
Tests: `TestReader_SEC002_TruncatedGiantLengthPrefix`,
`TestLegacyReader_SEC002_TruncatedGiantLengthPrefix`
(`pkg/codec/sec002_alloc_test.go`).

**What:** The length-delimited readers allocate the full declared message buffer
**before** confirming that many bytes are actually available in the stream. The
declared length is capped at `MaxMessageSize` (256 MB), but nothing requires the
stream to contain that much data — a handful of decompressed bytes encoding a
large varint length forces an immediate 256 MB allocation, after which
`io.ReadFull` fails on the truncated stream. Chained with SEC-001 an attacker can
drive repeated 256 MB allocations from a tiny file.

**Where / evidence:**
- `pkg/codec/tape.go:355-361`:
  ```go
  if length > MaxMessageSize { return nil, fmt.Errorf(...) }
  data := make([]byte, length)          // up to 256 MB, before any read
  if _, err := io.ReadFull(r.reader, data); err != nil { return nil, err }
  ```
- `pkg/codec/legacy.go:155-161`: same pattern (`data := make([]byte, length)` then `io.ReadFull`).

**Malformed-input scenario:** a `.tape` whose decompressed head is a varint
encoding `0x0FFFFFFF` (~256 MB) followed by EOF. `readEnvelope` allocates 256 MB,
then errors. Per read it is one large transient allocation; combined with SEC-001
(a compressible stream of many such max-length prefixes) it becomes sustained
memory pressure / OOM.

**Fix direction:** cap the eager allocation (e.g. allocate `min(length, sane
cap)` and grow, or bound `length` against remaining input) so buffer size is
tied to bytes actually present, not to an attacker's claimed length.
