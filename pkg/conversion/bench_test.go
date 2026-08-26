package conversion

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/echotools/tape/v4/pkg/events"
	"github.com/echotools/tape/v4/pkg/processing"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// richV1Frame returns a realistic v1 frame with session data, disc, multiple
// teams/players, and player bones.
func richV1Frame(idx uint32, baseTime time.Time) *telemetryv1.LobbySessionStateFrame {
	ts := baseTime.Add(time.Duration(idx) * 100 * time.Millisecond)
	return &telemetryv1.LobbySessionStateFrame{
		FrameIndex: idx,
		Timestamp:  timestamppb.New(ts),
		Session: &enginev1.SessionResponse{
			SessionId:    "BENCH-SESSION-CONV",
			GameStatus:   "playing",
			GameClock:    float64(300 - int(idx)),
			BluePoints:   2,
			OrangePoints: 1,
			MapName:      "mpl_arena_a",
			MatchType:    "Echo_Arena",
			Disc: &enginev1.Disc{
				Position:    []float64{0, 5.1, 12.3},
				Forward:     []float64{0, 0, 1},
				Left:        []float64{1, 0, 0},
				Up:          []float64{0, 1, 0},
				Velocity:    []float64{8.2, -1.3, 15.7},
				BounceCount: 1,
			},
			LastThrow: &enginev1.LastThrowInfo{
				ArmSpeed:   12.5,
				TotalSpeed: 18.3,
				RotPerSec:  4.2,
			},
			Teams: []*enginev1.Team{
				{
					TeamName: "BLUE TEAM",
					Players: []*enginev1.TeamMember{
						{
							SlotNumber: 0, DisplayName: "Player1",
							AccountNumber: 4355631379520676917, Level: 50,
							Head: &enginev1.BodyPart{
								Position: []float64{1, 2, 3},
								Forward:  []float64{0, 0, 1},
								Left:     []float64{1, 0, 0},
								Up:       []float64{0, 1, 0},
							},
							Body: &enginev1.BodyPart{
								Position: []float64{1, 1, 3},
								Forward:  []float64{0, 0, 1},
								Left:     []float64{1, 0, 0},
								Up:       []float64{0, 1, 0},
							},
							LeftHand: &enginev1.HandPart{
								Pos:     []float64{0.5, 1.5, 2.8},
								Forward: []float64{0, 0, 1}, Left: []float64{1, 0, 0}, Up: []float64{0, 1, 0},
							},
							RightHand: &enginev1.HandPart{
								Pos:     []float64{1.5, 1.5, 2.8},
								Forward: []float64{0, 0, 1}, Left: []float64{1, 0, 0}, Up: []float64{0, 1, 0},
							},
							Velocity:      []float64{0.5, 0.1, -0.3},
							HasPossession: true,
							Stats:         &enginev1.PlayerStats{Goals: 2, Stuns: 5, Assists: 1, Points: 4},
						},
						{
							SlotNumber: 1, DisplayName: "Player2",
							AccountNumber: 1234567890123456789, Level: 30,
							Head: &enginev1.BodyPart{
								Position: []float64{-3, 1.5, 8},
								Forward:  []float64{0, 0, 1},
								Left:     []float64{1, 0, 0},
								Up:       []float64{0, 1, 0},
							},
							Velocity: []float64{1.2, 0, 0.4},
							Stats:    &enginev1.PlayerStats{Goals: 1, Saves: 3, Points: 2},
						},
					},
				},
				{
					TeamName: "ORANGE TEAM",
					Players: []*enginev1.TeamMember{
						{
							SlotNumber: 2, DisplayName: "Player3",
							AccountNumber: 9876543210987654321, Level: 45,
							Head: &enginev1.BodyPart{
								Position: []float64{5, 2, -4},
								Forward:  []float64{0, 0, -1},
								Left:     []float64{-1, 0, 0},
								Up:       []float64{0, 1, 0},
							},
							Velocity: []float64{-0.8, 0.2, 1.1},
							Stats:    &enginev1.PlayerStats{Goals: 1, Stuns: 8, Steals: 2, Points: 2},
						},
					},
				},
			},
			Player: &enginev1.PlayerRoot{
				VrPosition: []float64{2, 1, 5},
				VrForward:  []float64{0, 0, 1},
				VrLeft:     []float64{1, 0, 0},
				VrUp:       []float64{0, 1, 0},
			},
		},
		PlayerBones: &enginev1.PlayerBonesResponse{
			UserBones: []*enginev1.UserBones{
				{
					PlayerIndex: 0,
					BoneT:       []float32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
					BoneO:       []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1},
				},
				{
					PlayerIndex: 1,
					BoneT:       []float32{-1, -2, -3, 4, 5, 6},
					BoneO:       []float32{0, 0, 0, 1, 0, 0},
				},
			},
		},
	}
}

// BenchmarkMapFrame measures a single v1 -> v2 frame mapping with full player,
// disc, bones, and VR root data.
func BenchmarkMapFrame(b *testing.B) {
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	frame := richV1Frame(42, baseTime)

	b.ReportAllocs()

	for b.Loop() {
		v2, err := MapFrame(frame, baseTime)
		if err != nil {
			b.Fatal(err)
		}
		if v2 == nil {
			b.Fatal("MapFrame returned nil")
		}
	}
}

