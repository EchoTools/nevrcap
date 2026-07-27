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

## OPEN — INDEX-001: `LoadoutChanged`/`GrabChanged` cannot appear in the footer event index

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

**Fix direction:** add `EVENT_TYPE_LOADOUT_CHANGED` and
`EVENT_TYPE_GRAB_CHANGED` to `EventType` in `nevr-proto`, publish to BSR, then
add the two `classifyEvent` cases and remove them from `eventTypeGap`. The tests
above will tell you when the enum lands. Not fixable in tape alone.

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
also stale: `ReconstructFile` (`pkg/conversion/reconstruct.go:441`) and
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
`reconstruct.go` never writes them); `Team.stats` / `TeamMember.stats` are never
reconstructed; `Session.replay()` handles 6 of 26 `EchoEvent` variants, which is
why the stat totals have no accumulator to read from.

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
