# nevr-tape

High-performance telemetry codec library for NEVR session data.

## Overview

This package provides optimized processing of game session frames with support for:

- **Two tape formats** — v1 (legacy `telemetry.v1`) and v2 (game-agnostic `telemetry.v2` envelope with footer indexes)
- **High-frequency frame processing** (600+ Hz capable)
- **Event detection** between consecutive frames
- **Legacy codec** for `.echoreplay` files
- **File format conversion** utilities

## Installation

```bash
go get github.com/echotools/nevr-tape/v1
```

## Package Structure

```
pkg/
├── codecs/      # File format readers/writers (.tape, .echoreplay)
├── conversion/  # Format conversion utilities
├── events/      # Event detection algorithms
└── processing/  # Frame processing pipeline
```

## Building

```bash
go test -v ./...                  # Run tests
go test -bench=. -benchmem ./... # Run benchmarks
go vet ./...                      # Static analysis
```

## Usage

### Tape v2 Codec (recommended for new code)

Zstd-compressed `telemetry.v2.Envelope` stream with header, frames, and footer.

```go
import "github.com/echotools/nevr-tape/v1/pkg/codecs"

// Writing
w, err := codecs.NewTapeV2Writer("capture.tape")
if err != nil {
    log.Fatal(err)
}
err = w.WriteHeader(header) // *telemetryv2.CaptureHeader
for _, frame := range frames {
    err = w.WriteFrame(frame) // *telemetryv2.Frame
}
err = w.Close() // writes footer, closes zstd, closes file

// Reading
r, err := codecs.NewTapeV2Reader("capture.tape")
if err != nil {
    log.Fatal(err)
}
defer r.Close()

header, err := r.ReadHeader()
for {
    frame, err := r.ReadFrame()
    if err != nil {
        break // io.EOF when footer is reached
    }
}
footer, err := r.ReadFooter() // frame count, duration, keyframe/event indexes
```

### Tape v1 Codec (legacy)

Zstd-compressed protobuf stream for `telemetry.v1` types. Reads both `.tape` and `.nevrcap` files.

```go
import "github.com/echotools/nevr-tape/v1/pkg/codecs"

// Writing
writer, err := codecs.NewTapeV1Writer("capture.tape")
if err != nil {
    log.Fatal(err)
}
defer writer.Close()
err = writer.WriteFrame(frame)

// Reading
reader, err := codecs.NewTapeV1Reader("capture.tape")
if err != nil {
    log.Fatal(err)
}
defer reader.Close()
frame, err := reader.ReadFrame()
```

### EchoReplay Codec (.echoreplay files)

ZIP-compressed JSON format for legacy compatibility.

```go
import "github.com/echotools/nevr-tape/v1/pkg/codecs"

// Writing
writer, err := codecs.NewEchoReplayWriter("replay.echoreplay")
if err != nil {
    log.Fatal(err)
}
defer writer.Close()

// Reading
reader, err := codecs.NewEchoReplayReader("replay.echoreplay")
if err != nil {
    log.Fatal(err)
}
defer reader.Close()
```

### File Conversion

```go
import "github.com/echotools/nevr-tape/v1/pkg/conversion"

// Convert .echoreplay to .tape (v1)
err := conversion.ConvertEchoReplayToNevrcap("input.echoreplay", "output.tape")

// Convert .tape (v1) to .echoreplay
err := conversion.ConvertNevrcapToEchoReplay("input.tape", "output.echoreplay")
```

### Event Detection

```go
import "github.com/echotools/nevr-tape/v1/pkg/events"

detector := events.NewEventDetector()
detectedEvents := detector.DetectEvents(previousFrame, currentFrame)
```

## File Formats

### .tape v2 (recommended)

| Property | Value |
|----------|-------|
| Compression | Zstd |
| Serialization | `telemetry.v2.Envelope` protobuf |
| Structure | Header + frames + footer with seek indexes |
| Wire size | ~73.5% smaller than v1 (float32 vs float64 spatial) |
| Random access | Footer-based scanning (true seeking needs Zstd seekable frames) |

### .tape / .nevrcap v1 (legacy)

| Property | Value |
|----------|-------|
| Compression | Zstd |
| Serialization | `telemetry.v1` protobuf |
| Structure | Header + length-delimited frames |
| Size | ~57% of .echoreplay size |

### .echoreplay (legacy)

| Property | Value |
|----------|-------|
| Compression | ZIP |
| Serialization | JSON |
| Structure | ZIP archive with TSV lines |
| Features | Legacy compatibility |

## Migration from nevr-capture

```diff
- go get github.com/echotools/nevr-capture/v3
+ go get github.com/echotools/nevr-tape/v1
```

| Old | New |
|-----|-----|
| `codecs.NewNevrCapWriter` | `codecs.NewTapeV1Writer` |
| `codecs.NewNevrCapReader` | `codecs.NewTapeV1Reader` |
| `codecs.NevrCap` struct | `codecs.TapeV1` struct |
| `apigame.SessionResponse` | `enginev1.SessionResponse` |

Proto imports changed from `github.com/echotools/nevr-common/v4/gen/go/` to
`buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/`.

## Related Repositories

- [nevr-proto](https://github.com/echotools/nevr-proto) — Protobuf definitions (BSR: `buf.build/echotools/nevr-api`)
- [nevr-agent](https://github.com/echotools/nevr-agent) — Recording and streaming CLI

## License

See [LICENSE](LICENSE) file for details.
