# Fable Audit — tape + telemetry/v2 Design Oracle

*Maintained by the Fable design twin (long-running). First full pass: 2026-07-11.*
*Role: design + mechanical implementation instructions only. Code edits are made by a
separate implementation twin from instructions written here.*

**Corpus loaded (every line read unless noted):** all of `~/src/tape` (source, tests,
justfile, docs, hooks, CI configs, `.claude/` primers, testdata README; the two big
fixtures spot-checked only — headers, first line, zip structure, byte counts) and all of
`~/src/nevr-proto` (every `.proto` incl. archive/, broadcaster/, gameservice/, engine/,
telemetry v1+v2, spatial/v1; README/CHANGELOG/v2 README; `.sisyphus` restructure plan +
longevity draft). Evidence discipline: **CONFIRMED** = read the code / ran the command;
**SPECULATIVE** is labeled as such.

**Locked design decisions (from Spritz, 2026-07-11) — all guidance below conforms:**

1. Concept name = "dense view"; code names = `Hydrate` / `Hydrator` / hydrating.
2. Sparse/dense marker = `frame_encoding` enum (`SPARSE|DENSE`), provenance-neutral.
   NOT `capture_kind`.
3. Dense payload = `EchoArenaFrameDense`, a NEW arm in `Frame.payload` oneof
   (`echo_arena_dense = 11`). `Frame` is the game-agnostic transport wrapper and is
   NOT renamed.
4. SemVer, additive, non-breaking. Proto package stays `telemetry.v2`. Keep
   `format_version` (uint32, field 3) as MAJOR=2; ADD `format_minor` / `format_patch`
   at new field numbers. This work = 2.0.0 → 2.1.0. Never retype/renumber existing
   fields; no `float`→`double` on `spatial.v1.Vec3` (precision is float32-origin).
5. CORE INVARIANT: sparse and dense are two ENCODINGS of the SAME complete
   information. Sparse must become information-complete (per-player/team stats, team
   names, raw orientation basis PER-FRAME, rules-changed/restart/err) so that
   sparse↔dense transcodes losslessly in either direction. The difference is
   MATERIALIZATION, not content.

---

## 1. Complete proto map — telemetry/v2 (+ spatial/v1), with produce/consume sites

Proto sources: `~/src/nevr-proto/telemetry/v2/capture.proto`,
`~/src/nevr-proto/telemetry/v2/echo_arena.proto`, `~/src/nevr-proto/spatial/v1/types.proto`.
Go types consumed via BSR module `buf.build/gen/go/echotools/nevr-api/protocolbuffers/go`
pinned at `v1.36.11-20260629074123-c89ff774a767.1` (tape `go.mod:11`). The generated Go
uses the opaque/hybrid API in places (`ea.HasDiscHolderSlot()` at `cmd/tapedeck/stats.go:120`,
`frame.SetTimestampOffsetMs` at `cmd/tapedeck/trim.go:112`) — implementers must use
setters/hazzers where the generated API requires them.

### 1.1 capture.proto (game-agnostic transport)

