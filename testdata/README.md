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
  capture and compares the tape bytes against the committed
  `sample.tape.golden`. **Byte-exact fidelity gate; deterministic, passes
  repeatably (`go test -count=10`).**

## `sample.tape.golden` is committed and trustworthy

Conversion is now byte-deterministic: converting `sample.echoreplay` always
produces the same tape bytes
(`sha256:cc060af5e7c937165239e977b8aff1b4520eb530b53c121796ad572485a9b41e`,
1625143 bytes, 1023 frames, 36 events). The golden is committed and tracked
(its `/testdata/*.golden` gitignore entry was removed), so `TestGoldenConvert`
is a real byte-level fidelity gate, not just a smoke test.

### The non-determinism that used to block it (now fixed)

Three player sensors built their per-frame event queue by iterating a Go map,
whose iteration order is randomized:

- `PlayerJoinSensor` (`pkg/events/sensor_player.go:39`)
- `PlayerLeaveSensor` (`pkg/events/sensor_player.go:101`)
- `PlayerTeamSwitchSensor` (`pkg/events/sensor_player.go:163`)

The first frame of a real capture has every player join at once, so the
`PlayerJoined` events serialized in a random order and the tape bytes differed
run to run. The fix collects the affected player slots, sorts them ascending
(slot is the player's stable identity), and emits in that order — the same set
of events, a deterministic order, and no allocation or sort on the common
no-event frame. Verified by `TestConvertDeterministic`
(`pkg/conversion/determinism_test.go`) and the per-sensor ordering tests in
`pkg/events/sensor_player_order_test.go`.

The other event sensors were already deterministic: they emit while iterating
the session's team/player **slices** (fixed order), not maps.

### The dropped-batch risk (investigated, not a loss)

The synchronous detector drops an event batch if its events channel is full
(`pkg/events/events.go:219-223`). This does **not** lose events during
conversion: the pipeline drains the channel fully after every frame
(`pkg/conversion/convert.go:232,252-261`), so the channel never holds more than
one batch and the drop branch is unreachable on this path. Verified by
`TestSyncDetector_ConversionPatternNoEventLoss`
(`pkg/events/sync_detector_drop_test.go`).
