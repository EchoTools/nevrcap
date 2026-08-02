# BUGS.md — tape

Bug + work ledger for `tape`. One entry per issue: what, where, evidence,
status. Check this before idling. (Convention: every repo gets a root BUGS.md.)

---

## FIXED — RELEASE-001: `feat/obvious-batch-2.1.0` carried a TEMP go.mod replace

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

**Status: FIXED.** The nevr-proto changes merged to `main` and the `buf` workflow
published them:

    Push to BSR: buf.build/echotools/nevr-api:fc62323fed494006ba66869c983ceeb0

tape then dropped the `replace` and pinned the published module, whose version
string carries that same BSR commit:

    buf.build/gen/go/echotools/nevr-api/protocolbuffers/go
        v1.36.11-20260629074123-c89ff774a767.1   (was)
        v1.36.11-20260729220401-fc62323fed49.1   (now)

`go.mod` carries **zero** `replace` directives, and `just` (fmt, vet,
golangci-lint 0 issues, `go test -race -count=1`) is green across all five
packages against that pin. The `main`-never-carries-the-replace invariant now
holds by construction rather than by discipline.

Note the nevr-proto history was rewritten with `git-filter-repo` in the same
session (455 ever-committed paths -> 38, `.git` 3.3M -> 296K, verified by fresh
clone), so every commit hash in this entry predating the publish refers to the
pre-rewrite history. The rewrite did not move the BSR digest: `buf push` on the
rewritten `main` produced the identical `fc62323fed494006ba66869c983ceeb0`,
because the schema content was unchanged and only commit hashes moved. tape's
pin is therefore stable across the rewrite.

---

## FIXED — RELEASE-002: the module path needed a `/vN` suffix; `/v3` is unusable

**Severity:** was a release-blocker. Resolved by moving to
`github.com/echotools/tape/v4`.

**What:** the published tags run to `v3.3.0` and `v3.3.0` is an ancestor of
`main`, so `v3.4.0` looked like the next tag. Go requires any major >= 2 to end
the module path in `/vN`, and the rename in `369b0e8` dropped the suffix, so a
v3 tag on the bare path is rejected outright.

**The part the first version of this entry got wrong.** It asserted "the next
tag cannot be `v3.4.0`" and recommended `v1.0.0`. Both were wrong, and the
reasoning under them ("the import path already changed, so downstreams must edit
their go.mod regardless") was false. `github.com/echotools/tape` has been the
module path since v1.2.0 and resolves today at `@latest = v1.2.1`. The tag list
was never read before recommending against it — `v1.0.0` and `v0.1.0` both
already exist.

**Measured — every path variant on proxy.golang.org:**

| path | HTTP | @latest | versions |
|---|---|---|---|
| `tape` | 200 | v1.2.1 | v0.1.0 v1.0.0 v1.1.0 v1.2.0 v1.2.1 |
| `tape/v2` | 404 | — | free |
| `tape/v3` | 200 | v3.3.0 | v3.0.0 v3.1.0 v3.2.0 v3.3.0 — **all four fail** |
| `tape/v4` | 404 | — | free |
| `tape/v5` | 404 | — | free |

`tape/v3` is registered and entirely broken. Each version's `go.mod` declares a
different path (`nevrcap`, `nevrcap/v3`, `nevr-capture/v3`):

    $ go get github.com/echotools/tape/v3@v3.3.0
    module declares its path as: github.com/echotools/nevr-capture/v3
            but was required as: github.com/echotools/tape/v3

**Why `/v3` could not be salvaged.** `tape/v3@v3.1.0` and `@v3.3.0` are in the
**checksum database** and have cached `.info`/`.zip` on the proxy — permanent and
unfixable. `@latest` for `tape/v3` is therefore pinned to a broken v3.3.0
forever. And `go` does **not** fall back past an invalid `@latest`; measured
against `github.com/echotools/nevrcap`, whose `@latest` (v1.2.1) is invalid while
v1.0.0/v1.1.0 are valid:

    $ go get github.com/echotools/nevrcap
    go: github.com/echotools/nevrcap@upgrade (v1.2.1) requires ...
        module declares its path as: github.com/echotools/tape
                but was required as: github.com/echotools/nevrcap

Hard failure, no fallback. So a bare `go get github.com/echotools/tape/v3` could
never work regardless of what else was tagged on it.

**Fix: `github.com/echotools/tape/v4`.** The path is virgin — proxy 404, and
`v4.0.0`/`v4.0.1`/`v4.1.0` all absent from the checksum database. It inherits no
broken versions, and the number tracks the project's real lineage (v1 `nevrcap`
-> v3 `nevr-capture` -> v4 `tape`) rather than claiming continuity with a module
path this was never published under.

