# testdata

## `sample.echoreplay`

A real EchoReplay capture (ZIP-wrapped `timestamp\tsessionJSON[\tbonesJSON]`,
~2.1 MB on disk / 8.76 MB uncompressed, 1023 frames, 36 detected events). It is
the same real capture the codec benchmark already uses as its "real file"
(`pkg/codec/echoreplay_bench_test.go:104`), copied here so the conversion tests
that were skipping now have an input to run against.

Tests unblocked by this file:

- `TestConvertFile_EchoReplay` (`pkg/conversion/convert_test.go:68`) — converts
  the capture and verifies the output is a valid tape file with a non-zero,
  consistent frame count. **Deterministic; runs and passes.**
- `TestGoldenConvert` (`pkg/conversion/golden_test.go:19`) — converts the
  capture and compares the tape bytes against a committed golden. See the
  caveat below.

## Why no `sample.tape.golden` is committed

`TestGoldenConvert` is byte-exact. The conversion output is **not
byte-deterministic** for the same input, so a committed golden would be a flaky
(wrong) golden. Evidence: converting `sample.echoreplay` repeatedly produces
different tape bytes — `go test -count=10 -run TestGoldenConvert` FAILS on most
runs once a golden exists.

Root cause (pre-existing, unrelated to the role-unify change):

- The player sensors build their per-frame event queue by iterating a Go map
  (`pkg/events/sensor_player.go:39` for `PlayerJoinSensor`, `:101` for
  `PlayerLeaveSensor`, `:163` for `PlayerTeamSwitchSensor`). Go randomizes map
  iteration order.
- `detectEvents` appends those events in emission order with no sort
  (`pkg/events/events.go:343-358`), and `drainEvents` attaches them to the
  frame without sorting (`pkg/conversion/convert.go:252-261`).
- The first frame of a real capture has every player join at once, so the
  `PlayerJoined` events serialize in a random order, and the tape bytes differ
  run to run.
- A secondary faithfulness risk: the synchronous detector drops event batches
  if the events channel is full (`pkg/events/events.go:219-223`).

Because of this, `sample.tape.golden` is **gitignored** (`/testdata/*.golden`).
`TestGoldenConvert` self-generates the golden on first run and passes, so it
acts as a smoke test over the full real-data pipeline (no crash, non-zero
frames, readable output) — but it does **not** assert byte stability yet.

To make `TestGoldenConvert` a real fidelity gate, event emission must be made
deterministic first (e.g. sort the per-frame detected events by player slot in
`detectEvents`), after which a golden can be committed and the gitignore line
removed. That ordering change is out of scope for the role-unify branch.
