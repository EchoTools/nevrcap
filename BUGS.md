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

## OPEN — FIDELITY-002: fields the reflective differ found losing, that the hand-written comparison never looked at (AWAITING RULING)

**Severity:** unresolved fidelity loss. `TestRoundTripBAC` is RED on the
committed sample until Andrew rules on each path below. Do NOT allowlist them to
get green.

**What:** the round-trip comparison was a hand-written enumeration. Replacing it
with a descriptor-driven differ (`pkg/fidelity`, per Andrew's 2026-07-25 ruling
"i want everything compared … errors on anything not in the round trip or any
fields/differences in the round trip source and target") raised SessionResponse
coverage from a partial hand list to **138/138 schema fields, proven reached**.
The fields below were never compared before and do not round-trip. Each needs a
ruling: fix the loss, or record it in `KnownUnpreserved` with a citation.

| path | observed |
| ---- | -------- |
| `SessionResponse.client_name` | sample: orig=`Milkyway` recon=`""`, all 1023 frames. Empty in the chi1 recordings, so it only fails where the file has one. |
| `SessionResponse.pause.unpaused_team` | sample: orig=`none` recon=`""` ×1023. chi1: orig=`blue` ×5272. |
| `SessionResponse.pause.paused_requested_team` | sample: orig=`none` recon=`""` ×1023. chi1: orig=`blue` ×5272. |
| `SessionResponse.pause.unpaused_timer` | chi1: orig=`-0.0033778567` recon=`0` ×5272. |
| `SessionResponse.pause.paused_timer` | chi1: orig=`128.05461` recon=`0` ×5272. |

### 2026-07-26 — the allowlist was narrowed to EXACT PATHS; these 24 are the new red

Andrew's ruling is errors on **any** difference. The allowlist absorbed whole
subtrees, so an entry meaning "`last_throw` has no v2 home" ALSO excused a
reconstruction that returned a *different value* for `last_throw.arm_speed` — a
field present on both sides and simply wrong. Measured: **52 of 138**
SessionResponse paths (37.7%) could not fail at all. Reproduced before the fix:
`last_throw.arm_speed` allowed=true with orig=12.5 recon=999, and
`teams[].players[].stats.stuns` allowed=true with orig=7 recon=4242.

An allowlist entry now covers exactly the path it names (plus that path's
`#presence`/`#count` forms — the same field failing differently) and nothing
beneath it. That un-excused **132 path forms / 44 fields**; on the committed
sample **24 of them actually differ** and are now RED. Each needs its own
ruling — fix the loss, or record the sub-field with a citation. **They were NOT
allowlisted to restore green.**

| newly-failing path (committed sample, ×1023 frames unless noted) | observed |
| ---- | -------- |
| `SessionResponse.last_throw.arm_speed` | orig=`0.39658257` recon=`0` |
| `SessionResponse.last_throw.total_speed` | orig=`0.56624627` recon=`0` |
| `SessionResponse.last_throw.off_axis_spin_deg` | orig=`49.16243` recon=`0` |
| `SessionResponse.last_throw.wrist_throw_penalty` | orig=`0.026895806` recon=`0` |
| `SessionResponse.last_throw.rot_per_sec` | orig=`0.24592718` recon=`0` |
| `SessionResponse.last_throw.pot_speed_from_rot` | orig=`0.18542472` recon=`0` |
| `SessionResponse.last_throw.speed_from_arm` | orig=`0.39658257` recon=`0` |
| `SessionResponse.last_throw.speed_from_movement` | orig=`0.086443841` recon=`0` |
| `SessionResponse.last_throw.speed_from_wrist` | orig=`0.083219856` recon=`0` |
| `SessionResponse.last_throw.wrist_align_to_throw_deg` | orig=`39.597031` recon=`0` |
| `SessionResponse.last_throw.throw_align_to_movement_deg` | orig=`51.407391` recon=`0` |
| `SessionResponse.last_throw.off_axis_penalty` | orig=`0.062036529` recon=`0` |
| `SessionResponse.last_throw.throw_move_penalty` | orig=`0.18051183` recon=`0` |
| `SessionResponse.last_score.disc_speed` | orig=`12.853542` recon=`0` |
| `SessionResponse.last_score.team` | orig=`orange` recon=`""` |
| `SessionResponse.last_score.goal_type` | orig=`LONG SHOT` recon=`""` |
| `SessionResponse.last_score.point_amount` | orig=`3` recon=`0` |
| `SessionResponse.last_score.distance_thrown` | orig=`9.2512016` recon=`0` |
| `SessionResponse.last_score.person_scored` | orig=`[INVALID]` recon=`""` |
| `SessionResponse.last_score.assist_scored` | orig=`[INVALID]` recon=`""` |
| `SessionResponse.teams[].stats.possession_time` | orig=`13.224689` recon=`0` ×2046 |
| `SessionResponse.teams[].stats.steals` | orig=`1` recon=`0` ×1655 |
| `SessionResponse.teams[].players[].stats.possession_time` | orig=`13.224689` recon=`0` ×2046 |
| `SessionResponse.teams[].players[].stats.steals` | orig=`1` recon=`0` ×1655 |

The remaining 20 of the 44 un-excused fields (the other `stats` sub-fields) are
zero in this recording on BOTH sides, so they do not differ here. They are
un-excused all the same: they now fail the day a recording carries a value.
`TestKnownUnpreservedExcusesNothingBeneathItself` prints the full list and fails
if any path is excused only because an ancestor is allowlisted.

### 2026-07-26 — losses on the 603 MB / 102,892-frame chi1 recording, in no bug doc until now (AWAITING RULING)

Found by the exhaustive differ on the largest audited recording. NOT
allowlisted; each needs Andrew's ruling.

| path | observed |
| ---- | -------- |
| `SessionResponse.total_round_count` | orig=`3` recon=`0` ×102,763 |
| `PlayerBonesResponse#presence` | whole bones payload lost on ×132 frames |
| `SessionResponse.teams[].players[].level` | ×689,770 |
| `SessionResponse.teams[].players[].jersey_number` | ×384,791 |

`level` / `jersey_number` are the DIRECTIVE's "per-frame jersey_number/level —
v2 stores only a join snapshot" item, now measured at scale. `total_round_count`
and the dropped bones payloads are new.

`PauseState` was previously compared on `paused_state` alone — 1 of its 5 fields.
The DIRECTIVE already lists "pause sub-state (`paused_state` narrowing,
`unpaused_team`, timers)" as still-open, so these are the same hole, now
measured; they are NOT allowlisted because no one has ruled that they may be
lost.

