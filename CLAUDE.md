# tape

Go codec library and CLI for reading and writing `.tape` telemetry capture files.
Core dependency of nevr-agent, nevr-anticheat, and nevr-profiler.

Module: `github.com/echotools/tape`

## Build & Test

```bash
go build ./...             # build library + tapedeck CLI
go test ./...              # run all tests
go test -bench=. ./...     # run benchmarks
go vet ./...               # static analysis
gofmt -l .                 # check formatting
```

The `tapedeck` CLI lives in `cmd/tapedeck/` (convert, show, replay, verify, stats, diff, trim commands).

Build runner: `just` (see justfile). `just` runs fmt, vet, lint, test by default.

## Architecture

### `pkg/codec`

Three codec implementations for telemetry frame serialization:

- **TapeV1** — Zstd-compressed, length-delimited protobuf stream using `telemetry.v1` types.
  Writes a `TelemetryHeader` followed by sequential `LobbySessionStateFrame` messages.
  File extensions: `.tape`, `.nevrcap` (legacy).
- **TapeV2** — Zstd-compressed, length-delimited `telemetry.v2.Envelope` stream.
  Game-agnostic envelope with `CaptureHeader`, sequential `Frame` messages, and `CaptureFooter`
  with seek indexes. Uses `spatial.v1` float32 primitives for 73.5% wire size reduction vs v1.
- **EchoReplay** — Zip-wrapped, line-oriented TSV+JSON format (`.echoreplay`).
  Legacy format from the game engine. Each line: `timestamp\tsessionJSON[\tbonesJSON]`.

### v1 vs v2 tape format

| Property      | v1 (TapeV1)                | v2 (TapeV2)                                                                 |
| ------------- | -------------------------- | --------------------------------------------------------------------------- |
| Envelope      | Raw protobuf messages      | `telemetry.v2.Envelope` oneof                                               |
| Header        | `TelemetryHeader`          | `CaptureHeader` with metadata map                                           |
| Frames        | `LobbySessionStateFrame`   | `Frame` with game-agnostic timing + oneof payload                           |
| Footer        | None                       | `CaptureFooter` with frame count, duration, keyframe/event indexes          |
| Spatial types | `repeated double` (64-bit) | `spatial.v1` float32 + bytes for bones                                      |
| Extension     | `.tape` / `.nevrcap`       | `.tape`                                                                     |
| Random access | No                         | Footer enables efficient scanning (true seeking needs Zstd seekable frames) |

### `pkg/conversion`

Bidirectional conversion between `.echoreplay` and `.tape` (v1) formats. Runs
event detection during echoreplay-to-tape conversion so the resulting file
includes enriched event data.

### `pkg/events`

Async and synchronous event detection pipeline. Sensors analyze sequential
frames and emit `LobbySessionEvent` messages (player joins, goals, game state
transitions). The `Detector` interface allows pluggable sensor implementations.

### `pkg/processing`

High-performance frame processor (designed for up to 600 Hz). Unmarshals raw
JSON frame data, runs event detection, and produces enriched protobuf frames.

## Conventions

- **Round-trip fidelity**: `decode(encode(x)) == x` for the tape format.
  Any codec change must preserve this property.
- **File extensions**: `.tape` is canonical. `.nevrcap` is accepted as legacy input.
- **EchoReplay compatibility**: The echoreplay writer applies `FixProtojsonUint64Encoding`
  and `FixExponentNotation` to match the original game engine output exactly.
  Third-party parsers depend on this byte-level compatibility.

## Dependencies

- **nevr-proto** (`buf.build/echotools/nevr-api`) — Protobuf definitions distributed
  via the Buf Schema Registry. Go types at `buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/`.
- **klauspost/compress** — Zstd compression for the tape format.
- **google.golang.org/protobuf** — Proto marshaling/unmarshaling and protojson
  for echoreplay format compatibility.

## CI

GitHub Actions workflows in `.github/workflows/`. Actions pinned to commit SHAs.
PR validation includes lint, format check, go vet, tests, coverage, race detection,
and vulnerability scanning. Main branch adds benchmark regression detection.
