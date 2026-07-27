# Tape Format — Design, Fidelity, and Reconstruction

The authoritative reference for how tape stores Echo VR telemetry, what it
keeps, what it drops, why, and how to get identity and lost data back. Read this
before touching the format, the converter, or the proto. It exists so nobody
re-derives this from scratch again. Every claim here is backed by a test in
`pkg/conversion/*_audit_test.go` / `*_probe_test.go` — run them on any recording
with `TAPE_AUDIT_FILE=/path go test ./pkg/conversion/ -run <Name> -v`.

---

## 1. The three formats and how they relate

| Format | What it is | Fidelity |
|---|---|---|
| **`.echoreplay`** | The game's own recording: ZIP of `timestamp\tsessionJSON[\tbonesJSON]` lines. `sessionJSON` is an `engine.v1.SessionResponse` — the `/session` API payload, the **complete** per-frame truth. | Full source of truth |
| **v1 (`telemetry.v1`)** | Proto mirror. `LobbySessionStateFrame` **embeds the whole `SessionResponse`** (field 4) + bones + events. The on-disk form is the **Legacy** codec (`.nevrcap`). | Full — nothing is lost at the v1 layer |
| **v2 (`telemetry.v2`)** | The current on-disk tape: zstd stream of `Envelope`s (`CaptureHeader` + `Frame` + `CaptureFooter` with seek indexes). `float32` spatial. Per-frame payload is `EchoArenaFrame`. | **Re-encoded, not curated** — see §2 and §4 |

**Conversion flow (`tapedeck convert`):** echoreplay/nevrcap → v1 `LobbySessionStateFrame`
→ `MapFrame` (`pkg/conversion/mapping.go`) → v2 `EchoArenaFrame` → `.tape`.
The loss happens **only** at the v1→v2 `MapFrame` step.

**The v2→echoreplay reverse path exists** — `ReconstructFile` /
`SessionReconstructor` (`pkg/conversion/reconstruct.go:478`), replaying the
header, frames and events through the `Session` state layer to rebuild each
per-frame `SessionResponse`. `TestRoundTripBAC` exercises the full
`echoreplay → v2 → echoreplay` cycle on every commit. (There is still no
v2→v1 path; `ConvertNevrcapToEchoReplay` is v1→echoreplay and has no caller.)

`echoreplay ↔ echoreplay` through the v1 codec is **lossless** — proven
field-for-field on 1023 frames (`TestEchoReplayRoundTripFidelity`, 0 diffs).

**On "lossy by design".** This table used to call v2 *curated — lossy by design*,
and that phrasing has been read as licence to accept any missing field as
intentional. It is not what §2 says. v2 stores the same information in a
different **shape**: constants once in the header, discrete changes as events,
per-frame data per frame. A field that lives in the header or an event is not
lost — provided reconstruction re-materializes it. The genuinely-absent fields
are the short list in §4, and each is a gap to close, not a design decision.

## 2. The v2 design philosophy (do not violate this)

v2 is **not** "every field, every frame." It is a high-performance format for
60k+ frames at low allocation/size, organized by **how often data changes**:

- **Session-constant → the header.** Names, account numbers, jersey, level, role
  live in `EchoArenaHeader.initial_roster` (`PlayerInfo`), written once. Server
  IP lives in `EchoArenaHeader.session_ip`.
- **Discrete changes → events** (`EchoEvent`). Joins, leaves, goals, team
  switches, disc possession changes, loadout changes, grab changes. Reconstruct
  per-frame state by replaying events (see §5).
- **Per-frame-varying → per-frame fields** on `EchoArenaFrame` / `PlayerState`.
  Poses, velocity, flags, ping, scores, clock, disc, analog shoulder input,
  packet loss. Frames are **self-contained for random access** (scores/round
  repeat every frame, ~6–9 bytes). proto3 writes **nothing for a zero value**,
  so an idle per-frame field costs 0 bytes on the wire.

**`float32` is correct, not a loss.** Coordinates never exceed ±40. A `float32`
ULP at magnitude 40 is 2^(5−23) ≈ **3.8 µm** — ~260× finer than a millimeter,
far below any real game resolution. `float64→float32` discards femtometer noise,
not signal. Orientation goes 9-float basis → quaternion (exact for a real
rotation) → `float32` (~1e-7 rad). **There is no meaningful precision loss in
v1→v2.** The loss is dropped *fields*, never resolution.

## 3. What v2 keeps vs. drops (measured, not asserted)