// BenchmarkMapEvent benchmarks each event type conversion independently.
func BenchmarkMapEvent(b *testing.B) {
	v1Events := map[string]*telemetryv1.LobbySessionEvent{
		"RoundStarted": {Event: &telemetryv1.LobbySessionEvent_RoundStarted{
			RoundStarted: &telemetryv1.RoundStarted{RoundNumber: 2},
		}},
		"RoundEnded": {Event: &telemetryv1.LobbySessionEvent_RoundEnded{
			RoundEnded: &telemetryv1.RoundEnded{RoundNumber: 1, WinningTeam: 1},
		}},
		"MatchEnded": {Event: &telemetryv1.LobbySessionEvent_MatchEnded{
			MatchEnded: &telemetryv1.MatchEnded{WinningTeam: 2},
		}},
		"PlayerJoined": {Event: &telemetryv1.LobbySessionEvent_PlayerJoined{
			PlayerJoined: &telemetryv1.PlayerJoined{
				Player: &enginev1.TeamMember{SlotNumber: 0, DisplayName: "P1", AccountNumber: 12345},
				Role:   1,
			},
		}},
		"PlayerLeft": {Event: &telemetryv1.LobbySessionEvent_PlayerLeft{
			PlayerLeft: &telemetryv1.PlayerLeft{PlayerSlot: 0, DisplayName: "P1"},
		}},
		"DiscThrown": {Event: &telemetryv1.LobbySessionEvent_DiscThrown{
			DiscThrown: &telemetryv1.DiscThrown{
				PlayerSlot: 0,
				ThrowDetails: &enginev1.LastThrowInfo{
					ArmSpeed: 12.5, TotalSpeed: 18.3, RotPerSec: 4.2,
					OffAxisSpinDeg: 2.1, WristThrowPenalty: 0.05,
					PotSpeedFromRot: 3.1, SpeedFromArm: 8.4,
					SpeedFromMovement: 1.2, SpeedFromWrist: 6.7,
				},
			},
		}},
		"DiscCaught": {Event: &telemetryv1.LobbySessionEvent_DiscCaught{
			DiscCaught: &telemetryv1.DiscCaught{PlayerSlot: 1},
		}},
		"GoalScored": {Event: &telemetryv1.LobbySessionEvent_GoalScored{
			GoalScored: &telemetryv1.GoalScored{
				ScoreDetails: &enginev1.LastScore{
					DiscSpeed: 18.5, Team: "blue", GoalType: "LONG SHOT",
					PointAmount: 3, DistanceThrown: 25.4,
					PersonScored: "Player1", AssistScored: "Player2",
				},
			},
		}},
		"ScoreboardUpdated": {Event: &telemetryv1.LobbySessionEvent_ScoreboardUpdated{
			ScoreboardUpdated: &telemetryv1.ScoreboardUpdated{
				BluePoints: 3, OrangePoints: 2,
				BlueRoundScore: 1, OrangeRoundScore: 0,
				GameClockDisplay: "2:30",
			},
		}},
		"PlayerGoal": {Event: &telemetryv1.LobbySessionEvent_PlayerGoal{
			PlayerGoal: &telemetryv1.PlayerGoal{PlayerSlot: 0, TotalGoals: 3, Points: 6},
		}},
		"PlayerStun": {Event: &telemetryv1.LobbySessionEvent_PlayerStun{
			PlayerStun: &telemetryv1.PlayerStun{PlayerSlot: 2, TotalStuns: 10},
		}},
	}

	for name, v1e := range v1Events {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				v2 := mapEvent(v1e, nil)
				if v2 == nil {
					b.Fatalf("mapEvent returned nil for %s", name)
				}
			}
		})
	}
}

// BenchmarkFullPipeline simulates the end-to-end hot path: read .echoreplay
// frame, detect events, map to v2. Uses synthetic data to avoid file dependency.
func BenchmarkFullPipeline(b *testing.B) {
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Build a 100-frame sequence with varied game states.
	const nFrames = 100
	frames := make([]*telemetryv1.LobbySessionStateFrame, nFrames)
	for i := range nFrames {
		frames[i] = richV1Frame(uint32(i), baseTime)
		switch {
		case i < 5:
			frames[i].Session.GameStatus = "pre_match"
		case i == 5:
			frames[i].Session.GameStatus = "round_start"
		case i < 40:
			frames[i].Session.GameStatus = "playing"
		case i == 40:
			frames[i].Session.GameStatus = "score"
			frames[i].Session.BluePoints = 3
		case i < 60:
			frames[i].Session.GameStatus = "playing"
			frames[i].Session.BluePoints = 3
		case i == 60:
			frames[i].Session.GameStatus = "round_over"
		case i == 61:
			frames[i].Session.GameStatus = "round_start"
		case i < 90:
			frames[i].Session.GameStatus = "playing"
		case i == 90:
			frames[i].Session.GameStatus = "score"
			frames[i].Session.OrangePoints = 2
		default:
			frames[i].Session.GameStatus = "post_match"
		}
	}

	b.ReportAllocs()

	for b.Loop() {
		detector := events.NewWithDefaultSensors(events.WithSynchronousProcessing())
		processor := processing.NewWithDetector(detector)
		mapper := &FrameMapper{BaseTime: baseTime}

		for _, frame := range frames {
			// Detect events.
			processor.DetectEvents(frame)
			// Drain events onto frame.
			for {
				select {
				case evts := <-processor.EventsChan():
					frame.Events = append(frame.Events, evts...)
				default:
					goto done
				}
			}
		done:
			// Map to v2.
			if _, err := mapper.MapFrame(frame); err != nil {
				b.Fatal(err)
			}
			// Reset events for reuse in next iteration.
			frame.Events = frame.Events[:0]
		}

		processor.Stop()
	}
}

// BenchmarkConvertEchoReplay benchmarks the full ConvertFile pipeline from a
// real .echoreplay file.
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

// BenchmarkConvertNevrcap benchmarks nevrcap -> tape conversion with synthetic data.
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