| Message.field (#) | Meaning | Produced | Consumed |
|---|---|---|---|
| `Envelope.message` oneof: `header=1`, `frame=2`, `footer=3` | Stream record | `codec.Writer.WriteHeader/WriteFrame/Close` (`pkg/codec/tape.go:73-158`) | `codec.Reader.readEnvelope` → `ReadHeader/ReadFrame/ReadFooter` (`tape.go:299-354`) |
| `CaptureHeader.capture_id` (1) | Capture UUID/name | `MapHeader` from v1 header (`pkg/conversion/mapping.go:89`); echoreplay import synthesizes `"echoreplay-import"` (`convert.go:88`) | `tapedeck show` (`show.go:64`), `diff` |
| `CaptureHeader.created_at` (2) | Base time; frame offsets are deltas from it | `mapping.go:90`; base-time fallback to first frame ts when nil/zero (`convert.go:141-151`) | `SessionReconstructor.baseTime` (`reconstruct.go:183,199`) |
| `CaptureHeader.format_version` (3) | Format MAJOR, hardcoded 2 | `mapping.go:91` | asserted by tests (`convert_test.go:120`); shown by `show` |
| `CaptureHeader.metadata` (4) | free-form map | copied from v1 metadata (`mapping.go:92`) | `show.go:69`; `MapHeader` also *reads* it to seed EchoArenaHeader (`mapping.go:96-114`) |
| `CaptureHeader.game_header` oneof: `echo_arena=10` | Game-specific header | `MapHeaderFromSession` (`mapping.go:121-164`) | `Session.replay` roster seed (`session.go:148-150`), reconstructor (`reconstruct.go:216-222`), `stats.go:75-83` |
| `Frame.frame_index` (1) | 0-based ordinal | copied from v1 (`mapping.go:322`); echoreplay reader assigns sequential (`echoreplay.go:516`); `trim` rewrites (`trim.go:113`) | `show`, `replay` status |
| `Frame.timestamp_offset_ms` (2) | ms since created_at; uint32 (~49.7 day max) | `mapping.go:316-324` (negative clamped to 0) | replay pacing (`replay.go:108-113`), stats possession time (`stats.go:112-125`), trim range |
| `Frame.payload` oneof: `echo_arena=10` | Per-frame game payload | `mapping.go:400` | everything (`GetEchoArena()` throughout) |
| `CaptureFooter.frame_count` (1) / `duration_ms` (2) / `footer_offset` (3) | summary; offsets are **uncompressed-stream** offsets | `Writer.Close` (`tape.go:115-141`) | `show` summary; `convert_test.go:137-143` |
| `CaptureFooter.keyframe_index` (4) `KeyframeEntry{frame_index,byte_offset}` | seek index every `keyframeInterval` (default 100, `tape.go:17`) | `Writer.WriteFrame` (`tape.go:84-90`) | **no production reader uses it** (browser doc only) |
| `CaptureFooter.event_index` (5) `EventIndexEntry{event_type, frame_indices}` | event-type → frames | `Writer.WriteFrame`/`classifyEvent` (`tape.go:92-99,192-245`) | **no production reader uses it** |
| `EventType` enum (0-50) | index key | `classifyEvent` | none in tape |

Notes (CONFIRMED):

- The footer's keyframe/event indices use the **writer's own counter** (`w.frameCount`,
  `tape.go:82`) as the frame index, not `frame.GetFrameIndex()`. For echoreplay-origin
  captures these coincide; for a legacy nevrcap whose stored `frame_index` values have
  gaps, the footer indexes writer ordinals while frames carry different `frame_index`
  values. See finding F-13.
- `LoadoutChanged`/`GrabChanged` have **no `EventType`** enum entries and are therefore
  invisible to the event index (`classifyEvent` returns UNSPECIFIED for them —
  `tape.go:192-244` has no case; enum in `capture.proto:121-159` has no values for them).
  See finding F-6.

### 1.2 echo_arena.proto — header + frame

**EchoArenaHeader** (session constants, written once):
`session_id(1) map_name(2) match_type(3) client_name(4) private_match(5)
tournament_match(6) total_round_count(7) initial_roster(8) skeleton(9) session_ip(10)`.
Produced by `MapHeaderFromSession` (`mapping.go:121-164`) — metadata-seeded fields
only filled if empty; roster from `buildInitialRoster` (`mapping.go:169-184`) with role
resolution through the single source of truth `events.ClassifyRole`
(`pkg/events/role.go:24-36`, locked by `role_unify_test.go`). Consumed by
`Session.replay` (roster seed), `SessionReconstructor.reconstructSession`
(`reconstruct.go:214-222`), `tapedeck stats` (seed players). `skeleton` (SkeletonLayout)
is **never produced or consumed** in tape — bones byte layout is implicit 22×3×f32 /
22×4×f32 (doc only). `PlayerInfo{slot,account_number,display_name,role,jersey_number,level}`
— note jersey/level are labeled "session constant" in the proto but alienq proved they
vary per-frame (BUGS.md:84-86); the roster stores a join-time snapshot.

**EchoArenaFrame** (per-frame):

| Field (#) | Produced (mapping.go) | Consumed |
|---|---|---|
| `game_status` (1) | `gameStatusMap[session.game_status]` (:333) — unknown strings → UNSPECIFIED (lossy; F-9) | `show`, reverse map `reconstruct.go:36,229` |
| `game_clock` (2) float | `float32(session.game_clock)` (:334) | reconstruct :230, show |
| `pause_state` (3) | `pauseStateMap[pause.paused_state]` (:340-344); `paused_requested`→PAUSED narrowing (:46-48) | reverse map `reconstruct.go:47-52,248` |
| `disc` (4) DiscState{pose,velocity,bounce_count} | `mapDisc` (:404-427); basis→quat via `quaternionFromAxes` (:449-493) | `reconstructDisc` (`reconstruct.go:361-370`) |
| `players` (5) PlayerState[] | `mapPlayers` (:495-558) flattens teams in order | `reconstructTeams/Member` (`reconstruct.go:267-349`) |
| `player_bones` (6) PlayerBones[] | `mapPlayerBones` (:599-622), f32→LE bytes (:625-631) | `reconstructBones` (`reconstruct.go:413-427`); replay `/player_bones` |
| `disc_holder_slot` (7) optional | last player with `has_possession` (:354-362 — **no break**; F-4) | possession[] rebuild (`reconstruct.go:381-408`), stats possession time (`stats.go:116-124`) |
| `vr_root` (8) Pose | `mapPlayerRoot` (:365-367,586-597) | `reconstructPlayerRoot` (:372-375) |
| `blue_points` (9) / `orange_points` (10) | :335-336 | reconstruct :231-232; show/diff |
| `round_number` (11) | FrameMapper state from RoundStarted events (:223-234, 378) — **derived, not source data** | verify round monotonicity (`verify.go:602-630`) |
| `events` (12) EchoEvent[] | `mapEvents` (:634-646) + appended Loadout/Grab deltas (:242-303, sorted by slot) | Session.replay, Writer event index, stats, verify |
| `payload` (13) optional PayloadState | populated only when any payload field non-zero (:388-398) | reconstruct: **not reconstructed** — `reconstructSession` never reads `ea.GetPayload()` (F-5) |
| `left/right_shoulder_pressed(_2)` (14-17) float | :382-385 (capture-client analog input) | reconstruct :233-236 |

**PlayerState** `slot(1) head(2) body(3) left_hand(4) right_hand(5) velocity(6) flags(7)
ping(8) packet_loss_ratio(9)`. Flags bits (proto comment `echo_arena.proto:201-207`):
0 stunned, 1 invulnerable, 2 blocking, 3 possession, 4 is_emote_playing, "5-31 reserved".
**Reality: bits 5-6 carry the team index (0 blue / 1 orange / 2 spectator)** — packed at
`mapping.go:550-552`, decoded at `reconstruct.go:18-30,276`. The proto comment is wrong
on disk today (F-1).

**DiscState** `pose(1) velocity(2) bounce_count(3)` — bounce_count int32→uint32 cast
(`mapping.go:406`) and back (`reconstruct.go:363`).

**PayloadState** `multiplier(1) checkpoint(2) distance(3) defenders(4) speed(5)` —
Echo Combat only; presence-gated (`mapping.go:388-398`).

**PlayerBones** `slot(1) transforms(2) orientations(3) skeleton_override(4)` — bytes are
packed LE float32 (`float32SliceToBytes` `mapping.go:625-631` /
`bytesToFloat32Slice` `reconstruct.go:115-125`); exact both ways (bones are f32 at the
engine). `skeleton_override` never produced/consumed.

### 1.3 echo_arena.proto — events (EchoEvent oneof, fields 10-60)

For each: v1 source event (`telemetry/v1/telemetry.proto`), sensor that detects it
(`pkg/events/`), v1→v2 map site (`mapping.go mapEvent :651-865`), and reader.

| v2 event (field) | Sensor (emit site) | mapEvent case | Read by |
|---|---|---|---|
| `RoundStarted{round_number}` (10) | `RoundStartSensor` (`sensor_gamestate.go:33-65`; round# = blueRound+orangeRound+1, first-frame mid-match handled) | :659-664 | FrameMapper round_number (:226-229); verify |
| `RoundPaused{pause_state, requesting_team, pause_timer}` (11) | `PauseSensor` (`sensor_gamestate.go:85-135`) — v1 event carries full engine `PauseState` clone | :665-668 — **maps to EMPTY message; all three v2 fields exist but are never populated** (F-2) | — |
| `RoundUnpaused{pause_state}` (12) | same sensor | :669-672 — same drop | — |
| `RoundEnded{round_number, winning_team}` (13) | `RoundEndSensor` (`sensor_gamestate.go:161-212`; fires on round_over transition OR round-score change) | :673-679 | verify |
| `MatchEnded{winning_team}` (14) | `MatchEndSensor` (`sensor_gamestate.go:233-263`; winner by points, tie→UNSPECIFIED) | :680-685 | — |
| `ScoreboardUpdated{blue_points, orange_points, blue_round_score, orange_round_score, game_clock_display}` (15) | `ScoreboardSensor` (`sensor_scoreboard.go:24-70`) — **seeds baseline silently at frame 0, no seed event** (F-3) | :686-695 | `Session.replay` ScoreAt (`session.go:225-233`) → reconstruct round scores/clock (`reconstruct.go:241-244`) |
| `PlayerJoined{slot, account_number, display_name, role, jersey_number, level}` (20) | `PlayerJoinSensor` (`sensor_player.go:26-75`; frame-0 roster all "joins"; ascending-slot deterministic order) | :696-709 | Session roster (`session.go:161-174`), stats, verify |
| `PlayerLeft{player_slot, display_name}` (21) | `PlayerLeaveSensor` (:99-146) | :710-716 | Session roster delete (`session.go:175-186`) |
| `PlayerSwitchedTeam{player_slot, new_role, prev_role}` (22) | `PlayerTeamSwitchSensor` (:170-224) | :717-724 | Session roster role update (`session.go:187-203`); stats team |
| `EmotePlayed{player_slot, emote}` (23) | `EmoteSensor` (:248-300) — **always PRIMARY** (F-11) | :725-731 | — |
| `LoadoutChanged{player_slot, weapon, ordnance, tac_mod}` (24) | **not a sensor** — `FrameMapper.appendLoadoutGrabEvents` (`mapping.go:242-303`), seeded at first sighting per slot | (produced directly in v2) | Session LoadoutAt (`session.go:204-214`) → reconstruct |
| `GrabChanged{player_slot, left_holding, right_holding}` (25) | same site | (direct) | Session GrabAt (`session.go:215-224`) → reconstruct |
| `DiscPossessionChanged{player_slot?, previous_player_slot?}` (30) | `DiscPossessionSensor` (`sensor_disc.go:23-50`; −1 sentinel = free) | :732-740 — **always sets both optionals, converting the −1 sentinel into a *present* −1**; proto contract says "absent when free" (F-4) | footer event index only |
| `DiscThrown{player_slot, throw_details}` (31) | `DiscThrownSensor` (`sensor_disc.go:72-114`; thrower = pre-throw possessor; **stale `last_throw` at frame 0 emits a spurious event, possibly slot −1**, F-7) | :741-764 (13 ThrowDetails floats f64→f32) | stats throws (`stats.go:166-167`) |
| `DiscCaught{player_slot}` (32) | `DiscCaughtSensor` (:136-163) | :765-770 | stats catches |
| `GoalScored{disc_speed, team, goal_type, point_amount, distance_thrown, scorer_slot, assist_slot?}` (40) | `GoalScoredSensor` (`sensor_scoreboard.go:92-116`; **stale `last_score` at frame 0 emits a spurious goal**, F-7) | :771-785 — **scorer_slot/assist_slot left zero/nil; v1 names dropped** (F-8; scorer_slot=0 is ambiguous with real slot 0) | verify |
| `PlayerGoal{player_slot,total_goals,points}` (41) | `StatEventSensor` (`sensor_stats.go:142-167`; splits multi-goal deltas, remainder on first) | :786-793 | stats |
| `PlayerSave/Stun/Pass/Steal/Block/Interception/Assist/ShotTaken` (50-57) | `StatEventSensor` (:169-281; steal victim = previous possessor, −1 sentinel → **present** −1 optional in v2, same F-4 shape at :815-823) | :794-851 | stats |
| `GenericEvent{event_type,data,payload}` (60) | none (passthrough) | :852-859 | — |

**Event-emission order per frame (CONFIRMED):** sensors run in `DefaultSensors()` order
(`pkg/events/sensors.go:4-30`: joins, leaves, switches, emotes, scoreboard, goals, disc
possession/thrown/caught, stats, round start/end, match end, pause), each drained to
exhaustion (`events.go:337-361`); then FrameMapper appends Loadout/Grab deltas in
ascending-slot order. This total order is what the golden test locks byte-for-byte.

### 1.4 spatial/v1

`Vec3{x,y,z}` float32; `Quat{x,y,z,w}` float32; `Pose{position, orientation}`.
Produced only via `poseFromVectors`/`quaternionFromAxes` (`mapping.go:431-493`);
consumed via `poseToVectors`/`quaternionToAxes` (`reconstruct.go:69-111`). The
quaternion is exact for a true rotation but **cannot represent a non-orthonormal or
degenerate basis** — the round-trip returns the orthonormalized basis
(`roundtrip_v2_test.go:26-33`; measured residual sets `orientTol = 5e-3` there; the
coordinator's measured worst case on degenerate frames ≈ 15.6°). This is the driver for
carrying the raw basis per-frame in sparse (locked decision 5).
Also used by `broadcaster/v1/r15net.proto` (`SR15NetPhysicsSimStateEvent` etc.) — any
change to spatial types has blast radius beyond telemetry; **add new messages, never
alter Vec3/Quat/Pose fields.**

### 1.5 The v1 lane (context for conversion)

- `telemetry/v1/telemetry.proto`: `Envelope{header|frame}`, `TelemetryHeader`,
  `LobbySessionStateFrame{frame_index, timestamp, events[], session, player_bones}` —
  the **full** `engine.v1.SessionResponse` is embedded, which is why v1/echoreplay is
  lossless (`TestEchoReplayRoundTripFidelity`, 1023 frames, 0 diffs).
- `engine/v1/engine_http.proto`: `SessionResponse` (40 fields), `Team{players,
  team_name, has_possession, stats}`, `TeamMember` (24 fields incl. stats), `PlayerStats`
  (12 fields incl. `possession_time` double + `points`/`catches`), `Disc`, `BodyPart`
  (position/forward/left/up f64 triads), `HandPart` (`pos` json_name quirk), `LastScore`
  (has `person_scored`/`assist_scored` display names), `LastThrowInfo` (13 f64),
  `PauseState` (5 fields), `PlayerRoot`, `UserBones{bone_t[], player_index, bone_o[]}`.
- Codecs: `EchoReplay` (zip of `ts\tsessionJSON[\tbonesJSON]` lines; Spark-format
  boolean coercion `FixBooleanInt32Fields`, uint64 unquoting, exponent flattening),
  `LegacyReader` (zstd varint-delimited v1 protos).
- Rest of nevr-proto (broadcaster, gameservice, archive) is not consumed by tape;
  loaded for the later stream/nevr-agent/nevr-runtime phases. Import chain relevant to
  us: telemetry/v2 ← spatial/v1 only (clean break, no v1 import); gameservice/v1 ←
  telemetry/v1 (`LobbySessionStateMessage.session_state`, `gameservice.proto:460-467`).

---

## 2. Design check

**Architecture (CONFIRMED clean):**

- Layering is strict and right: `Envelope` (stream record) → `Frame` (game-agnostic
  index+timestamp) → `EchoArenaFrame` (game payload) → `PlayerState`/`DiscState`/
  `EchoEvent`. Adding `echo_arena_dense = 11` to `Frame.payload` fits the design's own
  extension contract (`capture.proto:70-74` reserves 11-99 for game payloads;
  telemetry/v2/README.md documents oneof extension as the intended mechanism).
- The event-sourced sparse model is coherent: session-constants → header; rare changes →
  events (join/leave/loadout/grab replayed by `conversion.Session`, clone-on-write maps,
  O(1) per-frame lookups — `session.go:143-242`); per-frame data → frame fields. The
  reader-side `Session` and writer-side `FrameMapper` are honest mirror images, and the
  leave-does-not-clear-loadout/grab subtlety is documented and locked by test on both
  sides (`session.go:175-186`, `TestSessionLoadoutGrabPersistAcrossLeave`).
- Codec hardening is genuinely good: SEC-001/SEC-002 budgets are documented,
  configurable, belt-and-braces enforced at decoder AND accumulation sites
  (`pkg/codec/limits.go`, guards in `OpenSession`/`NewSessionReconstructor`/`ReadFrames`),
  fuzzed (`envelope_fuzz_test.go`, `echoreplay_fuzz_test.go`), with error sentinels.
- Determinism is a first-class property (golden byte-hash gate + ×2 determinism test +
  slot-ordered sensor emission). This is the anticheat-record posture and must be
  preserved by all new work.

**Fragile / debt (CONFIRMED):**

- **"Self-contained for random access" is only half-true.** Kinematics and scores are
  per-frame, but identity/loadout/grab/round-scores are event-sourced — a reader seeking
  via `keyframe_index` has no roster without replaying events from frame 0. The keyframe
  index is currently consumed by nothing in tape. The dense encoding is precisely the fix
  (a dense frame IS self-contained); the sparse docs should stop overclaiming.
- **In-memory whole-capture model.** `Session`, `SessionReconstructor`, `diff`, and the
  EchoReplay **writer** (buffers every frame in `frameBuffer` until `Finalize`,
  `echoreplay.go:193-201,461-490`) all hold the whole capture. ~200 MB for the 22.7k
  alienq lane. Acceptable now; the Hydrator must NOT copy this pattern — design it as a
  streaming state-carrier (`Next()` / iterator), with an optional convenience that
  materializes.
- The float64 mid-layer is a historical artifact: engine emits float32-precision values,
  v1 stores f64, v2 stores f32. The narrowing is micrometers (float32-origin), benign —
  format-design.md §2's "femtometer" framing is wrong in magnitude but right in verdict.
  **Do not widen Vec3** (locked decision 4).
- `pkg/events` AsyncDetector's ring buffer (10 frames, `getFrame`) is vestigial — only
  `lastFrame()` is used in production; the drop-on-full channels (frames `events.go:190-192`,
  event batches `:219-223,:270-274`) make the async path silently lossy (= GH #18).
  Conversion is safe only because it uses sync mode + drain-per-frame
  (`convert.go:175,260-269`, proven by `TestSyncDetector_ConversionPatternNoEventLoss`).

---

## 3. Bug hunt (cited; each with a concrete failure scenario)

Severity: H = corrupts/loses data or crashes; M = wrong results in real scenarios;
L = latent/edge. All CONFIRMED unless marked SPECULATIVE.

**F-1 (H, format contract). PlayerState.flags bits 5-6 secretly carry team index.**
`mapping.go:550-552` packs `teamIdx&0x3 << 5`; `reconstruct.go:18-30,267-298` depends on
it. `echo_arena.proto:206-207` says "bits 5-31: reserved". Failure: any third-party
consumer (browser doc, Demo-Viewer) masking flags per the proto sees spectators with a
mystery bit set, and any future writer that leaves bits 5-6 zero silently breaks
`reconstructTeams` (all players collapse into team 0 = blue). Fix for 2.1: document bits
5-6 in the proto comment as team index (additive doc change), and give
`EchoArenaFrameDense.PlayerStateDense` an explicit `team_index`/role field instead of
bit-packing.

**F-2 (H, wire-up loss). RoundPaused/RoundUnpaused mapped to empty messages.**
v1 event carries a full engine `PauseState` clone (`sensor_gamestate.go:112-117,123-129`;
`telemetry.proto:129-136`), and v2 `RoundPaused` has `pause_state(1) requesting_team(2)
pause_timer(3)` (`echo_arena.proto:292-298`) — but `mapEvent` constructs `&RoundPaused{}`
/ `&RoundUnpaused{}` with zero fields (`mapping.go:665-672`). Failure: a tape of a match
with pauses has pause events that say nothing; reconstructor cannot recover
`paused_requested_team`/timers (BUGS.md lists pause sub-state as unrecoverable — but
this slice of it is recoverable TODAY with a pure wire-up: enum from `pauseStateMap`,
requesting team from `paused_requested_team` via `teamStringToRole`, timer from
`paused_timer`).

**F-3 (H for dense correctness). Score sensor seeds frame 0 silently — pre-first-change
round scores / clock display are unrepresentable.** `sensor_scoreboard.go:36-43`
(also `RoundEndSensor` `sensor_gamestate.go:171-177`). `Session.ScoreAt` therefore
returns zero-values until the first change (`session.go:27-38` documents it; BUGS.md:81-83
calls it a BUG). Failure: convert a capture that starts at blue_round_score=1 → every
frame before the next score change reconstructs/hydrates round_score=0 and clock "".
Fix (required before Hydrator ships): emit a seed `ScoreboardUpdated` on the first
frame, exactly like Loadout/Grab seeding (`mapping.go:242-303` is the pattern). This
changes golden bytes → regenerate golden in the same commit.

**F-4 (M, sentinel vs optional mismatch).** v1 uses −1 sentinels; v2 uses `optional`
with documented "absent = free/unknown" (`echo_arena.proto:377-381,447-448`). `mapEvent`
always sets the pointers: `DiscPossessionChanged` (`mapping.go:732-740`) and
`PlayerSteal.victim_player_slot` (`mapping.go:815-823`) store a **present −1**. Related:
`disc_holder_slot` is chosen by a loop that doesn't break (`mapping.go:354-362`) — if the
engine ever flags two players `has_possession` in one frame, the *last* wins while the
sensors (`findPossessorSlot`, `sensor_disc.go:173-182`) pick the *first* — frame state and
events can disagree in the same frame. Failure: `tapedeck stats` creates a phantom
"slot −1" row when a DiscThrown with thrower −1 arrives (`stats.go:166-167` +
`ensurePlayer`); consumers honoring the proto's absence contract misread free-disc
transitions. Fix: map −1 → nil pointer at the three sites; pick first possessor (or
document last-wins).

**F-5 (M, asymmetry). PayloadState not reconstructed.** Forward mapping populates
`ea.Payload` (`mapping.go:388-398`) but `reconstructSession` never reads it —
`payload_*` fields of the rebuilt `SessionResponse` stay zero (`reconstruct.go:213-261`
has no payload section). Failure: echoreplay→v2→echoreplay on an Echo Combat capture
silently drops all payload telemetry even though v2 stored it. (The round-trip BAC
doesn't catch it because `measureFindings` doesn't compare payload fields either —
`roundtrip_v2_test.go:308-330`.) Pure wire-up fix + add payload fields to the tally.

**F-6 (M, index blindness). LoadoutChanged/GrabChanged missing from EventType.**
`capture.proto:121-159` has no values; `classifyEvent` (`tape.go:192-244`) returns
UNSPECIFIED → they're never indexed in `CaptureFooter.event_index`. Failure: a future
consumer using the event index to find loadout changes finds none, though the frames
contain them. Fix for 2.1: add `EVENT_TYPE_LOADOUT_CHANGED = 14`... careful — game-state
range is 1-6, player events 10-13 used; add `EVENT_TYPE_LOADOUT_CHANGED = 14` and
`EVENT_TYPE_GRAB_CHANGED = 15`? No: 14/15 sit in the "player events 10-..." decade;
allocate the next free numbers in the player decade: 14 and 15 are free (player decade
is 10-19; used 10,11,12,13) → `EVENT_TYPE_LOADOUT_CHANGED = 14`,
`EVENT_TYPE_GRAB_CHANGED = 15`, plus `classifyEvent` cases. Additive.

**F-7 (M, spurious frame-0 events).** `last_throw` and `last_score` persist in the
engine session across time; at recording start the sensors treat any present value as
new: `DiscThrownSensor` (`sensor_disc.go:93-110`; thrower may be −1 or the *current*
possessor, both wrong) and `GoalScoredSensor` (`sensor_scoreboard.go:104-113`) emit at
frame 0/1 for a throw/goal that predates the capture. Failure: a mid-match capture
starts with a GoalScored event; stats/anticheat count a goal that isn't in the capture;
`verify.go` is blind to it because `collectGroundTruth` counts the same stale value as a
"change" (`verify.go:220-247`). Semantics call for Andrew: either (a) suppress
first-sighting emission (initialize baseline like stat sensors do) — changes goldens and
loses "the last throw before capture started", or (b) keep and mark: with the dense/
carry-forward design, `last_throw`/`last_score` become per-frame carried state anyway,
which makes (b) coherent: the frame-0 event correctly seeds carried state (mirrors
Loadout/Grab seeding). Recommend (b) + document.

**F-8 (M, identity gap — blocks the core invariant). GoalScored scorer identity.**
`scorer_slot`/`assist_slot` never populated (`mapping.go:771-785`); v1 names
(`LastScore.person_scored/assist_scored`) dropped; and `scorer_slot == 0` is
indistinguishable from "player in slot 0" (non-optional int32). Real scorer slots ride
separately on `PlayerGoal`/`PlayerAssist` stat events (SCHEMA-GAPS BUG-2/3 note), but a
dense frame's `last_score` cannot reproduce the engine's display-name strings from
slots the converter never resolved. Under locked decision 5, sparse must carry this:
**add `person_scored = 8` and `assist_scored = 9` (strings) to v2 `GoalScored`**
(additive), wire them in `mapEvent`, and have the Hydrator carry the whole event into
dense `last_score`.

**F-9 (M, out-of-vocabulary strings collapse).** `gameStatusMap`/`matchTypeMap`/
`pauseStateMap`/`goalTypeStringMap` lookups default to UNSPECIFIED on any unknown string
(`mapping.go:18-80,333,340-344`), and the reverse maps render UNSPECIFIED as ""
(`reconstruct.go:33-52`). Failure: a social-lobby capture with `game_status:"lobby"`
(the exact string used in codec tests, `echoreplay_test.go:186-187`) round-trips to
`""`; an unrecognized match_type string is erased. For an archival format this is silent
vocabulary loss. Options for 2.1 (Andrew's call): extend enums (additive) as strings are
discovered, and/or add a raw-string escape field to the dense frame; at minimum the
converter should WARN on unknown strings.

**F-10 (M, mid-stream truncation is silent).** `Reader.ReadFrame` returns `io.EOF` for
ANY non-frame envelope (`tape.go:314-335`): an empty `Envelope{}` or a second header in
the middle of a stream ends reading with no error; every consumer then reports a
"successful" short read. Failure: a truncated-then-concatenated or maliciously crafted
tape reads as a clean shorter capture; `convert`'s own `verifyOutput` wouldn't notice
(it counts its own reads). Fix: distinguish "footer seen" (EOF) from "unexpected
envelope kind" (error), and have consumers cross-check `footer.frame_count`.

**F-11 (L). EmoteSensor always emits EMOTE_TYPE_PRIMARY** (`sensor_player.go:272-277`) —
secondary emotes indistinguishable. Source data (`is_emote_playing` bool) can't
distinguish either, so this is a source limitation; the enum overstates knowledge.

**F-12 (L, robustness).** Echoreplay scanner cap: a single line > 10 MiB aborts the whole
read with `scanner error: token too long` (`echoreplay.go:182-186,521-523`) rather than
skipping the line as other parse errors do (`:504-507`). A legit future capture with many
players + bones could conceivably cross 10 MiB. Also `initScanner` falls back to *any*
`.echoreplay`-suffixed member; the committed sample's member name is a full absolute path
(`home/andrew/Downloads/viewer/rec_2024-10-20_19-20-09.echoreplay` — CONFIRMED via zip
listing), so basename matching fails and only the extension fallback saves it. The
testdata generator must reproduce both member-naming shapes.

**F-13 (L).** Footer keyframe/event indexes use writer ordinals, not `Frame.frame_index`
(`tape.go:82-99`) — divergence possible for legacy inputs with non-sequential indices;
harmless today because nothing consumes the indexes (see F-6/§2).

**F-14 (L, concurrency).** Sync-mode `Stop()` closes `eventsChan` without waiting for
in-flight `ProcessFrame` (`events.go:142-150`); a concurrent `ProcessFrame` on another
goroutine can then send on a closed channel → panic (`events.go:213-224` — after
`cancel()` the select between a ready send and `ctx.Done()` is random). Conversion is
single-threaded (safe); library users aren't warned.

**F-15 (L, cast edges, SPECULATIVE trigger).** `uint32(member.GetPing())`
(`mapping.go:501`) and `uint32(disc.GetBounceCount())` (`:406`) wrap negative inputs; if
the engine ever emits ping −1 (common "unknown" idiom), v2 stores 4294967295 (renders as
absurd ping in consumers) though the reconstruct cast wraps it back exactly. Cheap
clamp-at-zero (or keep for bit-exact round-trip and document).

**F-16 (L, doc drift — misleads integrators).** (a) `docs/browser-integration.md` invents
fields: `player.pose`, `player.name/team`, `disc.position` — none exist on
`PlayerState`/`DiscState`; its seek example indexes `frames[keyframes[mid].frameIndex]`
assuming ordinal==frame_index. (b) `testdata/README.md` records golden as 1,625,143 bytes
(sha cc060af5…) but the committed file is 1,620,364 bytes (CONFIRMED `ls`) — the golden
was regenerated (superset fields) without updating the README. (c) format-design.md §2
"femtometer noise" — wrong magnitude (it's micrometers; float32-origin), right
conclusion; treat as historical per coordinator. (d) README.md's v1-column "Random
access: No" table is fine, but CLAUDE.md's claim that footer "enables efficient
scanning" is unconsumed capability (see §2).

**Verified-safe things checked and NOT bugs:** `fixStringEncodedNumber` /
`FixBooleanInt32Fields` can't match inside JSON string values (the quote bytes are
escaped in the encoding, so the raw patterns can't occur; `boolean_int32.go:46-56`
documents it); `FixExponentNotation` is string-aware and value-preserving (shortest f64
repr); varint readers cap shift at 64 and length at 256 MiB before the SEC-002-guarded
body read (`tape.go:363-390`, `legacy.go:166-193`); `parseFrameLineTo` proto-Resets
sub-messages to prevent merge bleed (`echoreplay.go:705-717`, locked by
`TestReadFrameTo_NoMergeCorruption`); negative frame-time offsets clamp to 0
(`mapping.go:317-320`); budget readers fail *before* the read when exhausted so
`io.ReadFull` can't mask the error (`limits.go:146-151`).

---

## 4. Improvement suggestions (ranked)

1. **Document flags bits 5-6 in the proto; make team explicit in dense** (F-1) — one
   comment edit in `echo_arena.proto` + explicit field in `PlayerStateDense`. Prevents a
   silent format break by future writers.
2. **Emit ScoreboardUpdated seed at frame 0** (F-3) — prerequisite for a correct
   Hydrator; mirrors Loadout/Grab seeding at `mapping.go:242-303`; regenerate golden.
3. **Wire RoundPaused/RoundUnpaused payloads** (F-2) — pure wire-up, fields already
   exist; also carry the remaining `PauseState` sub-fields into the 2.1 additions
   (`unpaused_team`, `unpaused_timer`, `paused_timer` need homes — extend v2
   `RoundPaused`/`RoundUnpaused` additively: `RoundPaused.paused_timer` exists;
   add `unpaused_timer = 4`? decide with Andrew; the narrowing `paused_requested`→PAUSED
   also deserves its own enum value `PAUSE_STATE_PAUSE_REQUESTED = 5`, additive).
4. **Fix sentinel→optional mapping** (F-4) + first-possessor selection with break.
5. **Reconstruct PayloadState** (F-5) + add payload fields to the round-trip tally.
6. **Add GoalScored name fields + wire** (F-8) — required by the information-complete
   invariant.
7. **Add LoadoutChanged/GrabChanged EventType entries + classifyEvent cases** (F-6).
8. **Harden Reader against mid-stream non-frame envelopes** (F-10) and have `verify`
   cross-check the footer count.
9. **Converter warnings on unknown enum strings** (F-9); extend enums as vocabulary is
   confirmed.
10. **Kill or use the AsyncDetector ring buffer; document sync-Stop contract** (F-14, §2
    debt). The conversion path only needs a plain synchronous detector.
11. **Doc sweep**: fix browser-integration.md field names, testdata/README byte counts,
    format-design.md precision framing (F-16).
12. `tapedeck replay` serves v2-shaped JSON at `/session` while claiming EchoVR API
    emulation (`replay.go:122-143` marshals `EchoArenaFrame`); route it through
    `SessionReconstructor` to serve genuine `SessionResponse` JSON (or re-document).
    With the Hydrator this becomes nearly free.

---

## 5. Workstream implications (design notes for the three tracks; NO code this phase)

### 5.1 Proto additions (nevr-proto, telemetry/v2, 2.0.0 → 2.1.0, all additive)

- `CaptureHeader`: `uint32 format_minor = 5; uint32 format_patch = 6;`
  (field 4 is `metadata`, 10 is the game_header oneof start — 5/6 are free, CONFIRMED
  against `capture.proto:36-54`). Plus
  `FrameEncoding frame_encoding = 7;` with
  `enum FrameEncoding { FRAME_ENCODING_UNSPECIFIED = 0; FRAME_ENCODING_SPARSE = 1;
  FRAME_ENCODING_DENSE = 2; }` — UNSPECIFIED must read as SPARSE for all existing files
  (zero value on old tapes).
- `Frame.payload`: `EchoArenaFrameDense echo_arena_dense = 11;`
- `EchoArenaFrameDense`: every-field-materialized frame — per-frame identity
  (slot+account+name+role+jersey+level), loadout, grab, team name + explicit team index,
  per-player stats (12 PlayerStats fields), per-team stats, running `last_throw` /
  `last_score` (with names, F-8), possession, round scores + clock display, pause
  sub-state, restart requests, rules_changed_by/at, err_code, payload state, and the
  raw orientation basis alongside quaternions.
- Sparse information-completeness additions (locked decision 5): raw basis PER-FRAME in
  sparse (new message, e.g. `spatial.v1`-adjacent `Basis{Vec3 forward, left, up}` —
  prefer a NEW message + optional fields on the *frame/player/disc* level rather than
  touching `Pose`, because `Pose` is shared with broadcaster/v1 — §1.4); per-player/team
  stats (event-sampled `StatsUpdated` won't cover `possession_time` which varies every
  frame — needs per-frame or accepted sampling; decision to surface to Andrew); team
  names; rules_changed/restart/err homes. Exact field numbering to be specified in the
  implementation instructions once Andrew approves the shape.

### 5.2 Hydrator (in tape, pkg/conversion or new pkg/hydrate)

- `conversion.Session.replay()` (`session.go:147-242`) already implements 80% of the
  carry-forward semantics (roster join-once identity, loadout/grab, event-sampled
  scores). The Hydrator generalizes it: walk frames once, carry running state, emit a
  fully-populated view per frame. Must ALSO carry: `last_throw` (from DiscThrown
  ThrowDetails), `last_score` (from GoalScored — needs F-8), possession (prefer explicit
  `disc_holder_slot`, fall back to flags bit 3 — reuse `discHolder`,
  `reconstruct.go:398-408`), pause sub-state (needs F-2), round scores/clock (needs
  F-3 seed). Design streaming (no whole-capture slice) — see §2 debt.
- `SessionReconstructor` (`reconstruct.go:142-209`) is the proof that hydration
  semantics already round-trip: the recoverable lane is exact (BUGS.md progress notes,
  `TestRoundTripBAC`). The Hydrator and the reconstructor should share the state-carrier
  so the dense writer, the v1 reconstructor, and `tapedeck replay` all sit on one engine.
- Bidirectional transcode: dense→sparse = re-derive events by diffing consecutive dense
  frames (exactly what the sensors do over v1 frames today — the sensor suite is the
  reference implementation of "what is an event").

### 5.3 Mechanical testdata

- Replace the 3.6 MB fixtures with a seeded synthesizer producing echoreplay input in
  code. Building blocks already exist: `richV1Frame` (`pkg/conversion/bench_test.go:20-131`),
  `realisticFrameSequence` (`pkg/events/event_detection_bench_test.go:16-197`), the
  property-based generator (`pkg/events/sensor_properties_test.go:26-198`), and
  `createSyntheticEchoreplay` (`pkg/codec/echoreplay_bench_test.go:262-281`). The
  generator must produce **byte-realistic** session JSON (protojson field names per
  `engine_http.proto` json_name annotations, decimal floats, unquoted uint64s, CRLF line
  endings — match `WriteReplayFrame`, `echoreplay.go:238-284`) or simply drive
  `codec.NewEchoReplayWriter` (which already produces the canonical byte shape).
- Named edge scenarios to encode (each ties to a finding): degenerate/non-orthonormal
  basis (F-quat, §1.4), mid-match name/level/jersey change (roster snapshot semantics),
  throw-at-frame-0 + stale last_score (F-7), spectator/third team + jersey −1
  (`ClassifyRole`), empty-team frame (reconstruct order, F-31-class), Spark-format lines
  (boolean restart fields, no bones, no second tab), zip member named with a full path
  AND with basename (F-12), a `Store`d (uncompressed) zip member, unknown
  game_status/match_type strings (F-9), paused_requested narrowing, multiple simultaneous
  joins (determinism), slot reuse after leave.
- Golden strategy: keep `TestConvertDeterministic` (×2 hash) + compute expectations
  in-test (round-trip property assertions per `roundtrip_v2_test.go`) instead of a
  committed multi-MB golden; at most one tiny (~KB) real regression clip. Note the
  current golden regeneration ritual (delete file, run test, commit) and the stale
  README byte-count (F-16b) as things the new scheme eliminates.

---

## 6. Implementer spec — OBVIOUS BATCH (2.1.0, additive, buf-breaking-PACKAGE clean)

Scope rule for this batch: additive only, no Andrew decision, low churn. Every proto edit
is a new field / new enum / comment — no renumber, no retype, no label change → passes
`buf breaking --against PACKAGE`. Field numbers below are **next-free, verified against the
current proto**. All header/proto value changes alter the committed golden bytes; Opus's
regen harness handles the single golden regeneration at the end of the batch.

**Global step order:** (S0) apply all `nevr-proto` edits (items 1,2,6) → `buf build` +
`buf lint` + `buf breaking --against '.git#branch=main'` clean → local `buf generate` +
`replace` (or bump the BSR pin) so tape sees the new getters/setters. (S1) add red tests
(they compile against new getters, assert current behavior fails). (S2) apply converter/
codec wire-ups (items 1,3,5,6). (S3) `go test ./...` green. (S4) regenerate golden +
determinism ×2. Item 4 (doc-only) and the F-10 codec fix (item 5) need no proto regen and
can land first.

### Item 1 — `CaptureHeader.format_minor` + `format_patch` (SemVer groundwork)

**Proto** — `nevr-proto/telemetry/v2/capture.proto`, message `CaptureHeader` (currently
fields 1,2,3,4 then oneof `game_header` starting at `echo_arena = 10`; **5–9 free**).
Insert after `format_version` (line 44):

```proto
  // Core capture format version. For this schema it must be 2.
  uint32 format_version = 3;

  // Format MINOR/PATCH (SemVer). MAJOR is format_version. A reader that
  // understands MAJOR=2 must read any 2.x file; unknown minors add only
  // optional fields. Absent (0) on pre-2.1 captures.
  uint32 format_minor = 5;
  uint32 format_patch = 6;
```

**Converter wire-up** — `pkg/conversion/mapping.go:88-93`, the sole `CaptureHeader`
constructor. Add two fields:

```go
	header := &capturepb.CaptureHeader{
		CaptureId:     v1.GetCaptureId(),
		CreatedAt:     v1.GetCreatedAt(),
		FormatVersion: 2,
		FormatMinor:   1, // this batch ships 2.1.0
		FormatPatch:   0,
		Metadata:      v1.GetMetadata(),
	}
```

(Note: generated API is hybrid; struct-literal construction is used throughout this file
and works for scalar fields — no setter needed here.)

**Red test** (`pkg/conversion/mapping_test.go`, new func):

```go
func TestMapHeader_DeclaresFormat210(t *testing.T) {
	got := MapHeader(&telemetryv1.TelemetryHeader{CaptureId: "x"})
	if got.GetFormatVersion() != 2 || got.GetFormatMinor() != 1 || got.GetFormatPatch() != 0 {
		t.Errorf("format = %d.%d.%d, want 2.1.0",
			got.GetFormatVersion(), got.GetFormatMinor(), got.GetFormatPatch())
	}
}
```

Red today: `GetFormatMinor()` returns 0. Green after wire-up.

### Item 2 — `CaptureHeader.frame_encoding` + `FrameEncoding` enum (zero = SPARSE)

**Proto** — `nevr-proto/telemetry/v2/capture.proto`. Add a top-level enum (place beside
the other top-level enum `EventType`, e.g. after line 159). **buf lint STANDARD forces the
zero value to be `_UNSPECIFIED`** — do NOT make `SPARSE = 0` (it would fail lint). Instead
the *semantic* zero is SPARSE: readers treat UNSPECIFIED as SPARSE, so every pre-2.1 file
(field absent → 0) reads correctly as sparse.

```proto
// How each Frame's game payload is materialized. UNSPECIFIED is read as SPARSE
// (pre-2.1 captures have no field set), so sparse is the safe zero behavior.
// SPARSE = event-sourced (identity/loadout/scores reconstructed by replay);
// DENSE = every field materialized per frame (Frame.payload = echo_arena_dense).
enum FrameEncoding {
  FRAME_ENCODING_UNSPECIFIED = 0;
  FRAME_ENCODING_SPARSE = 1;
  FRAME_ENCODING_DENSE = 2;
}
```

Add the header field (next-free after item 1 uses 5,6):

```proto
  // Materialization of this capture's frames. UNSPECIFIED == SPARSE.
  FrameEncoding frame_encoding = 7;
```

**Converter wire-up** — same `CaptureHeader` literal (`mapping.go:88-93`), set explicitly
so future dense writers set DENSE and old-vs-new is unambiguous:

```go
		FrameEncoding: capturepb.FrameEncoding_FRAME_ENCODING_SPARSE,
```

Reader contract (document; no reader change needed this batch): treat
`FRAME_ENCODING_UNSPECIFIED` and `FRAME_ENCODING_SPARSE` identically. A one-line helper is
optional, not required:
`func IsDense(h *capturepb.CaptureHeader) bool { return h.GetFrameEncoding() == capturepb.FrameEncoding_FRAME_ENCODING_DENSE }`.

**Red test** (`pkg/conversion/mapping_test.go`):

```go
func TestMapHeader_FrameEncodingSparse(t *testing.T) {
	got := MapHeader(&telemetryv1.TelemetryHeader{CaptureId: "x"})
	if got.GetFrameEncoding() != capturepb.FrameEncoding_FRAME_ENCODING_SPARSE {
		t.Errorf("frame_encoding = %v, want SPARSE", got.GetFrameEncoding())
	}
}
```

Red today (getter returns UNSPECIFIED). Green after wire-up. No `buf breaking` risk:
adding an enum + an optional-presence scalar field is purely additive.

### Item 3 — F-2: wire RoundPaused / RoundUnpaused (converter-only, fields already exist)

No proto change. v2 `RoundPaused{pause_state=1, requesting_team=2, pause_timer=3}` and
`RoundUnpaused{pause_state=1}` already exist (`echo_arena.proto:292-302`); the v1 events
carry a full `engine.v1.PauseState` clone (sensor sets it at
`sensor_gamestate.go:112-117,123-129`). Only `mapEvent` drops it.

**Wire-up** — replace `pkg/conversion/mapping.go:665-672` (currently builds empty
messages):

```go
	case *telemetryv1.LobbySessionEvent_RoundPaused:
		rp := &capturepb.RoundPaused{}
		if ps := e.RoundPaused.GetPauseState(); ps != nil {
			rp.PauseState = pauseStateMap[ps.GetPausedState()]
			rp.RequestingTeam = teamStringToRole(ps.GetPausedRequestedTeam())
			rp.PauseTimer = float32(ps.GetPausedTimer())
		}
		evt.Event = &capturepb.EchoEvent_RoundPaused{RoundPaused: rp}
	case *telemetryv1.LobbySessionEvent_RoundUnpaused:
		ru := &capturepb.RoundUnpaused{}
		if ps := e.RoundUnpaused.GetPauseState(); ps != nil {
			ru.PauseState = pauseStateMap[ps.GetPausedState()]
		}
		evt.Event = &capturepb.EchoEvent_RoundUnpaused{RoundUnpaused: ru}
```

Both helpers are in-file: `pauseStateMap` (`mapping.go:42-51`, direct index → zero-value
UNSPECIFIED for unknowns, acceptable), `teamStringToRole` (`mapping.go:59-64`).

**Red test** (`pkg/conversion/mapping_test.go`):

```go
func TestMapEvent_RoundPausedCarriesPauseState(t *testing.T) {
	v1e := &telemetryv1.LobbySessionEvent{
		Event: &telemetryv1.LobbySessionEvent_RoundPaused{
			RoundPaused: &telemetryv1.RoundPaused{
				PauseState: &enginev1.PauseState{
					PausedState:         "paused",
					PausedRequestedTeam: "blue",
					PausedTimer:         12.5,
				},
			},
		},
	}
	rp := mapEvent(v1e).GetRoundPaused()
	if rp.GetPauseState() != capturepb.PauseState_PAUSE_STATE_PAUSED {
		t.Errorf("pause_state = %v, want PAUSED", rp.GetPauseState())
	}
	if rp.GetRequestingTeam() != capturepb.Role_ROLE_BLUE_TEAM {
		t.Errorf("requesting_team = %v, want BLUE", rp.GetRequestingTeam())
	}
	if rp.GetPauseTimer() != 12.5 {
		t.Errorf("pause_timer = %v, want 12.5", rp.GetPauseTimer())
	}
}
```

Red today (all zero from `&RoundPaused{}`). Green after wire-up. Golden may shift only if
the sample contains pause events (it likely does not); regen covers it either way.

### Item 4 — F-1: document flags bits 5-6 (DOC-ONLY)

**Proto** — `nevr-proto/telemetry/v2/echo_arena.proto:201-208`. Comments only, no wire
change → `buf breaking` clean. Replace the flags comment:

```proto
  // Packed boolean flags + team index:
  //   bit 0: stunned
  //   bit 1: invulnerable
  //   bit 2: blocking
  //   bit 3: possession
  //   bit 4: is_emote_playing
  //   bits 5-6: team index (0=blue, 1=orange, 2=spectator) — REQUIRED for
  //             team reconstruction; a writer that leaves these zero collapses
  //             all players onto the blue team. See tape mapping.go mapPlayers
  //             / reconstruct.go reconstructTeams.
  //   bits 7-31: reserved
  uint32 flags = 7;
```

No test (comment). **Design note (NOT specced this batch):** the bit-packed team index is
fragile — a future minor should give the dense `PlayerStateDense` an explicit
`team_index` (or `Role`) field, and consider promoting it to a first-class field on the
sparse `PlayerState` too. Flag for Andrew; do not implement now.

### Item 5 — F-10: mid-stream non-frame envelope must ERROR, not clean-EOF

No proto change. `pkg/codec/tape.go:314-335` `ReadFrame`: only a **footer** ends frames;
any other non-frame envelope (stray header, empty `Envelope{}`) is malformed and must
error. Add a sentinel and split the branch.

**Sentinel** — `pkg/codec/tape.go` (file already imports `errors`, line 4). Add near the
top-level vars, or in `limits.go` beside the other sentinels:

```go
// ErrUnexpectedEnvelope is returned when the frame stream contains a non-frame,
// non-footer envelope (e.g. a stray header or an empty envelope) before the
// footer — a malformed or truncated-and-concatenated capture.
var ErrUnexpectedEnvelope = errors.New("unexpected non-frame envelope before footer")
```

**Fix** — replace the tail of `ReadFrame` (`tape.go:330-334`):

```go
	// A footer marks the clean end of frames.
	if footer := env.GetFooter(); footer != nil {
		r.pendingFooter = footer
		return nil, io.EOF
	}
	// Anything else here is a malformed stream.
	return nil, fmt.Errorf("tape: %w", ErrUnexpectedEnvelope)
```

Safe against existing tests: `ReadHeader` still consumes the leading header first, and
every reader reads the header before frames; empty captures still end on the footer → EOF
(`TestTapeEmptyCapture`, `TestReader_SEC001_*` all read header-then-frames).

**Red test** (`pkg/codec/tape_test.go` or new `pkg/codec/stream_integrity_test.go`). Craft
an uncompressed stream of `[header][frame][header-again]`, zstd it, and read:

```go
func TestReader_MidStreamNonFrameEnvelopeErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concat.tape")
	f, err := os.Create(path)
	if err != nil { t.Fatal(err) }
	enc, err := zstd.NewWriter(f)
	if err != nil { t.Fatal(err) }
	writeEnv := func(e *capturepb.Envelope) {
		data, err := proto.Marshal(e)
		if err != nil { t.Fatal(err) }
		var buf [10]byte
		l := uint64(len(data)); i := 0
		for l >= 0x80 { buf[i] = byte(l) | 0x80; l >>= 7; i++ }
		buf[i] = byte(l); i++
		enc.Write(buf[:i]); enc.Write(data)
	}
	writeEnv(&capturepb.Envelope{Message: &capturepb.Envelope_Header{Header: &capturepb.CaptureHeader{}}})
	writeEnv(&capturepb.Envelope{Message: &capturepb.Envelope_Frame{Frame: &capturepb.Frame{}}})
	writeEnv(&capturepb.Envelope{Message: &capturepb.Envelope_Header{Header: &capturepb.CaptureHeader{}}}) // stray
	if err := enc.Close(); err != nil { t.Fatal(err) }
	if err := f.Close(); err != nil { t.Fatal(err) }

	r, err := NewReader(path)
	if err != nil { t.Fatal(err) }
	defer r.Close()
	if _, err := r.ReadHeader(); err != nil { t.Fatalf("ReadHeader: %v", err) }
	if _, err := r.ReadFrame(); err != nil { t.Fatalf("ReadFrame #1: %v", err) } // the real frame
	_, err = r.ReadFrame()                                                       // hits the stray header
	if !errors.Is(err, ErrUnexpectedEnvelope) {
		t.Fatalf("mid-stream stray header: want ErrUnexpectedEnvelope, got %v", err)
	}
}
```

Red today: second `ReadFrame` returns `io.EOF` (clean). Green after fix.

### Item 6 — F-8: GoalScored `person_scored` / `assist_scored` (add + populate)

**Proto** — `nevr-proto/telemetry/v2/echo_arena.proto`, message `GoalScored`
(fields 1-7; **8,9 free**). Append:

```proto
  int32 scorer_slot = 6;
  // Slot of the assisting player. Absent if no assist.
  optional int32 assist_slot = 7;
  // Scorer / assister display names from the engine (v1 LastScore). Empty when
  // unknown. Authoritative scorer IDENTITY at the GoalScored level: prefer a
  // non-empty person_scored over scorer_slot, which is 0 both for "slot 0" and
  // "unresolved" (the real scorer slot rides the parallel PlayerGoal event).
  string person_scored = 8;
  string assist_scored = 9;
```

**Disambiguation mechanism (recommended, no proto churn):** do NOT retype `scorer_slot` to
`optional` (that trips `buf breaking` presence rules). Instead the new `person_scored`
string is the presence signal — empty = unresolved, non-empty = the actual scorer — and
the real scorer *slot* remains available on the parallel `PlayerGoal` event
(`sensor_stats.go:159-161`) plus the header roster. Document this on the field (done above).

**Wire-up** — `pkg/conversion/mapping.go:771-785`, add two lines inside the `if sd != nil`
block (drop the now-stale comment):

```go
			gs.DistanceThrown = float32(sd.GetDistanceThrown())
			gs.PersonScored = sd.GetPersonScored()
			gs.AssistScored = sd.GetAssistScored()
```

**Red test** — extend/replace the existing audit test
(`pkg/conversion/mapping_audit_test.go:18-69` `TestGoalScored_...` uses
`PersonScored:"PlayerOne"`, `AssistScored:"PlayerTwo"`). Add:

```go
func TestGoalScored_NamesPopulated(t *testing.T) {
	v1e := &telemetryv1.LobbySessionEvent{
		Event: &telemetryv1.LobbySessionEvent_GoalScored{
			GoalScored: &telemetryv1.GoalScored{
				ScoreDetails: &enginev1.LastScore{
					PersonScored: "PlayerOne", AssistScored: "PlayerTwo",
				},
			},
		},
	}
	gs := mapEvent(v1e).GetGoalScored()
	if gs.GetPersonScored() != "PlayerOne" {
		t.Errorf("person_scored = %q, want PlayerOne", gs.GetPersonScored())
	}
	if gs.GetAssistScored() != "PlayerTwo" {
		t.Errorf("assist_scored = %q, want PlayerTwo", gs.GetAssistScored())
	}
}
```

Red after proto regen (getters return ""). Green after wire-up. The existing
`TestGoalScored_ScorerSlotAndAssistSlotStayZero` (asserts `scorer_slot==0`,
`assist_slot==nil`) stays true and unchanged.

**Round-trip note:** `SessionReconstructor` rebuilds `SessionResponse` but `GoalScored`
does not flow back into a per-frame `last_score` today (F-5/F-8 territory), so this batch
adds the names to the tape but does not yet reconstruct them — that's Hydrator work in the
pending queue. `roundtrip_v2_test.go measureFindings` already lists `last_score` as a
finding, so no BAC regression.

### Batch verification gate

`buf build && buf lint && buf breaking --against '.git#branch=main'` (nevr-proto) → clean;
`gofmt -l . && go vet ./... && golangci-lint run && go test -race ./...` (tape) → clean;
regenerate golden + `TestConvertDeterministic` ×2. Everything above is additive; the only
byte-level churn is the header (items 1,2) and any pause/goal frames (items 3,6) → one
golden regeneration.

## 7. Pending-decision queue for Andrew (recommendations attached; NOT specced)

1. **`EchoArenaFrameDense` contents + raw-orientation-basis home.** *Recommend:* new
   `Basis { spatial.v1.Vec3 forward = 1; left = 2; up = 3; }` message; carry it per-frame
   on the dense frame (and on sparse per-player/disc) as an OPTIONAL companion to the
   quaternion. **Do NOT touch `spatial.v1.Pose`** — it is shared with `broadcaster/v1`
   physics messages (§1.4). Full dense field roster needs one design pass.
2. **F-3 frame-0 `ScoreboardUpdated` seed + golden-regen sequencing.** *Recommend:* do it
   AFTER the mechanical-testdata generator lands, so the seed's golden churn is absorbed
   by the same regeneration rather than two. Blocks a correct Hydrator.
3. **F-7 stale `last_throw`/`last_score` at frame 0 (spurious throw/goal).** *Recommend:*
   KEEP + mark as seed (option b) — under the dense/carry-forward model a frame-0
   throw/goal correctly seeds carried state, mirroring Loadout/Grab seeding. Suppressing
   (option a) loses "the last throw before capture started." Needs Andrew's semantic call.
4. **`possession_time` per-frame stat.** It changes every frame, so it CANNOT be
   event-sampled like the other stats. *Recommend:* carry it as a genuine per-frame field
   on the dense frame (and decide whether sparse samples it or omits it, accepting that as
   the one non-lossless-by-design stat). Andrew's call on the sparse treatment.
5. **Player metadata shape:** typed `uuid` + `map<string,string> metadata` on
   `PlayerInfo`/`PlayerJoined`, and a conditional `username` field. *Recommend:* additive
   `string uuid` + `map<string,string> metadata` are safe next-frees whenever wanted;
   HOLD `username` pending Andrew's username-vs-`display_name` answer (don't add two
   name fields until the semantics are fixed).

---

*Interrogate me. I hold: every line of tape (source+tests), all of nevr-proto, the
locked 2.1 design decisions, the finding registry (F-1…F-16), and the batch spec above.*
