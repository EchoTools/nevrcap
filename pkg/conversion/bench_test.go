package conversion

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func BenchmarkConvertEchoReplay(b *testing.B) {
	const samplePath = "../../testdata/sample.echoreplay"
	if _, err := os.Stat(samplePath); os.IsNotExist(err) {
		b.Skip("testdata/sample.echoreplay not available")
	}

	dir := b.TempDir()

	b.ResetTimer()
	for i := range b.N {
		outputPath := filepath.Join(dir, "bench_"+itoa(i)+".tape")
		result, err := ConvertFile(samplePath, outputPath)
		if err != nil {
			b.Fatalf("ConvertFile: %v", err)
		}
		b.ReportMetric(float64(result.FrameCount), "frames/op")
		// Clean up to avoid filling temp dir.
		os.Remove(outputPath) //nolint:errcheck // best-effort cleanup in benchmark
	}
}

func BenchmarkConvertNevrcap(b *testing.B) {
	// Create a synthetic nevrcap with 1000 frames.
	dir := b.TempDir()
	inputPath := filepath.Join(dir, "bench.nevrcap")

	now := timestamppb.Now()
	header := &telemetryv1.TelemetryHeader{
		CaptureId: "bench",
		CreatedAt: now,
	}

	const frameCount = 1000
	frames := make([]*telemetryv1.LobbySessionStateFrame, frameCount)
	for i := range frameCount {
		ts := now.AsTime().Add(100 * 1000 * 1000 * int64ToDuration(int64(i)))
		frames[i] = &telemetryv1.LobbySessionStateFrame{
			FrameIndex: uint32(i), //nolint:gosec // bench index fits uint32
			Timestamp:  timestamppb.New(ts),
			Session: &enginev1.SessionResponse{
				SessionId:  "bench-session",
				GameStatus: "playing",
				BluePoints: 2,
				GameClock:  float64(300 - i),
				Teams: []*enginev1.Team{
					{
						TeamName: "BLUE TEAM",
						Players: []*enginev1.TeamMember{
							{
								SlotNumber:  0,
								DisplayName: "Player1",
								Head: &enginev1.BodyPart{
									Position: []float64{1.0, 2.0, 3.0},
									Forward:  []float64{0, 0, 1},
									Left:     []float64{1, 0, 0},
									Up:       []float64{0, 1, 0},
								},
								Velocity: []float64{0.1, 0.2, 0.3},
							},
						},
					},
					{
						TeamName: "ORANGE TEAM",
						Players: []*enginev1.TeamMember{
							{
								SlotNumber:  1,
								DisplayName: "Player2",
							},
						},
					},
				},
				Disc: &enginev1.Disc{
					Position:    []float64{0, 5, 0},
					Forward:     []float64{0, 0, 1},
					Left:        []float64{1, 0, 0},
					Up:          []float64{0, 1, 0},
					Velocity:    []float64{1, 2, 3},
					BounceCount: 0,
				},
			},
		}
	}

	writeLegacyV1FileForBench(b, inputPath, header, frames...)

	b.ResetTimer()
	for i := range b.N {
		outputPath := filepath.Join(dir, "out_"+itoa(i)+".tape")
		_, err := ConvertFile(inputPath, outputPath)
		if err != nil {
			b.Fatalf("ConvertFile: %v", err)
		}
		os.Remove(outputPath) //nolint:errcheck // best-effort cleanup in benchmark
	}
	b.ReportMetric(float64(frameCount), "frames/op")
}

// itoa is a simple int-to-string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte(n%10) + '0'
		n /= 10
	}
	return string(buf[i:])
}

func int64ToDuration(n int64) time.Duration {
	return time.Duration(n)
}

// writeLegacyV1FileForBench is like writeLegacyV1File but accepts *testing.B.
func writeLegacyV1FileForBench(b *testing.B, filename string, header *telemetryv1.TelemetryHeader, frames ...*telemetryv1.LobbySessionStateFrame) {
	b.Helper()

	f, err := os.Create(filename)
	if err != nil {
		b.Fatalf("create file: %v", err)
	}

	enc, encErr := zstd.NewWriter(f, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if encErr != nil {
		f.Close() //nolint:errcheck // test helper
		b.Fatalf("create zstd encoder: %v", encErr)
	}

	writeMsg := func(msg proto.Message) {
		b.Helper()
		data, marshalErr := proto.Marshal(msg)
		if marshalErr != nil {
			b.Fatalf("marshal: %v", marshalErr)
		}
		var buf [10]byte
		length := uint64(len(data))
		i := 0
		for length >= 0x80 {
			buf[i] = byte(length) | 0x80
			length >>= 7
			i++
		}
		buf[i] = byte(length)
		i++
		if _, writeErr := enc.Write(buf[:i]); writeErr != nil {
			b.Fatalf("write varint: %v", writeErr)
		}
		if _, writeErr := enc.Write(data); writeErr != nil {
			b.Fatalf("write data: %v", writeErr)
		}
	}

	writeMsg(header)
	for _, frame := range frames {
		writeMsg(frame)
	}

	if err := enc.Close(); err != nil {
		b.Fatalf("close encoder: %v", err)
	}
	if err := f.Close(); err != nil {
		b.Fatalf("close file: %v", err)
	}
}
