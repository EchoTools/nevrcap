# Primer — Tape v1→v2 Conversion Data-Loss Fixes

**You are fixing data loss in the tape conversion + event-sensor layer.**
Read this whole file before touching code. Then read
`.claude/GO-ADDENDUM-GENERIC.md` — it is binding for every `.go` file you
write or change.

---

## What `tape` is (read this — it's how you make good calls, not just blind edits)

`tape` is a **high-performance telemetry codec library + CLI (`tapedeck`) for
Echo VR session data**. Module `github.com/echotools/tape`. It is a **core
dependency of `nevr-agent`, `nevr-anticheat`, and `nevr-profiler`.** The
anticheat is the live cheater investigation — if the capture isn't a faithful
record of the match, the anticheat is reasoning over a lie. Faithfulness is
the product.

**The three formats it speaks:**

- **EchoReplay** (`.echoreplay`) — the *legacy* on-disk format produced by the
  game engine: a ZIP wrapping line-oriented `timestamp\tsessionJSON[\tbonesJSON]`.
  The `sessionJSON` is a **`SessionResponse`** — the game's `/session` API
  payload, the per-frame source of truth (teams, players, disc, scores, clock,
  possession, …). This is the *input* most data-loss bugs touch.
- **TapeV1** — zstd-compressed, length-delimited `telemetry.v1` protobuf:
  a `TelemetryHeader` + sequential `LobbySessionStateFrame`. Spatial as 64-bit
  `repeated double`. Extensions `.tape` / `.nevrcap` (legacy).
- **TapeV2** (current) — zstd-compressed, length-delimited
  `telemetry.v2.Envelope` oneof: `CaptureHeader` + `Frame` (game-agnostic
  timing + oneof payload) + `CaptureFooter` (frame count, duration,
  keyframe/event seek indexes). Spatial as `spatial.v1` **float32** (~73.5%
  smaller). This is where the "no v2 counterpart" bugs live.

**The packages:**
- `pkg/codec` — readers/writers for the three formats above.
- `pkg/conversion` — maps between them; `mapping.go` turns a `SessionResponse`
  into proto frames, running event detection so the output is enriched.
- `pkg/events` — sensors walk sequential frames and emit `LobbySessionEvent`
  (player joins, goals, gamestate). The drain loop in BUG-1 is here.
- `pkg/processing` — the 600 Hz frame processor.

**Invariants you must NOT break while fixing data loss:**
1. **Round-trip fidelity:** `decode(encode(x)) == x` for the tape format. Any
   codec/mapping change must preserve it — there are golden tests; keep them green.
2. **Byte-level EchoReplay compatibility:** the echoreplay writer applies
   `FixProtojsonUint64Encoding` and `FixExponentNotation` to match the engine's
   output exactly. **Third-party parsers depend on these bytes.** Do not perturb them.
3. **Performance:** this is a hot path (built for up to 600 Hz). No per-frame
   allocations you can avoid, no needless copies. Faithful *and* fast.

**The v2 proto is NOT in this repo.** It's generated from `nevr-proto`
(`buf.build/echotools/nevr-api`) and vendored as read-only Go types under
`buf.build/gen/go/...`. You **cannot** add a v2 field here. That is the hard
wall behind the schema-gap fork below.

## The prime directive: VERIFY before you FIX. Trust nothing.

This work exists because a previous cog wrote *"this works"* on code it
**never tested**, and a 117-line workaround grew on top of that false claim.
You will not repeat that.

- **Do not trust the audit tests.** They came from the same source that lied
  once. For every bug, confirm the data loss against the *actual code* —
  read `mapping.go` / the sensor file, find the exact line where the field is
  dropped, and cite it `file:line`. The audit test is a hint, not proof.
- **Do not trust your own fix.** A fix is only real when a test asserts the
  *corrected* value and that test fails before your change and passes after.
- If a bug turns out **not** to be real, say so with evidence and move on.
  A refuted bug is a valid, valuable outcome — that is the whole point of
  verifying.

## Ground truth (already established — do not re-litigate)

- Branch: `fix/tape-conversion-data-loss`. Work here. **Do NOT merge to `main`.**
- Module is on **Go 1.26** (`go.mod`). Use 1.26 idioms (`errors.AsType`,
  `new(expr)`, `b.Loop()`, range-over-func) where they fit. Build/vet/`-race`
  suite was green at the start of your run.
- Audit tests are committed and **pass today** because they assert the
  *current broken* behavior. When you fix a bug, you will flip its audit
  test to assert the *correct* behavior (Red→Green).
- The conversion path lives in `pkg/conversion/mapping.go` (+ `converters.go`,
  `convert.go`). Event sensors live in `pkg/events/sensor_*.go`.

---

## The 8 bugs