Measured on a real arena recording (`TestFieldLossAudit`, 22,727 frames /
234,482 player-frames):

- **Kept — 100%:** head/body/both-hands poses + velocity + bones every frame;
  disc, scores, clock, round, game status; events. The full kinematic record.
- **Dropped but always-empty in arena (0%) — no real loss:** `weapon`,
  `ordnance`, `tac_mod`, payload fields. (Echo *Combat* fields.)
- ~~**Dropped and genuinely present:** grab state, `packet_loss_ratio`, analog
  shoulder~~ — **all three now have v2 homes and round-trip** (the §6 superset
  landed): `GrabChanged` event, `PlayerState.packet_loss_ratio`, and all four
  `EchoArenaFrame` shoulder fields including the `_2` variants. Loadout
  (`weapon`/`ordnance`/`tac_mod`) likewise, via `LoadoutChanged`.
- **Not actually lost (already represented):**
  - `has_possession` → `PlayerState.flags` bit 3, and `disc_holder_slot` (frame).
  - round scores → `ScoreboardUpdated` event (seeded at frame 0, so the opening
    scoreboard is recorded — `pkg/events/sensor_scoreboard.go`).
  - per-player names/account/jersey/level → `initial_roster` + `PlayerJoined`.
  - **`possession[]` (engine field 23) — fully redundant, intentionally not
    added.** It is `[team, in-team-player-index]`, proven on alienq to agree
    with `has_possession` 100% (0 frames where one knew the holder and the other
    didn't). See `TestPossessionProbe`. The team is derivable from the roster.
    Rebuilt by `reconstructPossession` (`reconstruct.go:381`).
  - **`game_clock_display` — derived, not stored.** It is a pure formatting of
    the per-frame `game_clock`: `trunc(clock*100)` centiseconds rendered
    `"%02d:%02d.%02d"` (the engine truncates, does not round; minutes are
    zero-padded). Exact on 13,840 real frames. It was previously read from the
    `ScoreboardUpdated` sample, which fires only on a score change, so it was
    stale between goals and empty before the first — wrong on 100% of frames in
    both available recordings. Same reasoning as `possession[]`: redundant and
    reconstructable, so reconstruct it. See BUGS.md CLOCK-001.

## 4. Round-trip

- **Lossless today:** `echoreplay ↔ echoreplay` via the v1 codec (full
  `SessionResponse`).
- **`echoreplay → v2 → echoreplay` runs today** and is exact on every
  recoverable field — `TestRoundTripBAC`, 0 mismatches on the committed sample
  (max magnitude deviation 5e-6, max orientation 2.4e-3, both inside tolerance).
  The reconstructor **is** built (§1).
- **What still does not round-trip** (measured; `TestRoundTripBAC` reports these
  as findings, and the audit lane confirms them on a 12,817-frame capture):
  - `rules_changed_by` / `rules_changed_at`, `err_code`, both
    `*_team_restart_request` — **no v2 field exists**. Needs a proto addition.
  - `team_name`, `Team.stats`, `TeamMember.stats` — **no v2 field for any of
    them, and the stats are NOT derivable from the events.** Measured on the
    committed sample: the engine reports `stuns=0 catches=0` for both players
    while the event stream yields 3 and 5 (`tapedeck stats`), and engine
    `possession_time` is already 13.22s on frame 0 because the capture starts
    mid-match. The engine's counters are an independent quantity with a
    pre-capture baseline; accumulating events from frame 0 fabricates a
    different number that happens to look plausible. These need a proto home.
  - `last_throw` / `last_score` — `ThrowDetails` and `GoalScored` carry every
    field, but neither is read off the session on the way in nor written on the
    way out. Note `last_throw` is **local-player-only** in the source; see
    BUGS.md before wiring it.
  - per-frame `jersey_number` / `level` — vary per frame (12,572 frames on the
    audit capture); v2 stores only a join snapshot.
  - `pause` sub-state — v2 narrows five engine fields to one enum plus
    `RoundPaused{requesting_team, pause_timer}`.
  - empty-team structural case — a team with no players has no v2 representation.
- **Goal:** make v2 a **superset** so the round-trip is field-identical. What
  remains is the proto additions for the genuinely-absent fields above, plus
  read-side plumbing for the ones that already have homes.

## 5. Identity / roster reconstruction (the per-frame state is anonymized)

`PlayerState` carries **slot only** — no name/account. Identity comes entirely
from events:

- `PlayerJoined` carries slot + name + account + role + jersey + level.
  `previousPlayers` starts empty, so **every player present at frame 0 emits a
  `PlayerJoined`** — the initial roster's names are in the tape.
- `PlayerLeft` carries slot + name → handles slot reuse (someone leaves slot 3,
  someone else takes it).
- Replaying joins/leaves rebuilds slot→name for any frame. Proven 100% on alienq
  (`TestRosterRebuildAudit`: 12 joins + 7 leaves recover all 234,482
  player-frames, 0 wrong, 0 missing). This is what GH #34 (`Session.RosterAt`)
  formalizes.
- **Risk:** GH issue #18 (silent event loss on a full channel). If a join is
  dropped during conversion, that slot goes anonymous. Did not occur on alienq
  (19 events total). Fix #18 before relying on roster reconstruction at scale.

## 6. The superset plan (proto change in `nevr-proto/telemetry/v2/echo_arena.proto`)

Placed by §2's rule — nothing constant or rare gets propagated per-frame.

**SHIPPED** — all of the following are in the proto and wired end to end:

- **Per-frame:** `packet_loss_ratio` on `PlayerState`; the capture client's analog
  shoulder input (`left/right_shoulder_pressed` + alternate) on `EchoArenaFrame`
  alongside `vr_root` — it's the recording client's own input, not per-player.
- **Events** (rare changes; seed at frame 0, reconstruct via Session):
  `LoadoutChanged{weapon,ordnance,tac_mod}`, `GrabChanged{left_holding,right_holding}`.
- **Combat-only sub-message** (absent in arena): `EchoArenaFrame.payload`
  (`PayloadState`).
- **Header** (constant): `EchoArenaHeader.session_ip`.
- **Not added:** `possession[]` (redundant — §3).

**Still needed** — fields with no v2 home at all (§4):
`rules_changed_by`, `rules_changed_at`, `err_code`,
`blue/orange_team_restart_request`, `Team.team_name`, the four `PauseState`
sub-fields v2 narrows away, and `EventType` values for `LoadoutChanged` /
`GrabChanged` (BUGS.md INDEX-001 — without them those two events cannot appear
in the footer's event index).

Ship path: edit proto → CI `buf push` on merge to `nevr-proto` main publishes
the BSR module → `go get` the new version in tape (or `buf generate` + a local
`replace` for dev) → wire `MapFrame` to populate it → wire `reconstruct.go` to
read it back → round-trip test. **Both directions in the same change** — a field
written but never read is the failure mode this repo keeps repeating.

## 7. Open work (also tracked in `BUGS.md`)

- ~~Build the v2→echoreplay reconstructor~~ — **done** (`reconstruct.go`, §1).
- ~~Build GH #34 `Session`~~ — **partial**: `RosterAt`/`LoadoutAt`/`GrabAt`/
  `ScoreAt` exist, but `replay()` handles only 6 of 26 `EchoEvent` variants and
  there is no disc-holder accessor. The stat events it ignores are exactly why
  `player.stats`/`team.stats` cannot be reconstructed.
- Proto additions for the genuinely-absent fields (§6 "Still needed").
- Wire `last_throw` / `last_score` — homes exist, plumbing absent at both ends.
- Fix GH #18 (silent event loss) — the one hole in §5.
- Deprecate v1 as a runtime dep once v2 is a superset; add a v1→v2 importer.

## 8. The audit tools (reusable receipts)

**None of these run in CI** — every one skips unless `TAPE_AUDIT_FILE` is set,
so the numbers quoted throughout this document come from tests that only ran
when someone ran them by hand. Re-measure before relying on any of them.

In `pkg/conversion/`, taking `TAPE_AUDIT_FILE` and skipping when absent:

- `field_loss_audit_test.go` — what's dropped + how often it carries data
- `roster_rebuild_audit_test.go` — identity recovery from events
- `possession_probe_test.go` — `possession[]` redundancy
- `roundtrip_v2_test.go` `TestRoundTripBACAudit` — the full
  `echoreplay → v2 → echoreplay` comparison on an external capture

`TAPE_AUDIT_FILE` must be an **absolute** path: these run with the package
directory as cwd, and a relative path silently takes the skip branch.

`roundtrip_baseline_test.go` (`TestEchoReplayRoundTripFidelity`) does **not**
take `TAPE_AUDIT_FILE` — it is hardcoded to `testdata/sample.echoreplay` and so
runs in CI. This document previously listed it among the audit tools; it is not
one.

Run them on a new recording before assuming anything about it.