**Status: FIXED.** `go.mod` declares `module github.com/echotools/tape/v4`; 28
files rewritten across 4 import lines; `README.md`, `CLAUDE.md` and the primer
updated. `just` green — fmt, vet, golangci-lint 0 issues, `go test -race
-count=1` across all five packages, now reported under `github.com/echotools/tape/v4/...`.

**Remaining, and it is Andrew's:** cutting the `v4.0.0` tag itself.

---

## FIXED — VOCAB-001: an unknown engine string is erased silently

**Severity:** medium (archival integrity). Not currently triggered by any capture
measured, but the failure mode was total and silent.

**What:** `gameStatusMap`, `matchTypeMap`, `pauseStateMap` and
`goalTypeStringMap` translate engine strings to enums; an unrecognized string
falls through to `*_UNSPECIFIED`, and the reverse map renders `UNSPECIFIED` as
`""`. So a value the tables do not know converts cleanly and is simply gone.
This is the fable-audit finding F-9 (the audit doc is removed; F-1..F-16
resolution is recorded under AUDIT-001 below).

**GOALTYPE-001 was an instance of exactly this** — six of eleven goal types were
missing and were being discarded on real captures. That one is fixed; the class
is not.

**Measured (this is the reassuring part):** across 25 dal1 captures plus the two
client captures, every `game_status`, `match_type` and `paused_state` value is
already mapped:

| field | values seen | unmapped |
|---|---|---|
| `game_status` | `pre_match`, `playing`, `round_start`, `score`, `""` | none |
| `match_type` | `Echo_Arena_Private` | none |
| `paused_state` | `unpaused`, `none`, `paused`, `unpausing` | none |

`game_status: ""` appears in 10 of 25 files and round-trips **correctly by
accident**: unmapped → `UNSPECIFIED` → `""`. It is indistinguishable from a real
unknown value, which is the whole problem.

Note the binary is not authoritative for `match_type`: echovr.exe carries
lowercase symbol names (`echo_arena_private` @`0x1416d8d07`), while the JSON uses
`Echo_Arena_Private`. The two vocabularies differ, so the mapping table cannot be
verified against the string table the way GOALTYPE-001 was.

**Fix:** `pkg/conversion/vocabulary.go`. A per-conversion `vocabulary`
accumulator owns the only sanctioned lookups into the four tables; each is a
plain map hit on the common case and does work only on a miss, so counting costs
nothing per frame. The result reaches `ConvertResult.UnmappedValues` (field,
value, count — sorted by field then value so a corpus receipt is diffable) and
`tapedeck convert` aggregates it across inputs.

Deliberately *not* "fail the conversion" — one unknown string must not abort a
174 GB pass — and deliberately not "guess", since the tables cannot be completed
from the binary.

The **empty string is deliberately not counted**. An absent engine field is not
an unknown vocabulary item: `game_status` is empty in 10 of 25 measured dal1
captures and round-trips correctly, so counting it would report a loss that is
not real on nearly half the corpus. This is the one case the counter cannot
distinguish, and it is called out here rather than papered over.

The exported `MapHeader`, `MapHeaderFromSession`, `MapFrame` and
`goalTypeStringToEnum` keep their signatures and pass a nil accumulator — they
have nowhere to report. Conversion goes through the unexported variants.

**Evidence:** the sample with `game_status` patched to an unknown value on 3 of
40 frames, through the real binary:

    $ tapedeck convert poisoned.echoreplay
    converted: 1, skipped: 0, failed: 0
    total frames: 40, total events: 13
    unmapped engine values: 1 distinct — these convert to UNSPECIFIED and are LOST

    unmapped engine values:
      game_status = "quantum_overtime" (3 occurrence(s))

**Status: FIXED.** Pinned by `TestVocabularyCountsWhatTheTablesLose`,
`TestVocabularyIgnoresTheEmptyString`, `TestVocabularyIsSilentOnAKnownCapture`
(no false positives on the committed sample) and `TestNilVocabularyRecordsNothing`
(`pkg/conversion/vocabulary_test.go`).

**Still Andrew's call:** whether `tapedeck convert` should also exit non-zero
when any unmapped value is seen. It currently exits 0 and reports, matching
`SkippedLines`.

---

## OPEN — CANONICAL-001: `last_throw` is not wired (§2 only; §1b and §3 are closed)

