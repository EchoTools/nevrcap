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
| **v2 (`telemetry.v2`)** | The current on-disk tape: zstd stream of `Envelope`s (`CaptureHeader` + `Frame` + `CaptureFooter` with seek indexes). `float32` spatial. Per-frame payload is `EchoArenaFrame`. | **Curated** — lossy by design (see §3) |

**Conversion flow (`tapedeck convert`):** echoreplay/nevrcap → v1 `LobbySessionStateFrame`
→ `MapFrame` (`pkg/conversion/mapping.go`) → v2 `EchoArenaFrame` → `.tape`.
The loss happens **only** at the v1→v2 `MapFrame` step. **There is currently no
v2→echoreplay or v2→v1 reverse path** — the only reverse converter is
`ConvertNevrcapToEchoReplay` (v1→echoreplay).

`echoreplay ↔ echoreplay` through the v1 codec is **lossless** — proven
field-for-field on 1023 frames (`TestEchoReplayRoundTripFidelity`, 0 diffs).

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
- **Dropped and genuinely present:** grab state `left/right_holding_onto`
  (~10% of player-frames), per-player `packet_loss_ratio` (~7%), analog
  `*_shoulder_pressed[2]` (~31% of frames). These are the real gaps.
- **Not actually lost (already represented):**
  - `has_possession` → `PlayerState.flags` bit 3, and `disc_holder_slot` (frame).
  - round scores + `game_clock_display` → `ScoreboardUpdated` event.
  - per-player names/account/jersey/level → `initial_roster` + `PlayerJoined`.
  - **`possession[]` (engine field 23) — fully redundant, intentionally not
    added.** It is `[team, in-team-player-index]`, proven on alienq to agree
    with `has_possession` 100% (0 frames where one knew the holder and the other
    didn't). See `TestPossessionProbe`. The team is derivable from the roster.

## 4. Round-trip

- **Lossless today:** `echoreplay ↔ echoreplay` via the v1 codec (full
  `SessionResponse`).
- **Lossy today:** `echoreplay → v2`. And there is **no** `v2 → echoreplay`
  path, so "round-trip an echoreplay through v2" is not currently possible at
  all.
- **Goal:** make v2 a **superset** so `echoreplay → v2 → echoreplay` is
  byte/field identical. That needs both the proto additions in §6 **and** a
  v2→echoreplay reconstructor (replay header + frames + events to rebuild each
  per-frame `SessionResponse`). The reconstructor is not built yet.

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

Placed by §2's rule — nothing constant or rare gets propagated per-frame:

- **Per-frame:** `packet_loss_ratio` on `PlayerState`; the capture client's analog
  shoulder input (`left/right_shoulder_pressed` + alternate) on `EchoArenaFrame`
  alongside `vr_root` — it's the recording client's own input, not per-player.
- **Events** (rare changes; seed at frame 0, reconstruct via Session):
  `LoadoutChanged{weapon,ordnance,tac_mod}`, `GrabChanged{left_holding,right_holding}`.
- **Combat-only sub-message** (absent in arena): `EchoArenaFrame.payload`
  (`PayloadState`).
- **Header** (constant): `EchoArenaHeader.session_ip`.
- **Not added:** `possession[]` (redundant — §3).

Ship path: edit proto → CI `buf push` on merge to `nevr-proto` main publishes
the BSR module → `go get` the new version in tape (or `buf generate` + a local
`replace` for dev) → wire `MapFrame` to populate it → round-trip test.

## 7. Open work (also tracked in `BUGS.md`)

- Build the v2→echoreplay reconstructor (true round-trip; §4).
- Wire the converter for the §6 fields + a round-trip BAC test.
- Fix GH #18 (silent event loss) — the one hole in §5.
- Build GH #34 `Session` (roster/score/disc-holder reader layer).
- Deprecate v1 as a runtime dep once v2 is a superset; add a v1→v2 importer.

## 8. The audit tools (reusable receipts)

In `pkg/conversion/`, all take `TAPE_AUDIT_FILE` and skip when absent:
`roundtrip_baseline_test.go` (echoreplay round-trip), `field_loss_audit_test.go`
(what's dropped + how often it carries data), `roster_rebuild_audit_test.go`
(identity recovery from events), `possession_probe_test.go` (possession
redundancy). Run them on a new recording before assuming anything about it.
