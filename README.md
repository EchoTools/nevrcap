# nevrcap

High-performance telemetry processing and streaming library for NEVR lobby session data.

## Overview

This package provides optimized processing of game session frames with support for:

- **High-frequency frame processing**
- **Event detection** between consecutive frames
- **Multiple streaming codecs** (.nevrcap, .echoreplay, WebSocket)
- **File format conversion** utilities


## Installation

```bash
go get github.com/echotools/nevrcap
```

## Building

Manual build steps:

```bash
# Download dependencies
go mod download
go mod tidy

# Build the library
make all

# Run tests
go test -v ./...

# Run benchmarks
go test -bench=. -benchmem
```

## Usage

### Frame Processing

```go
import "github.com/echotools/nevrcap"

// Create processor
processor := nevrcap.NewFrameProcessor()

// Process raw game data
frame, err := processor.ProcessFrame(sessionData, userBonesData, timestamp)
if err != nil {
    log.Fatal(err)
}

// Events are automatically detected
fmt.Printf("Detected %d events in frame %d\n", len(frame.Events), frame.FrameIndex)
```

### Streaming Codecs

#### Zstd Codec (.nevrcap files)

```go
// Writing
writer, err := nevrcap.NewZstdCodecWriter("capture.nevrcap")
if err != nil {
    log.Fatal(err)
}
defer writer.Close()

// Write header
header := &rtapi.TelemetryHeader{
    CaptureId: uuid.Must(uuiid.NewV4()),
    CreatedAt: timestamppb.Now(),
}
err = writer.WriteHeader(header)
if err != nil {
    log.Fatal(err)
}

// Write frames
err = writer.WriteFrame(frame)
if err != nil {
    log.Fatal(err)
}

// Reading
reader, err := nevrcap.NewZstdCodecReader("capture.nevrcap")
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

header, err := reader.ReadHeader()
if err != nil {
    log.Fatal(err)
}

frame, err := reader.ReadFrame()
if err != nil {
    log.Fatal(err)
}
```

#### EchoReplay Codec (.echoreplay files)

```go
// Reading
reader, err := nevrcap.NewEchoReplayFileReader("replay.echoreplay")
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

frames, err := reader.ReadFrames()
if err != nil {
    log.Fatal(err)
}
```

### File Conversion

```go
// Convert .echoreplay to .nevrcap
err := nevrcap.ConvertEchoReplayToNevrcap("input.echoreplay", "output.nevrcap")

// Convert .nevrcap to .echoreplay  
err := nevrcap.ConvertNevrcapToEchoReplay("input.nevrcap", "output.echoreplay")
```

## Event Detection

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

- **Compression**: Zstd
- **Serialization**: Protocol Buffers
- **Structure**: Header + length-delimited frames
- **Features**: Event detection, streaming support
- **Size**: ~57% of .echoreplay size

### .echoreplay Format  

- **Compression**: ZIP
- **Serialization**: JSON
- **Structure**: ZIP archive with replay.txt
- **Features**: Legacy compatibility
- **Size**: Baseline reference

## Benchmarks

Run benchmarks:
```bash
go test -bench=. -benchmem
```

Quick benchmark (just frame processing):
```bash
go test -bench=BenchmarkFrameProcessing -benchtime=1s
```

See [BENCHMARKS.md](BENCHMARKS.md) for detailed performance metrics.

## Optimization Features

1. **Pre-allocated Structures**: Reuses objects to minimize GC pressure
2. **Efficient Event Detection**: O(1) player lookups using maps
3. **Streaming Support**: Processes data incrementally
4. **High-Performance Compression**: Zstd provides optimal speed/size ratio
5. **Memory Pooling**: Minimal allocations for high-frequency operations

## Contributing

When adding new event types:

1. Update protobuf definitions in `nevr-common/proto/rtapi/telemetry_v1.proto`
2. Regenerate protobuf code: `go generate` or `./scripts/build.sh`
3. Add detection logic in `events.go`
4. Add tests in `*_test.go` files
5. Update benchmarks if needed
6. Run the full test suite: `go test -v ./...`