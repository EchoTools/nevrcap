package nevrcap

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"github.com/gofrs/uuid/v5"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// BenchmarkWriteFrame benchmarks the optimized WriteFrame method
func BenchmarkWriteFrame(b *testing.B) {
	tempDir := b.TempDir()
	filename := filepath.Join(tempDir, "benchmark.echoreplay")

	codec, err := NewEchoReplayCodecWriter(filename)
	if err != nil {
		b.Fatal(err)
	}
	defer codec.Close()

	// Create a sample frame
	frame := &rtapi.LobbySessionStateFrame{
		Timestamp: timestamppb.New(time.Now()),
		Session: &apigame.SessionResponse{
			SessionId: uuid.Must(uuid.NewV4()).String(),
			// Add more realistic session data
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := codec.WriteFrame(frame); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWriteFrameBatch benchmarks batch writing
func BenchmarkWriteFrameBatch(b *testing.B) {
	tempDir := b.TempDir()
	filename := filepath.Join(tempDir, "benchmark_batch.echoreplay")

	codec, err := NewEchoReplayCodecWriter(filename)
	if err != nil {
		b.Fatal(err)
	}
	defer codec.Close()

	// Create a batch of sample frames
	frames := make([]*rtapi.LobbySessionStateFrame, 100)
	for i := range frames {
		frames[i] = &rtapi.LobbySessionStateFrame{
			Timestamp: timestamppb.New(time.Now().Add(time.Duration(i) * time.Millisecond)),
			Session: &apigame.SessionResponse{
				SessionId: uuid.Must(uuid.NewV4()).String(),
			},
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := codec.WriteFrameBatch(frames); err != nil {
			b.Fatal(err)
		}
	}
}

// ExampleEchoReplayCodec demonstrates how to use the optimized codec
func ExampleEchoReplayCodec() {
	// Create a new codec for writing
	codec, err := NewEchoReplayCodecWriter("example_optimized.echoreplay")
	if err != nil {
		fmt.Printf("Error creating codec: %v\n", err)
		return
	}
	defer codec.Close()

	// Write some frames
	for i := 0; i < 1000; i++ {
		frame := &rtapi.LobbySessionStateFrame{
			Timestamp: timestamppb.New(time.Now().Add(time.Duration(i) * time.Millisecond)),
			Session: &apigame.SessionResponse{
				SessionId: uuid.Must(uuid.NewV4()).String(),
			},
		}

		if err := codec.WriteFrame(frame); err != nil {
			fmt.Printf("Error writing frame: %v\n", err)
			return
		}

		// Check buffer size periodically
		if i%100 == 0 {
			fmt.Printf("Buffer size after %d frames: %d bytes\n", i+1, codec.GetBufferSize())
		}
	}

	fmt.Printf("Final buffer size: %d bytes\n", codec.GetBufferSize())

	// Cleanup
	os.Remove("example_optimized.echoreplay")
	fmt.Println("Optimized codec example completed successfully")
}

// ComparePerformance compares the old vs new implementation performance
func ComparePerformance() {
	fmt.Println("=== Performance Comparison ===")

	// Test with different frame counts
	frameCounts := []int{100, 1000, 10000}

	for _, count := range frameCounts {
		fmt.Printf("\nTesting with %d frames:\n", count)

		// Create test frames
		frames := make([]*rtapi.LobbySessionStateFrame, count)
		for i := range frames {
			frames[i] = &rtapi.LobbySessionStateFrame{
				Timestamp: timestamppb.New(time.Now().Add(time.Duration(i) * time.Millisecond)),
				Session: &apigame.SessionResponse{
					SessionId: uuid.Must(uuid.NewV4()).String(),
				},
			}
		}

		// Test optimized version
		start := time.Now()
		codec, err := NewEchoReplayCodecWriter(fmt.Sprintf("test_optimized_%d.echoreplay", count))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		for _, frame := range frames {
			codec.WriteFrame(frame)
		}
		codec.Close()
		optimizedDuration := time.Since(start)

		// Get file size
		stat, _ := os.Stat(fmt.Sprintf("test_optimized_%d.echoreplay", count))
		fileSize := stat.Size()

		fmt.Printf("  Optimized: %v (%.2f frames/sec, %d bytes)\n",
			optimizedDuration,
			float64(count)/optimizedDuration.Seconds(),
			fileSize)

		// Cleanup
		os.Remove(fmt.Sprintf("test_optimized_%d.echoreplay", count))
	}
}
