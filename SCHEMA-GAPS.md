# SCHEMA-GAPS — v1→v2 conversion fields with no v2 home

These fields exist in the v1 source (`engine.v1` / `telemetry.v1`) but have **no
destination in the v2 `telemetry.v2` proto**. The v2 proto is generated from the
separate `nevr-proto` repo (`buf.build/echotools/nevr-api`) and is read-only
here, so these cannot be wired up in `tape` — they require a proto change in
`nevr-proto` first. Listed for Andrew's decision; **no proto fields were
invented**.

Field-number citations are proto field tags (stable). Go struct evidence is the
vendored generated types under
`buf.build/gen/go/echotools/nevr-api/protocolbuffers/go@v1.36.11-…/`:
- v1 source: `engine/v1/engine_http.pb.go`
- v2 destination: `telemetry/v2/echo_arena.pb.go`

Verified v2 `EchoArenaFrame` fields (the only per-frame homes that exist):
`game_status, game_clock, pause_state, disc, players, player_bones,
disc_holder_slot, vr_root, blue_points, orange_points, round_number, events`.

---

## BUG-2 / BUG-3 — GoalScored scorer/assist identity

Source: `engine.v1 LastScore` (`engine_http.pb.go` — `PersonScored` field 6,
`AssistScored` field 7). Reached via `telemetry.v1 GoalScored.ScoreDetails`.
Drop site: `pkg/conversion/mapping.go` `mapEvent`, `GoalScored` case
(currently mapping.go:666-679).

- **`PersonScored` (string, name)** → no v2 home. `telemetry.v2 GoalScored`
  (echo_arena.pb.go) has `scorer_slot` (field 6, int32) and `assist_slot`
  (field 7, *int32) — **slot indices, not names**. There is no name field.
- **`AssistScored` (string, name)** → same: no v2 name field.
- **`ScorerSlot` / `AssistSlot`** cannot be resolved from a v1 display name:
  `LastScore` carries no slot, and `mapEvent` has no roster to map name→slot.
  A heuristic name match could mis-attribute a goal — unacceptable for an
  anticheat record.

**Not data loss in practice:** the authoritative scorer/assister *slots* are
already preserved faithfully via the `PlayerGoal` and `PlayerAssist` events,
which carry the real `PlayerSlot` (emitted by the stat sensor —
`pkg/events/sensor_stats.go:159-161` and `:259-261`; mapped at
`pkg/conversion/mapping.go` `PlayerGoal`/`PlayerAssist` cases). Only the
free-text display names on `LastScore` are dropped.

**What v2 would need (decision):** either add `person_scored` / `assist_scored`
string fields to `telemetry.v2 GoalScored`, or accept that scorer identity lives
on `PlayerGoal`/`PlayerAssist` (slot) plus the header `InitialRoster`
(slot→name) and treat the `LastScore` names as redundant.

## BUG-5 — SessionResponse per-frame fields (19) with no EchoArenaFrame home

Source: `engine.v1 SessionResponse` (`engine_http.pb.go:1898-1974`). Drop site:
`pkg/conversion/mapping.go` `mapFrame` (mapping.go:248-253 builds
`EchoArenaFrame` from only a subset of session fields).