| Bug | Symptom | Audit test (the hint) |
|---|---|---|
| **BUG-1** | BuildRoster: false "tested" comment + ~117-line workaround built on the unverified claim that the PlayerJoined drain loop is broken. The drain loop **actually works**. | sensor_audit_test.go: `TestPlayerJoinSensor_Multiple*`, `TestPlayerJoinSensor_MultipleTeamsJoin`, `TestEmptyBufferBug_*`, `TestDetectEvents_NoFalseEvents` |
| **BUG-2** | `GoalScored.ScorerSlot`/`AssistSlot` always zero (names never resolved to slots) | `TestGoalScored_ScorerSlotAndAssistSlotStayZero` |
| **BUG-3** | `GoalScored` PersonScored / AssistScored display names dropped entirely | `TestGoalScored_DisplayNamesDropped` |
| **BUG-4** | `InitialRoster` always nil — never populated from `session.GetTeams()` | `TestMapHeaderFromSession_InitialRosterNil` |
| **BUG-5** | 19 `SessionResponse` fields dropped: possession tracking, payload data, clock display, restart requests, … | `TestMapFrame_SessionFieldsDropped`, `TestMapFrame_AdditionalSessionFieldsDropped` |
| **BUG-6** | Team-level data dropped: `TeamName`, `HasPossession`, `Stats` | `TestMapFrame_TeamFieldsDropped` |
| **BUG-7** | Per-frame `TeamMember` fields dropped: `DisplayName`, `AccountNumber`, `PacketLossRatio`, `Weapon`, `Ordnance`, grab state | `TestMapFrame_TeamMemberFieldsDropped` |
| **BUG-8** | `PlayerJoined` `JerseyNumber` and `Level` dropped — **source and dest fields both exist, just not wired** | `TestPlayerJoined_JerseyNumberAndLevelDropped` |

Also honor these audit tests, don't ignore them:
`TestMapFrame_DiscWithPartialOrientation`, `TestEmotePlayed_EmoteTypePreserved`.

## BUG-1 in detail (the one that started this)

1. **Locate** the BuildRoster logic / the ~117-line workaround. `grep` did not
   find a literal `BuildRoster` symbol in `tape` — find the real function
   (roster construction around `InitialRoster` / PlayerJoined handling). If it
   genuinely does not exist in this repo, report that and treat BUG-1 as the
   InitialRoster wiring (BUG-4) plus removing any dead drain-workaround.
2. **Confirm the drain loop works** by reading the sensor and running the
   PlayerJoin sensor tests. Quote the passing output.
3. If the workaround is dead weight built on the false premise: **remove it.**
   Every removal gets a `grep` proving nothing else depends on the removed
   symbols — paste the grep showing zero remaining references. No dead code
   left behind, no silently orphaned helpers.

## The wire-up vs schema-gap fork (READ THIS — do not fabricate schema)

The v2 destination is the tape proto (`buf.build/.../nevr-api`). For each
dropped field decide which case you are in:

- **WIRE-UP** — the v2 destination field already exists, the mapping just
  doesn't set it (BUG-8 is explicitly this; BUG-2/3/4 likely too). → Wire it,
  test it, done.
- **SCHEMA-GAP** — there is **no** v2 field to hold the data (some of BUG-5/6/7
  "no v2 counterpart"). → **Do NOT invent proto fields.** That proto is
  generated from another repo; fabricating a schema is exactly the
  build-on-an-unverified-premise failure that caused this. Instead, document
  the gap precisely: field name, source struct + `file:line`, what v2 would
  need. Collect these into a `SCHEMA-GAPS.md` report for Andrew's decision.

Map **everything that has a home.** No arbitrary subset, no "good enough"
cherry-pick — if the destination exists, the data goes through.

---

## Method, per bug (TDD, non-negotiable)

1. Read the real code; cite the drop site `file:line`.
2. Confirm or refute the audit test's claim.
3. Write/adjust the test to assert the **correct** behavior. Run it — watch it
   fail (Red).
4. Fix the mapping/sensor.
5. Run the test — watch it pass (Green). Quote the output.
6. Commit this bug alone (small, reviewable commits).

## The gate (must be clean before you report done)

Run the project gate (`justfile`): `just fmt vet lint test` plus `-race`.
From the addendum's hard stops, all must hold:

- `gofmt -l .` → empty
- `go vet ./...` → empty
- `golangci-lint run` → 0 issues (the pre-commit hook enforces this — no `--no-verify`)
- `go test -race -count=1 ./...` → all pass
- `go fix ./...` → no changes, `go mod tidy` → no changes
- coverage on changed packages does not drop

## Commit discipline

- Author every commit `Spritz Metis Sprock <spritz@sprock.io>`,
  trailer `Co-Authored-By: Andrew Bates <a@sprock.io>`.
- Sign OFF on these WIP commits: `git -c commit.gpgsign=false commit ...`
  (Andrew signs the merge to main himself).
- One bug per commit where practical. Conventional-commit subjects
  (`fix:`, `test:`, `refactor:`).
- **Never** `git push`, never merge to main, never touch `gh`. Andrew handles
  the remote under the `metis-sprock` identity.

## Report back (this is what you return)

For **each** of the 8 bugs:
- Verdict: **REAL** / **REFUTED** (with the `file:line` evidence).
- If REAL: what was dropped, the fix, the test that now proves it (+ output),
  the commit hash.
- WIRE-UP vs SCHEMA-GAP classification.
- For BUG-1: the drain-loop confirmation + what you removed + the grep proof.

End with the full gate output (vet/lint/`-race`/fix/tidy) pasted, and a list
of any SCHEMA-GAPS deferred to Andrew. No "close enough." Every claim cited.