**Severity:** known limitation, not a fixable defect. `last_throw` is
**local-player-only** in the engine's `/session` endpoint — each client reports
only its own last throw, so a Spark recording (one client's perspective) has
throw data for exactly one player. Reconstructing it from `DiscThrown` events
would fabricate data for every other player. The field stays absent until the
local-player identity problem is solved upstream.

Everything else in CANONICAL-001 is FIXED: float spelling (§1), orientation
basis orthonormalization (§1b), bones payload representation (§3). The only
remaining field-level gap is `last_throw`, and it is by design, not omission.

**Status on the committed sample:** structure identical — 1023 records, no key
added, none dropped.

**How it is measured:** `pkg/conversion/canonical_test.go`. The comparison is
against the CANONICAL form of the source, normalising out recorder drift so only
v2 loss remains.

**Status on the committed sample:** structure identical — 1023 records, no key
added, none dropped.

### 1. Float spelling — FIXED

The engine formats floats with 8 significant digits, trailing zeros trimmed but
never leaving a bare integer, and writes exponents bare (`9.6339078e-5`,
`1.5345009e24`). protojson emits the shortest float64 round-trip and Go spells
exponents `e-05` / `e+24`. Identical values, different bytes.

`FixEngineFloatFormatting` (`pkg/codec/engine_float.go`) rewrites them. It is
key-directed, not lexical: protojson writes a zero double and a zero int32
identically (`0`) while the engine writes `0.0` and `0`, so the float-typed JSON
keys are derived from the proto descriptors at init — 39 keys, verified to have
**zero collisions** (no key is float-typed in one message and integer-typed in
another), so a key alone determines the spelling.

**Evidence:** all **490,017** float literals in the committed capture reproduce
the engine's spelling exactly (`TestAppendEngineFloatMatchesTheEngine`).

It **replaces** `FixExponentNotation` on the write path. That function expands
exponent form to decimal, which measurement showed is wrong — the engine *uses*
exponent form for small magnitudes, so expanding diverged from it. Only
float-typed fields can produce exponents, so the new fixer subsumes the old one.
`FixExponentNotation` is retained as exported API for external callers.

**Cost:** 19.5 µs/frame vs 6.6 µs (238 MB/s vs 706 MB/s), same single
allocation — 3x, on the reconstruction path only. ~12 min/core for 174 GB.

**Note on precision:** 8 significant digits does *not* round-trip every float32
(a denormal such as 1.02424515e-36 needs 9). That cannot reach us: every value
in a capture was written by the engine at 8 digits, so the precision was spent
upstream. `TestAppendEngineFloatIsIdempotent` pins the invariant that actually
governs — respelling an engine-written value reproduces it.

### 1b. Orientation basis — FIXED (not loss)

**What was measured:** the quaternion round-trip returns an orthonormalized
basis. For Spark captures, this produces a ~0.0003 median component difference
at the 4th-5th decimal place — invisible at the engine's reported 3-decimal
precision. For nevr-agent captures and the 20-60 Hz engine-memory streaming
case, the basis is already orthonormal and the round-trip is exact (0.00000
across 41,943 measured frames).

**Why this is not loss.** The `/session` HTTP endpoint serializes an internal
quaternion as 9 floats through JSON. Spark recorded the slightly non-orthonormal
decimals. v2 stores the quaternion directly — the engine's own internal
representation — and reconstruction orthonormalizes the basis back to what the
engine computed. This is a correction, not a loss.

When tape sources from engine memory directly (the 20-60 Hz streaming case),
the quaternion IS the authoritative value and no `/session` artifact exists to
correct.

**Evidence, 115,453 frames across both recorders:**

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

**The silent-on-read half is FIXED.** `parseFrameLine` discarded a bones payload
that failed to unmarshal without recording anything: the session parsed, so the
frame was returned and the line was not skipped, and the loss was invisible to
every counter the reader exposed. `EchoReplay.DroppedBones()` now counts it, and
it reaches `ConvertResult.DroppedBones` and `tapedeck convert`.

Measured end-to-end on the sample with two bones payloads truncated:

    $ tapedeck convert badbones.echoreplay
    converted: 1, skipped: 0, failed: 0
    total frames: 40, total events: 12
    unparsed bones payloads: 2 — those frames converted WITHOUT their bone data

Before this, that file reported 40 frames and 0 skipped — a clean conversion, by
every number the tool printed. Pinned by `TestDroppedBonesAreCounted`
(`pkg/codec/dropped_bones_test.go`), which also pins that an *absent* payload is
not a dropped one, and that the reader's deliberate `DiscardUnknown`
(`echoreplay.go:134`) means an unknown field is tolerated rather than counted.

**§3 is FIXED — per-frame inference, no proto change.**

The third field's content is determined by the presence or absence of bones in
the capture as a whole — a per-file inference, not a representability gap.

Spark recordings are all-or-nothing per file (213 all-bare, 1 all-bones in 215
measured). nevr-agent always polls: 0% bare 2-field, 98.9% populated, 1.1%
`{"err_code":0}` gaps. A gap is not the same as "bones never recorded," and
collapsing the two would make bones-off and an all-gaps capture
indistinguishable — the distinction cheat detection needs.

**Rule:** at reconstruction (`pkg/conversion/reconstruct.go`),
`NewSessionReconstructor` scans all frames to determine `hasBones`. If true,
every frame emits a 3-field line — gap frames get an empty
`PlayerBonesResponse` (protojson: `{"user_bones":[],"err_code":0}`). If false,
every frame emits a bare 2-field line. The semantic gap (endpoint polled but
no data returned) is preserved without a proto change.

The source form `{"err_code":0}` and the reconstructed form
`{"user_bones":[],"err_code":0}` differ in one key (empty array vs absent) —
semantically identical on read (`DiscardUnknown`, zero user_bones either way).

**Pinned by `TestReconstructPreservesBonesPresence`**
(`pkg/conversion/bones_reconstruct_test.go`, two subtests: hasBones+gap,
noBones).

---

## FIXED — KEYFRAME-001 (GH #23): a zero keyframe interval panicked the writer

**What:** `NewWriterWithKeyframeInterval(path, 0)` stored the interval
unvalidated, and the first `WriteFrame` evaluated `frameIndex %
w.keyframeInterval` — an integer divide by zero. Exported API, so any caller
passing 0 (plausibly meaning "no keyframes") crashed the process.

**Where:** `pkg/codec/tape.go:85`, constructor at `:53`.

**Evidence:** the guard test panicked before the fix:

    panic: runtime error: integer divide by zero
    github.com/echotools/tape/pkg/codec.(*Writer).WriteFrame
        /home/andrew/src/tape/pkg/codec/tape.go:85 +0x2d4

**Fix:** clamp 0 to `DefaultKeyframeInterval` in the constructor. This follows
the precedent already set for the identical class of bug in this repo —
`events.WithFrameBufferSize` (`pkg/events/events.go:48-56`) clamps a zero size
that otherwise divided by zero in `addFrameToBuffer`. Erroring instead would be
defensible, but no working caller passes 0 today (they panic), so the clamp
breaks nothing and keeps the two constructors consistent.

**Status: FIXED.** Pinned by `TestNewWriterWithKeyframeInterval_ClampsZero` and
`TestKeyframeIntervalZeroIndexesLikeTheDefault`
(`pkg/codec/keyframe_interval_test.go`). The second asserts the clamp picked a
meaningful value rather than merely dodging the panic: a zero-interval writer
produces a byte-identical keyframe index to an explicit default-interval one.

---

## FIXED — GOALTYPE-001: six of eleven goal types collapsed to UNSPECIFIED

**What:** `goalTypeStringMap` covered 5 goal types; the engine has 11. Anything
unmapped became `GOAL_TYPE_UNSPECIFIED`, silently losing the value on every
`GoalScored` event carrying it.

**Where:** the engine's table is contiguous in `echovr.exe` at
`0x1416e22b0`-`0x1416e2360` (read via ReVault):

| VA | string | was mapped |
|---|---|---|
| `0x1416e22b0` | `[NO GOAL]` | no |
| `0x1416e22c0` | `SLAM DUNK` | no |
| `0x1416e22d0` | `INSIDE SHOT` | yes |
| `0x1416e22e0` | `LONG SHOT` | yes |
| `0x1416e22f0` | `BOUNCE SHOT` | yes |
| `0x1416e2300` | `LONG BOUNCE SHOT` | yes |
| `0x1416e2318` | `HEADBUTT` | no |
| `0x1416e2328` | `LONG HEADBUTT` | no |
| `0x1416e2338` | `BUMPER SHOT` | no |
| `0x1416e2348` | `LONG BUMPER SHOT` | no |
| `0x1416e2360` | `SELF GOAL` | yes |

**Impact, measured:** across 30 sampled dal1 captures, `SLAM DUNK` appears in 7
files and `[NO GOAL]` in 6. A January 2026 client capture carries `SLAM DUNK` on
2,032 frames and `LONG HEADBUTT` on 729. All were being discarded.

**Fix:** six enum values added (nevr-proto `5c921c3`, numbered 6-11 after the
originals since renumbering a published enum is a wire break), map completed,
and `goalTypeReverse` added for reconstruction.

Tests: `TestGoalTypeMapCoversEveryEngineValue` walks the engine table and fails
on any value that maps to UNSPECIFIED; `TestGoalTypeRoundTrips` pins the reverse
direction, since a value that maps in but not back is still lost.

---

## FIXED — LASTSCORE-001: `last_score` never round-tripped

**What:** `SessionResponse.last_score` (7 fields) is a carried-forward snapshot —
the engine repeats the most recent goal on every frame. `GoalScoredSensor`
already emitted a `GoalScored` event on each change, and `GoalScored` already
carried all 7 fields, but nothing replayed it back: `reconstructSession` never
assigned `s.LastScore`.

**Fix:** `Session` gains a `lastGoal` lane replaying `GoalScored` forward
(`LastGoalAt`), and `reconstructSession` rebuilds `LastScore` from it. Required
`goalTypeReverse` and `teamRoleReverse` to render the enums back to the engine's
exact spellings — which is why GOALTYPE-001 had to land first: without the six
missing values, reconstruction would have written an empty `goal_type`.

**Evidence:** `last_score` removed from the BAC findings on the committed sample
and on the 12,817-frame / 114,805-player-frame audit capture, both reporting
"exact on all non-spatial, within tolerance on all spatial. 0 mismatches."

`last_throw` is now the only remaining field-level gap.

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
28 variants / 28 mappable (matches the current oneof, which gained
`PlayerInfoUpdated`), so any future variant added without a case fails
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

**FINAL STATUS (2026-08-02):** RECONSTRUCT-001 is FIXED, the reconstructor
round-trips every field the v2 format carries, and the DIRECTIVE's "Still OPEN"
items are all resolved except `last_throw` / `last_score` (CANONICAL-001 §2,
tracked separately as a known limitation — local-player-only in source). v2 is
a working round-trip format for the recoverable lane (kinematics + identity +
loadout + grab + disc + scores). FIDELITY-001's original claim ("no
v2→echoreplay path") is incorrect — the path exists and `TestRoundTripBAC`
exercises it on every commit.

This entry is kept OPEN as a design reference, not an active bug.

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

## FIXED — SENTINEL-OPTIONAL-001 (F-4): v1 −1 sentinel stored as present −1 pointer in v2 optionals

**Severity:** medium (format correctness). Three `mapEvent` sites always take the
address of the local variable and set the `optional int32` pointer, even when the
v1 value is the −1 sentinel ("disc free" / "no victim" / "unknown").

**Where / evidence:**

| site | file:line | field | proto contract |
|---|---|---|---|
| DiscPossessionChanged | `pkg/conversion/mapping.go:936-941` | `PlayerSlot`, `PreviousPlayerSlot` | "Absent if disc is free" (`echo_arena.proto:477-480`) |
| PlayerSteal | `mapping.go:1022-1027` | `VictimPlayerSlot` | "Absent if unknown" (`echo_arena.proto:553-554`) |

v1 uses −1 as a sentinel (e.g. `findPossessorSlot`, `pkg/events/sensor_disc.go:172`
returns −1 when no player holds the disc). The proto uses `optional int32` with the
contract "absent when free/unknown." A reader that honours the absence contract
(e.g. checking `HasPlayerSlot()` before reading) sees `true` for a free disc and
reads −1.

**Fix:** check for −1 before setting the pointer in both `mapEvent` cases.
`pkg/conversion/mapping.go`:
- `DiscPossessionChanged`: only set `PlayerSlot`/`PreviousPlayerSlot` pointers
  when the value is non-negative.
- `PlayerSteal`: only set `VictimPlayerSlot` pointer when non-negative.

**Status: FIXED.** Pinned by `TestMapEvent_DiscPossessionChanged_FreeDiscMapsToAbsentOptional`,
`TestMapEvent_DiscPossessionChanged_OccupiedDiscKeepsSlot`,
`TestMapEvent_DiscPossessionChanged_PartialFreeDisc`,
`TestMapEvent_PlayerSteal_NoVictimMapsToAbsentOptional`,
`TestMapEvent_PlayerSteal_WithVictimKeepsSlot`
(`pkg/conversion/sentinel_optional_test.go`).

---

## FIXED — WRITER-RACE-001 (GH #29): Writer carries shared mutable state with no synchronization

**Severity:** medium (concurrency safety). The `Writer` struct (`pkg/codec/tape.go:35-45`)
carries `frameCount`, `keyframes`, `eventIndex`, `bytesWritten`, `lastTimestampMs`,
the `zstd.Encoder`, and the `*os.File` — all mutated by `WriteFrame` with no mutex.
Concurrent `WriteFrame` calls race on every field.

**Fix:** added `sync.Mutex` to the `Writer` struct. `WriteHeader`, `WriteFrame`,
and `Close` each acquire it at entry and release it on return. The doc comment
states "Writer is safe for concurrent use."

**Status: FIXED.** Pinned by `TestWriter_ConcurrentWriteFrameRace`
(`pkg/codec/writer_race_test.go`) — 8 concurrent goroutines, `-race` clean.

---

## FIXED — EVENTDROP-001 (GH #18): event-detector drop counters existed but were never surfaced

**Severity:** medium (observability). The event detector's `DroppedFrames()` and
`DroppedEvents()` counters (`pkg/events/events.go:101-108`) were populated but
never read by the conversion pipeline or the CLI. Under the drain-per-frame
pattern the drop path is unreachable, so the counters are always zero — but
a regression would be invisible to every consumer.

**Fix:** `ConvertResult` gains `DroppedFrames` and `DroppedEvents` fields
(`pkg/conversion/convert.go:46-52`), populated after the conversion loop.
`tapedeck convert` reports non-zero values as warnings alongside the existing
`SkippedLines`/`DroppedBones`/`UnmappedValues` loss block.

The drop path itself is correct (non-blocking send, no deadlock — GH #26
confirmed not reproducible), and the drain-per-frame pattern keeps counters at
zero. The fix makes the observability contract explicit: drops are counted, the
counts are surfaced, and the CLI warns when they are non-zero.

**Status: FIXED.** Pinned by the existing `TestSyncDetector_ConversionPatternNoEventLoss`
and `TestSyncDetector_DropsWhenNeverDrained` (`pkg/events/sync_detector_drop_test.go`),
which prove the drop path is live and the conversion pattern keeps it at zero.

---

## FIXED — PROCDROP-001 (R6 / GH #18): default processing drops without a caller-visible receipt

**Severity:** medium (observability). `processing.New()` wraps the asynchronous
`events.AsyncDetector`, whose non-blocking sends drop frames and event batches
under back-pressure while incrementing private counters. The `Processor`
exposed only `EventsChan()` (`pkg/processing/frame_processor.go:90-93`) — no
loss metric and no error — so a caller using the default async processor had no
way to know frames or events were dropped. This is the second half of GH #18;
the conversion-side surfacing (`ConvertResult.DroppedFrames/DroppedEvents`) was
committed in `3f99fdf` (EVENTDROP-001).

**Evidence:** measured by saturation — 8 concurrent producers pushing 25,000
frames each (200,000 sends) into the default detector's 100-slot input channel
against its single one-frame-per-iteration consumer dropped 156,732–177,631
frames (78–89%) across 10 runs, and the counters were invisible through the
Processor.

**Fix:** `Processor` gains `DroppedFrames() uint64` and `DroppedEvents() uint64`
(`pkg/processing/frame_processor.go:114-132`), which type-assert the wrapped
detector for the unexported `frameDropCounter`/`eventDropCounter` interfaces
and return 0 for a custom `Detector` that does not count drops — a detector
which does not track loss is reported as having none. `New()`'s doc comment
documents the contract: drops are counted and surfaced, and a non-zero receipt
is a capacity signal. The non-blocking drop behavior itself is unchanged; this
is observability, not back-pressure.

**Status: FIXED.** Pinned by `TestProcessor_DroppedFramesReceipt`,
`TestProcessor_DroppedEventsReceipt`, and
`TestProcessor_DropCountersZeroForNonCountingDetector`
(`pkg/processing/frame_processor_test.go`).

---

## FIXED — FORMAT-VERSION-001 (GH #30): Reader accepted any format_version silently

**Severity:** low (forward-compatibility). `Reader.ReadHeader` (`pkg/codec/tape.go:343-353`)
accepted any `CaptureHeader` without checking `format_version`. A future tape with
`format_version=3` would be read without error, producing a header that downstream
code cannot interpret.

**Fix:** `ReadHeader` now validates `format_version` is 0 (pre-2.1) or 2. Any other
value returns `ErrUnsupportedVersion`. The sentinel is defined in `limits.go`
alongside the other reader errors.

**Status: FIXED.** Pinned by `TestReader_ReadHeader_RejectsUnknownFormatVersion`
and `TestReader_ReadHeader_AcceptsVersion2` (`pkg/codec/format_version_test.go`).

---

## FIXED — FRAMECOUNT-001 (GH #21): uint32 frame count wraps silently

**Severity:** low (only reached at ~4.3B frames, >82 days at 600 Hz). `Writer.frameCount`
(`pkg/codec/tape.go:47`) was uint32 with no overflow check. At `math.MaxUint32 + 1`
frames it wrapped to 0, corrupting the footer and all subsequent index entries.

**Fix:** `WriteFrame` now checks `w.frameCount == math.MaxUint32` before incrementing
and returns `ErrFrameCountOverflow` when the format limit is reached. The sentinel
is defined in `limits.go`.

**Status: FIXED.** Pinned by `TestWriter_WriteFrame_OverflowErrors` and
`TestWriter_WriteFrame_BelowMaxSucceeds` (`pkg/codec/frame_count_overflow_test.go`).

---

## FIXED — STOP-RACE-001 (F-14): sync-mode Stop() closes eventsChan without waiting for in-flight ProcessFrame

**Severity:** medium (crash risk). `AsyncDetector.Stop()` (`pkg/events/events.go:142-149`)
in synchronous mode calls `cancel()` then immediately `close(ed.eventsChan)` without
waiting for any in-flight `processFrameSync` to return. A concurrent `ProcessFrame`
that reaches the event-send `select` (`events.go:213-223`) after the channel is
closed panics with "send on closed channel."

The conversion path (`pkg/conversion/convert.go:260,269`) is single-threaded and
safe, but a library caller using `ProcessFrame` from one goroutine while tearing
down from another hits the panic on every run.

**Fix:** added `syncMu sync.Mutex` to `AsyncDetector`. `Stop()` acquires it after
`cancel()` (in sync mode only), blocking until any in-flight `processFrameSync`
completes, then closes the channel. `processFrameSync` acquires it before the
channel send and checks `ctx.Done()` first, so it returns without touching the
channel when Stop has been called.

**Status: FIXED.** Pinned by `TestSyncDetector_StopDuringProcessFrameDoesNotPanic`
(`pkg/events/stop_race_test.go`) — 200 concurrent Stop+ProcessFrame iterations,
`-race` clean, zero panics.

---

## FIXED — START-RACE-001 (R3): public double Start races detector state

**Severity:** high (concurrency safety). `events.New` calls `Start`
(`pkg/events/events.go:138`), and the public `Start`
(`pkg/events/events.go:141-144` before the fix) unconditionally started a
second background `processLoop`. With the loop already running from `New`, a
library caller's `detector.Start()` spawned a second worker; both then pulled
frames from the same `inputChan` and mutated the same shared state —
`writeIndex`, `frameCount`, `frameBuffer` (`addFrameToBuffer`,
`pkg/events/events.go:331-338`) and the reused `eventBuffer` (events.go:283)
— with no synchronization.

**Evidence (measured):** `TestAsyncDetector_DoubleStartDoesNotRace`
(`pkg/events/double_start_test.go`) is RED pre-fix under
`go test -race -count=1 -run TestAsyncDetector_DoubleStartDoesNotRace
./pkg/events/`: 38 `WARNING: DATA RACE` reports across the two workers
(greater than the 11 reported by the release audit for the same pattern).

**Fix:** `Start` is now idempotent via a `sync.Once startOnce` — the loop is
started at most once, so a redundant `Start` cannot spawn a second worker. It
is a no-op in synchronous mode (no background loop exists) and a no-op after
`Stop` (a stopped detector cannot be restarted; guarded by an `atomic.Bool
stopped` set in `Stop`). `New`'s doc comment now states it auto-starts in async
mode.

**Status: FIXED.** Pinned by `TestAsyncDetector_DoubleStartDoesNotRace`
(New-then-Start under `-race`, green) and
`TestAsyncDetector_StartAfterStopIsNoop` (`pkg/events/double_start_test.go`),
which pins the documented no-restart-after-Stop semantics.

---

## FIXED — STOP-DEADLOCK-001 (GH #26): Stop() deadlock with slow event consumer — not reproducible in current code

**Severity:** was medium (shutdown hang). GH #26 claimed `processLoop` blocks on
a full `eventsChan` send while `Stop()` `wg.Wait()`s, deadlocking shutdown. The
issue's evidence describes an older `stopChan`-based design; in the current code
the `eventsChan` send in `processLoop` is non-blocking — a full channel takes the
`default` branch and drops the batch (`pkg/events/events.go:300-303`), so
`Stop()`'s `wg.Wait()` cannot wait on a blocked send.

**Evidence (measured):** `TestAsyncDetector_StopWithFullUndrainedEventsChanReturns`
(`pkg/events/stop_full_eventschan_test.go`) feeds more event-producing frames
than the 10-buffered `eventsChan` can hold, never drains, and requires `Stop()`
to return within a 2s deadline. Green on the pre-fix code — the non-blocking
send design already resolved the claimed deadlock.

**Status: FIXED.** Resolved by the existing non-blocking send, confirmed by
`TestAsyncDetector_StopWithFullUndrainedEventsChanReturns`
(`pkg/events/stop_full_eventschan_test.go`), committed alongside the START-RACE-001
fix.

---

## FIXED — READFRAMETO-PARITY-001 (release-audit R4): `ReadFrameTo` discarded a session `ReadFrame` retains

**Severity:** medium (fidelity; ledger-mismatch with the fixed dropped-bones
accounting claim, CANONICAL-001 §3).

**What:** `ReadFrame` counts a bones payload that fails to unmarshal via
`DroppedBones` and still returns the line's session
(`pkg/codec/echoreplay.go:596-617`). The reuse API `ReadFrameTo` instead
returned a bones error (`echoreplay.go:799-806`), whose caller incremented
`SkippedFrames` and discarded the whole line (`echoreplay.go:721-723`) — a valid
session that `ReadFrame` kept was silently lost through the reuse path.

**Where:** `pkg/codec/echoreplay.go` — `parseFrameLineTo` bones branch (was
`:799-806`), `ReadFrameTo` skip loop (`:721-723`).

**Evidence:** measured pre-fix on one `2023/01/01 12:00:00.000\t{}\t{` line:

    ReadFrame   frame=true  err=<nil>  skipped=0 droppedBones=1
    ReadFrameTo ok=false    err=EOF    skipped=1 droppedBones=0

**Fix:** `parseFrameLineTo` now mirrors `parseFrameLine` exactly: a bones
payload that does not unmarshal increments `DroppedBones`, leaves `PlayerBones`
nil, and returns nil — the frame survives with its session intact, and the line
is not skipped. Measured post-fix on the same line:

    ReadFrame   frame=true  err=<nil>  skipped=0 droppedBones=1
    ReadFrameTo ok=true     err=<nil>  skipped=0 droppedBones=1

**Status: FIXED.** Pinned by `TestReadFrameToReadFrameParity`
(`pkg/codec/dropped_bones_test.go`) — valid, malformed-bones, and skip lines
yield identical sessions, `SkippedFrames`, and `DroppedBones` through both
entry points.

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
- ~~fix GH #18 (silent event loss)~~ — **RESOLVED 2026-08-02**: drop counters
  surfaced on both call sites — `ConvertResult` (EVENTDROP-001) and `Processor`
  (PROCDROP-001) — so the loss is counted, not silent; then v1 deprecation + v1→v2 importer.

**Status as of 2026-08-02 — the DIRECTIVE is effectively complete.** Every
"Still OPEN" item except `last_throw` / `last_score` has been shipped and tested:

| Was OPEN | Resolution |
|---|---|
| `rules_changed_by` / `rules_changed_at` | FIXED — header fields, `TestReconstructPreservesSupersetFields` |
| `game_clock_display` | FIXED — CLOCK-001, derived from `game_clock` |
| `blue/orange_round_score` | FIXED — `TestReconstructPreservesInitialRoundScores` |
| `team_name` | FIXED — header field, round-trips |
| `jersey_number` / `level` | FIXED — ROSTER-001, `PlayerInfoUpdated` |
| `pause` sub-state | FIXED — full `PauseState` round-trips |
| `team.stats` / `player.stats` | FIXED — STATS-001, event + per-frame |
| GH #18 (silent event loss) | FIXED — EVENTDROP-001, drop counters surfaced |
| **`last_throw` / `last_score`** | **REMAINING** — CANONICAL-001 §2; `last_throw` is local-player-only in the source, so wiring it without solving identity would fabricate data. Tracked as a known limitation. |
| v1 deprecation + v1→v2 importer | Deferred — v1 codec still used as the intermediate layer in conversion. Not blocking the v4.0.0 tag. |

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
