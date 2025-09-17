#!/bin/bash

# Build script for nevrcap library

set -e

echo "Building nevrcap library..."

# Change to project root
cd "$(dirname "$0")/.."

# Clean any previous builds
echo "Cleaning previous builds..."
go clean -cache

# Download dependencies
echo "Downloading dependencies..."
go mod download
go mod tidy

# Generate protobuf files
echo "Generating protobuf files..."
export PATH="$HOME/go/bin:$PATH"
if command -v protoc >/dev/null 2>&1 && command -v protoc-gen-go >/dev/null 2>&1; then
    protoc -I./proto -I./third_party/googleapis --go_out=./gen/go --go_opt=paths=source_relative rtapi/telemetry_v1.proto apigame/http_v1.proto
    echo "✅ Protobuf files generated successfully"
else
    echo "⚠️ Warning: protoc or protoc-gen-go not found. Using existing generated files."
fi

# Build the library
echo "Building library..."
go build ./...

# Run tests
echo "Running tests..."
go test -v ./...

# Run benchmarks (quick run)
echo "Running quick benchmarks..."
go test -bench=BenchmarkFrameProcessing -benchtime=1s

echo "✅ Build completed successfully!"
echo ""
echo "To run full benchmarks:"
echo "  go test -bench=. -benchmem"
echo ""
echo "To generate documentation:"
echo "  go doc -all ."