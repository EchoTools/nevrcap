# nevrcap

High-performance telemetry processing and streaming library for NEVR lobby session data.

## Overview

This package provides optimized processing of game session frames with support for:

- **High-frequency frame processing** (600+ Hz capable)
- **Event detection** between consecutive frames
- **Multiple streaming codecs** (.nevrcap, .echoreplay)
- **File format conversion** utilities

## Installation

```bash
go get github.com/echotools/nevr-capture/v3
```

## Package Structure

```
pkg/
├── codecs/      # File format readers/writers (.nevrcap, .echoreplay)
├── conversion/  # Format conversion utilities
├── events/      # Event detection algorithms
└── processing/  # Frame processing pipeline
```

## Building

```bash
# Download dependencies
go mod download

# Run tests
go test -v ./...

# Run benchmarks
go test -bench=. -benchmem ./...
```

## Usage

### Codecs

#### NevrCap Codec (.nevrcap files)

Zstd-compressed protobuf format for efficient storage and streaming.

```go
import "github.com/echotools/nevr-capture/v3/pkg/codecs"

// Writing
writer, err := codecs.NewNevrCapWriter("capture.nevrcap")
if err != nil {
    log.Fatal(err)
}
defer writer.Close()

// Write frames
err = writer.WriteFrame(frame)

// Reading
reader, err := codecs.NewNevrCapReader("capture.nevrcap")
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

frame, err := reader.ReadFrame()
```

#### EchoReplay Codec (.echoreplay files)

ZIP-compressed JSON format for legacy compatibility.

```go
import "github.com/echotools/nevr-capture/v3/pkg/codecs"

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
import "github.com/echotools/nevr-capture/v3/pkg/conversion"

// Convert .echoreplay to .nevrcap
err := conversion.ConvertEchoReplayToNevrcap("input.echoreplay", "output.nevrcap")

// Convert .nevrcap to .echoreplay  
err := conversion.ConvertNevrcapToEchoReplay("input.nevrcap", "output.echoreplay")

// Batch convert all files matching pattern
err := conversion.BatchConvert("*.echoreplay", "./output", true) // toNevrcap=true
```

### Event Detection

```go
import "github.com/echotools/nevr-capture/v3/pkg/events"

detector := events.NewEventDetector()

// Detect events between consecutive frames
detectedEvents := detector.DetectEvents(previousFrame, currentFrame)

for _, event := range detectedEvents {
    fmt.Printf("Event: %s\n", event.Type)
}
```

## Event Types

The system automatically detects various game events:

### Game State Events
- Round started/ended
- Match ended
- Scoreboard updates
- Game paused/unpaused

### Player Events
- Player joined/left
- Team switches
- Emote playing

### Disc Events
- Possession changes
- Disc thrown/caught

### Stat Events
- Saves, stuns, passes
- Catches, steals, blocks
- Interceptions, assists
- Shots taken

## File Formats

### .nevrcap Format

| Property | Value |
|----------|-------|
| Compression | Zstd |
| Serialization | Protocol Buffers |
| Structure | Header + length-delimited frames |
| Features | Event detection, streaming support |
| Size | ~57% of .echoreplay size |

### .echoreplay Format  

| Property | Value |
|----------|-------|
| Compression | ZIP |
| Serialization | JSON |
| Structure | ZIP archive with replay.txt |
| Features | Legacy compatibility |
| Size | Baseline reference |

## Benchmarks

```bash
# Run all benchmarks
go test -bench=. -benchmem ./...

# Quick benchmark (frame processing only)
go test -bench=BenchmarkFrameProcessing -benchtime=1s ./pkg/processing
```

**Performance Targets:**
- Frame Processing: 600+ Hz (achieved: 14,000+ Hz)
- Event Detection: <1ms per frame

See [BENCHMARKS.md](BENCHMARKS.md) for detailed performance metrics.

## Related Repositories

- [nevr-common](https://github.com/echotools/nevr-common) - Protobuf definitions
- [nevr-agent](https://github.com/echotools/nevr-agent) - Recording and streaming CLI

## Contributing

When adding new event types:

1. Update protobuf definitions in `nevr-common/proto/telemetry/`
2. Regenerate protobuf code in nevr-common
3. Add detection logic in `pkg/events/`
4. Add tests in `*_test.go` files
5. Update benchmarks if needed
6. Run the full test suite: `go test -v ./...`

## License

See [LICENSE](LICENSE) file for details.