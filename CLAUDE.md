# nevrcap

Go codec library for reading and writing `.nevrcap` and `.tape` telemetry
capture files. Core dependency of nevr-agent, evr-anticheat, and nevr-profiler.

Module: `github.com/echotools/nevr-capture/v3`

## Build & Test

```bash
go test ./...              # run all tests
go test -bench=. ./...     # run benchmarks
go vet ./...               # static analysis
go fmt ./...               # format
```

No binary is produced; this is a library-only module.

## Architecture

### `pkg/codecs`

Two codec implementations for telemetry frame serialization:

- **NevrCap** — Zstd-compressed, length-delimited protobuf stream (`.nevrcap`/`.tape`).
  Writes a `TelemetryHeader` followed by sequential `LobbySessionStateFrame` messages.
- **EchoReplay** — Zip-wrapped, line-oriented TSV+JSON format (`.echoreplay`).
  Legacy format from the game engine. Each line: `timestamp\tsessionJSON[\t bonesJSON]`.

Both codecs expose symmetric `NewXxxWriter`/`NewXxxReader` constructors and
`WriteFrame`/`ReadFrame` methods operating on `telemetry.LobbySessionStateFrame`.

### `pkg/conversion`

Bidirectional conversion between `.echoreplay` and `.nevrcap` formats. Runs
event detection during echoreplay-to-nevrcap conversion so the resulting file
includes enriched event data.

### `pkg/events`

Async and synchronous event detection pipeline. Sensors analyze sequential
frames and emit `LobbySessionEvent` messages (player joins, goals, game state
transitions). The `Detector` interface allows pluggable sensor implementations.

### `pkg/processing`

High-performance frame processor (designed for up to 600 Hz). Unmarshals raw
JSON frame data, runs event detection, and produces enriched protobuf frames.

## Conventions

- **Round-trip fidelity**: `decode(encode(x)) == x` for the nevrcap format.
  Any codec change must preserve this property. Round-trip tests exist in
  `pkg/codecs/codec_roundtrip_test.go`.
- **File extensions**: `.nevrcap` and `.tape` are interchangeable names for the
  same Zstd+protobuf format. Both must be supported everywhere.
- **EchoReplay compatibility**: The echoreplay writer applies `FixProtojsonUint64Encoding`
  and `FixExponentNotation` to match the original game engine output exactly.
  Third-party parsers depend on this byte-level compatibility.
- **No backward-compatibility obligation on protos**: Break anything that is
  non-idiomatic. Protobuf definitions live in `nevr-proto`.

## Dependencies

- **nevr-proto** (`github.com/echotools/nevr-proto/v4`) — Protobuf definitions
  for telemetry, apigame, and shared types. All `.proto` generated Go code
  lives there.
- **klauspost/compress** — Zstd compression for the nevrcap format.
- **google.golang.org/protobuf** — Proto marshaling/unmarshaling and protojson
  for echoreplay format compatibility.

## CI

GitHub Actions workflows live in `.github/`. Linting uses golangci-lint
(config in `.golangci.yml`).