| v1 field (proto #) | type | what v2 would need |
|---|---|---|
| `OrangeTeamRestartRequest` (1) | int32 | restart-request pair on `EchoArenaFrame` |
| `BlueTeamRestartRequest` (15) | int32 | restart-request pair on `EchoArenaFrame` |
| `GameClockDisplay` (3) | string | display string on `EchoArenaFrame` (or derive from `game_clock`) |
| `SessionIp` (5) | string | belongs on `EchoArenaHeader` (session constant), not per-frame |
| `BlueRoundScore` (11) | int32 | per-frame round-score pair on `EchoArenaFrame` |
| `OrangeRoundScore` (17) | int32 | per-frame round-score pair on `EchoArenaFrame` |
| `Possession` (23) | repeated int32 | possession-tracking array on `EchoArenaFrame` |
| `LeftShoulderPressed` (24) | double | analog-input group on `EchoArenaFrame` |
| `RightShoulderPressed` (25) | double | analog-input group on `EchoArenaFrame` |
| `LeftShoulderPressed2` (26) | double | analog-input group on `EchoArenaFrame` |
| `RightShoulderPressed2` (27) | double | analog-input group on `EchoArenaFrame` |
| `RulesChangedBy` (28) | string | rules-change metadata (likely header/event) |
| `RulesChangedAt` (29) | uint64 | rules-change metadata (likely header/event) |
| `PayloadMultiplier` (34) | double | combat/payload sub-message on `EchoArenaFrame` |
| `PayloadCheckpoint` (35) | int32 | combat/payload sub-message on `EchoArenaFrame` |
| `PayloadDistance` (36) | double | combat/payload sub-message on `EchoArenaFrame` |
| `PayloadDefenders` (37) | int32 | combat/payload sub-message on `EchoArenaFrame` |
| `PayloadSpeed` (38) | double | combat/payload sub-message on `EchoArenaFrame` |
| `ErrCode` (40) | int32 | error-code on `EchoArenaFrame` (or drop as transport noise) |

Note: `LastThrow` (field 20) and `LastScore` (field 31) on `SessionResponse` are
not lost — their data flows through `DiscThrown` / `GoalScored` events.

Note: `BlueRoundScore` (field 11), `OrangeRoundScore` (field 17), and
`GameClockDisplay` (field 3) **partially** survive via the v2
`ScoreboardUpdated` event, parallel to the `LastThrow`/`LastScore` note above.
The `ScoreboardSensor` reads all three off the session
(`pkg/events/sensor_scoreboard.go:32-34`) and emits them
(`pkg/events/sensor_scoreboard.go:59-63`); `mapEvent` carries them into the v2
`ScoreboardUpdated` (`pkg/conversion/mapping.go:582-589`). This is **not**
per-frame preservation: the event fires only when a points/round-score value
*changes* (`pkg/events/sensor_scoreboard.go:46-49`), so only score-change
snapshots are kept. `BlueRoundScore`/`OrangeRoundScore` changes are therefore
captured, but the per-frame value of each field on `EchoArenaFrame` is still
lost. `GameClockDisplay` survives least of all — it merely rides along on the
score-change event, so it is sampled only at the instants a score changes, not
on any clock tick. The table rows above stand for the per-frame loss.

## BUG-6 — Team-level fields with no home

Source: `engine.v1 Team` (`engine_http.pb.go` — `TeamName` field 2,
`HasPossession` field 3, `Stats` field 4). Drop site:
`pkg/conversion/mapping.go` `mapPlayers` (mapping.go:391-452) flattens teams to
a single `players` slice and never reads the `Team` struct's own fields;
`EchoArenaFrame` has no team container at all.

| v1 field (proto #) | type | what v2 would need |
|---|---|---|
| `TeamName` (2) | string | team name on `EchoArenaHeader` (constant) or a per-frame `TeamState` |
| `HasPossession` (3) | bool | per-frame `TeamState` (partially derivable from `disc_holder_slot`) |
| `Stats` (4) `TeamStats` | message | per-frame `TeamState.stats` on `EchoArenaFrame` |

## BUG-7 — Per-frame TeamMember fields with no PlayerState home

Source: `engine.v1 TeamMember` (`engine_http.pb.go`). Drop site:
`pkg/conversion/mapping.go` `mapPlayers` (mapping.go:391-452). Verified v2
`PlayerState` fields (echo_arena.pb.go): `slot, head, body, left_hand,
right_hand, velocity, flags, ping` only.

Genuinely lost per-frame data (no v2 home):

| v1 field (proto #) | type | what v2 would need |
|---|---|---|
| `Weapon` (1) | string | combat loadout on `PlayerState` |
| `Ordnance` (2) | string | combat loadout on `PlayerState` |
| `TacMod` (3) | string | combat loadout on `PlayerState` |
| `PacketLossRatio` (13) | double | network stat on `PlayerState` |
| `LeftHoldingOnto` (16) | string | grab-state pair on `PlayerState` |
| `RightHoldingOnto` (17) | string | grab-state pair on `PlayerState` |
| `Stats` (24) `PlayerStats` | message | per-frame `PlayerState.stats` (cumulative stats also flow through `Player*` stat events) |

Session-constant fields on `TeamMember` are **not lost** once BUG-4/BUG-8 are
in: `DisplayName` (8), `AccountNumber` (7), `JerseyNumber` (10), `Level` (11)
are now captured in `EchoArenaHeader.InitialRoster` (BUG-4) and on the
`PlayerJoined` event (BUG-8). Per-frame repetition is unnecessary.
