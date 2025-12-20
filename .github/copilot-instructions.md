# GitHub Copilot Instructions for nevrcap

## Project Overview

nevrcap is a high-performance Go library for processing EchoVR telemetry frames. It provides codec implementations for file formats and event detection algorithms.

## Architecture

```
pkg/
├── codecs/      # File format readers/writers
│   ├── codec_echoreplay.go   # ZIP + JSON format (legacy)
│   └── codec_nevrcap.go      # Zstd + protobuf format (efficient)
├── conversion/  # Format conversion utilities
├── events/      # Frame-to-frame event detection
└── processing/  # Frame processing pipeline
```

**Depends on**: `nevr-common` for protobuf types (`telemetry.LobbySessionStateFrame`)

## Key Patterns

### Codec Interface
Both formats implement consistent reader/writer interfaces:
```go
// Writing
writer, _ := codecs.NewNevrCapWriter("file.nevrcap")
writer.WriteFrame(frame)
writer.Close()

// Reading
reader, _ := codecs.NewNevrCapReader("file.nevrcap")
frame, _ := reader.ReadFrame()
reader.Close()
```

### Event Detection
Detect game events by comparing consecutive frames:
```go
detector := events.NewEventDetector()
events := detector.DetectEvents(previousFrame, currentFrame)
```

Event types: goals, saves, stuns, passes, possession changes, round start/end, player join/leave

## Build & Test

```bash
go test -v ./...                           # Run all tests
go test -bench=. -benchmem ./...           # Run benchmarks
go test -bench=BenchmarkFrameProcessing ./pkg/processing  # Specific benchmark
```

**Performance targets**: Frame processing 600+ Hz (achieved: 14,000+ Hz)

## File Format Comparison

| Format | Compression | Serialization | Size |
|--------|-------------|---------------|------|
| .nevrcap | Zstd | Protobuf | ~57% of echoreplay |
| .echoreplay | ZIP | JSON | Baseline |

## Adding New Event Types

1. Add event message to `nevr-common/proto/telemetry/v1/telemetry.proto`
2. Regenerate protos in nevr-common (`buf generate`)
3. Add detection logic in `pkg/events/`
4. Add tests and update benchmarks