Held failing deliberately (Andrew, same ruling): `blue_round_score`,
`orange_round_score`, `possession`. Pinned by
`TestKnownUnpreservedDoesNotCoverTheHeldFields`.

~~Also newly compared and now covered by EXISTING allowlist entries as
subtrees: `last_throw.*` (13 sub-fields), `last_score.*` (7), `teams[].stats.*`
(12), `teams[].players[].stats.*` (12).~~ **Superseded 2026-07-26:** subtree
coverage was the defect. Those 44 fields are no longer excused — see the
narrowing section above.

---

## OPEN — PERF-001: `ReconstructFile` accumulates every frame; 11.9 GB RSS on a 603 MB recording, OOM on 16 GB

**Severity:** blocks verifying the largest recordings on ordinary hardware —
which is exactly the archival decision the verdict exists to inform.

**Measured (2026-07-26):** the 603 MB / 102,892-frame chi1 recording took
**7m15s at 11.9 GB peak RSS** through `VerifyEchoReplayRoundTrip`. The
comparison itself streams (one frame per side at a time); the cost is
`ReconstructFile` / `NewSessionReconstructor` accumulating every frame in memory
(`pkg/conversion/reconstruct.go:159`, `session.go:63-66` — the same unbounded
accumulation SEC-001 capped for hostile input but did not make streaming). The
largest chi1 file is **741 MB**, extrapolating to ~14–15 GB, which OOMs a 16 GB
machine.

**Not fixed — recorded only.** The fix is to make reconstruction streaming
rather than materializing, which is a real design change to `session.go`'s
random-access API (`RosterAt`/`LoadoutAt`/`GrabAt`/`ScoreAt` index by frame).

---

## FIXED — VERDICT-001: five ways to obtain a passing verdict on a file that lost data

**Status: FIXED** 2026-07-26 on `feat/obvious-batch-2.1.0`. An independent
adversarial review of `171c5e1` found five. A verdict authorizes deleting an
irreplaceable recording, so each one was a way to delete data on a receipt for
work nobody did.

1. **The receipt lied.** With `VerifyOptions.SkipKeyScan`, a file with 1023
   discarded JSON keys returned FIDELITY PASS while the receipt printed
   `keyscan=0/0 frames (exhaustive) unknown-keys=0`. `SkipKeyScan` is **deleted**
   — the scan is unconditional. `KeyScanResult.Ran` renders as
   `keyscan=NOT RUN — this verdict does not certify key completeness` and is
   fatal, so no hand-assembled verdict can claim the scan either.
   (`TestKeyScanSummaryNeverClaimsWorkItDidNotDo`, `TestVerifyHasNoSkipKnob`)
2. **The zero value read as success.** `fidelity.Verdict{}` printed FIDELITY
   PASS with `Pass()==true`. A verdict now has to be affirmatively completed
   (`MarkComplete`, called last, after every lane); until then it fails closed.
   (`TestZeroVerdictFailsClosed`)
3. **Two loss vectors had no lane.** A 4th tab-separated payload on a frame line
   (`parseFrameLine` reads `parts[0..2]`) and a second zip member
   (`ReplayMember` picks one) were both silently discarded and both passed.
   Each is now fatal and named. Corpus shape ASSERTED, not assumed: the
   committed sample is 1 zip member and 1023 lines of exactly 3 fields; all 458
   chi1 files measured 1 member / 3 fields.
   (`TestVerifyFailsOnExtraTabField`, `TestVerifyFailsOnSecondZipMember`,
   `TestCorpusShapeIsOneMemberThreeFields`)
4. **The coverage denominator was guarded for one root of three.** Deleting
   `PlayerBonesResponse.user_bones[].bone_o[]` — recorded skeleton ORIENTATION —
   from the plan reported "reached 4/4 schema fields" and kept the package green.
   The guard now runs over every compared root; repeating the deletion fails with
   the missing path named. (`TestSchemaPathsMatchDescriptors/PlayerBonesResponse`)
5. **52 of 138 paths could not fail.** See the FIDELITY-002 narrowing section.

**Made load-bearing:** `VerifyEchoReplayRoundTrip` was called only from
`_test.go`. `tapedeck verify` now runs it, prints the receipt, and exits
non-zero on FAIL; its old hand-written event checks — which printed
`VERDICT: PASS` for the sample — are a clearly subordinate section over derived
data. `tapedeck convert` is deliberately unchanged (refusing to emit unverified
tapes is sequenced separately).

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
- per-frame `game_clock_display` + `blue/orange_round_score` — only event-sampled
  (`ScoreboardUpdated`); **BUG: the score sensor seeds frame 0 silently (no
  event)**, so any pre-first-change value is unrecoverable. Fix: emit a seed.
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
