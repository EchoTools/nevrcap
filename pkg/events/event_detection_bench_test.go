package events

import (
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// realisticFrameSequence builds a 20-frame sequence that exercises multiple
// sensor paths: player joins, possession changes, throws, goals, round/match
// transitions, and stat changes.
func realisticFrameSequence() []*telemetry.LobbySessionStateFrame {
	baseTime := time.Date(2025, 3, 15, 14, 30, 0, 0, time.UTC)

	blueTeam := func(players ...*enginev1.TeamMember) *enginev1.Team {
		return &enginev1.Team{TeamName: "BLUE TEAM", Players: players}
	}
	orangeTeam := func(players ...*enginev1.TeamMember) *enginev1.Team {
		return &enginev1.Team{TeamName: "ORANGE TEAM", Players: players}
	}

	p1 := func(possession bool, stats *enginev1.PlayerStats) *enginev1.TeamMember {
		return &enginev1.TeamMember{
			SlotNumber: 0, DisplayName: "Player1", AccountNumber: 111,
			HasPossession: possession,
			Head: &enginev1.BodyPart{
				Position: []float64{1, 2, 3}, Forward: []float64{0, 0, 1},
				Left: []float64{1, 0, 0}, Up: []float64{0, 1, 0},
			},
			Velocity: []float64{0.5, 0, -0.3},
			Stats:    stats,
		}
	}
	p2 := func(possession bool, stats *enginev1.PlayerStats) *enginev1.TeamMember {
		return &enginev1.TeamMember{
			SlotNumber: 1, DisplayName: "Player2", AccountNumber: 222,
			HasPossession: possession,
			Head: &enginev1.BodyPart{
				Position: []float64{-3, 1.5, 8}, Forward: []float64{0, 0, 1},
				Left: []float64{1, 0, 0}, Up: []float64{0, 1, 0},
			},
			Velocity: []float64{1.2, 0, 0.4},
			Stats:    stats,
		}
	}
	p3 := func(possession bool, stats *enginev1.PlayerStats) *enginev1.TeamMember {
		return &enginev1.TeamMember{
			SlotNumber: 2, DisplayName: "Player3", AccountNumber: 333,
			HasPossession: possession,
			Head: &enginev1.BodyPart{
				Position: []float64{5, 2, -4}, Forward: []float64{0, 0, -1},
				Left: []float64{-1, 0, 0}, Up: []float64{0, 1, 0},
			},
			Velocity: []float64{-0.8, 0.2, 1.1},
			Stats:    stats,
		}
	}

	mkFrame := func(idx uint32, status string, blue, orange int32,
		teams []*enginev1.Team, lastThrow *enginev1.LastThrowInfo,
		disc *enginev1.Disc,
	) *telemetry.LobbySessionStateFrame {
		return &telemetry.LobbySessionStateFrame{
			FrameIndex: idx,
			Timestamp:  timestamppb.New(baseTime.Add(time.Duration(idx) * 100 * time.Millisecond)),
			Session: &enginev1.SessionResponse{
				SessionId:    "BENCH-SESSION",
				GameStatus:   status,
				GameClock:    float64(300 - int(idx)),
				BluePoints:   blue,
				OrangePoints: orange,
				Teams:        teams,
				LastThrow:    lastThrow,
				Disc:         disc,
			},
		}
	}

	defaultDisc := &enginev1.Disc{
		Position: []float64{0, 5, 12}, Forward: []float64{0, 0, 1},
		Left: []float64{1, 0, 0}, Up: []float64{0, 1, 0},
		Velocity: []float64{0, 0, 0},
	}

	stats0 := &enginev1.PlayerStats{}

	throw1 := &enginev1.LastThrowInfo{ArmSpeed: 12.5, TotalSpeed: 18.3, RotPerSec: 4.2}
	throw2 := &enginev1.LastThrowInfo{ArmSpeed: 10.1, TotalSpeed: 14.7, RotPerSec: 3.8}

	return []*telemetry.LobbySessionStateFrame{
		// 0: pre_match, 2 blue players join
		mkFrame(0, "pre_match", 0, 0,
			[]*enginev1.Team{blueTeam(p1(false, stats0), p2(false, stats0)), orangeTeam()},
			nil, defaultDisc),
		// 1: pre_match, orange player joins
		mkFrame(1, "pre_match", 0, 0,
			[]*enginev1.Team{blueTeam(p1(false, stats0), p2(false, stats0)), orangeTeam(p3(false, stats0))},
			nil, defaultDisc),
		// 2: round_start -> round begins
		mkFrame(2, "round_start", 0, 0,
			[]*enginev1.Team{blueTeam(p1(false, stats0), p2(false, stats0)), orangeTeam(p3(false, stats0))},
			nil, defaultDisc),
		// 3: playing, p1 grabs disc
		mkFrame(3, "playing", 0, 0,
			[]*enginev1.Team{blueTeam(p1(true, stats0), p2(false, stats0)), orangeTeam(p3(false, stats0))},
			nil, defaultDisc),
		// 4: playing, p1 throws disc
		mkFrame(4, "playing", 0, 0,
			[]*enginev1.Team{blueTeam(p1(false, stats0), p2(false, stats0)), orangeTeam(p3(false, stats0))},
			throw1, defaultDisc),
		// 5: playing, p2 catches disc
		mkFrame(5, "playing", 0, 0,
			[]*enginev1.Team{blueTeam(p1(false, stats0), p2(true, stats0)), orangeTeam(p3(false, stats0))},
			throw1, defaultDisc),
		// 6: playing, p2 throws
		mkFrame(6, "playing", 0, 0,
			[]*enginev1.Team{blueTeam(p1(false, stats0), p2(false, stats0)), orangeTeam(p3(false, stats0))},
			throw2, defaultDisc),
		// 7: playing, steady state
		mkFrame(7, "playing", 0, 0,
			[]*enginev1.Team{blueTeam(p1(false, stats0), p2(false, stats0)), orangeTeam(p3(false, stats0))},
			throw2, defaultDisc),
		// 8: score! blue scores, stat changes
		mkFrame(8, "score", 1, 0,
			[]*enginev1.Team{
				blueTeam(p1(false, &enginev1.PlayerStats{Goals: 1, Points: 2}), p2(false, stats0)),
				orangeTeam(p3(false, stats0)),
			}, throw2, defaultDisc),
		// 9: playing resumes
		mkFrame(9, "playing", 1, 0,
			[]*enginev1.Team{
				blueTeam(p1(false, &enginev1.PlayerStats{Goals: 1, Points: 2}), p2(false, stats0)),
				orangeTeam(p3(false, stats0)),
			}, throw2, defaultDisc),
		// 10: playing, p3 gets disc (steal)
		mkFrame(10, "playing", 1, 0,
			[]*enginev1.Team{
				blueTeam(p1(false, &enginev1.PlayerStats{Goals: 1, Points: 2}), p2(false, stats0)),
				orangeTeam(p3(true, &enginev1.PlayerStats{Steals: 1})),
			}, throw2, defaultDisc),
		// 11: playing, p3 scores
		mkFrame(11, "score", 1, 1,
			[]*enginev1.Team{
				blueTeam(p1(false, &enginev1.PlayerStats{Goals: 1, Points: 2}), p2(false, stats0)),
				orangeTeam(p3(false, &enginev1.PlayerStats{Goals: 1, Points: 2, Steals: 1})),
			}, throw2, defaultDisc),
		// 12: playing again
		mkFrame(12, "playing", 1, 1,
			[]*enginev1.Team{
				blueTeam(p1(false, &enginev1.PlayerStats{Goals: 1, Points: 2}), p2(false, stats0)),
				orangeTeam(p3(false, &enginev1.PlayerStats{Goals: 1, Points: 2, Steals: 1})),
			}, throw2, defaultDisc),
		// 13-16: steady playing
		mkFrame(13, "playing", 1, 1,
			[]*enginev1.Team{
				blueTeam(p1(true, &enginev1.PlayerStats{Goals: 1, Points: 2}), p2(false, &enginev1.PlayerStats{Assists: 1})),
				orangeTeam(p3(false, &enginev1.PlayerStats{Goals: 1, Points: 2, Steals: 1, Stuns: 1})),
			}, throw2, defaultDisc),
		mkFrame(14, "playing", 1, 1,
			[]*enginev1.Team{
				blueTeam(p1(true, &enginev1.PlayerStats{Goals: 1, Points: 2}), p2(false, &enginev1.PlayerStats{Assists: 1})),
				orangeTeam(p3(false, &enginev1.PlayerStats{Goals: 1, Points: 2, Steals: 1, Stuns: 1})),
			}, throw2, defaultDisc),
		mkFrame(15, "playing", 1, 1,
			[]*enginev1.Team{
				blueTeam(p1(true, &enginev1.PlayerStats{Goals: 1, Points: 2}), p2(false, &enginev1.PlayerStats{Assists: 1})),
				orangeTeam(p3(false, &enginev1.PlayerStats{Goals: 1, Points: 2, Steals: 1, Stuns: 1})),
			}, throw2, defaultDisc),
		mkFrame(16, "playing", 2, 1,
			[]*enginev1.Team{
				blueTeam(p1(false, &enginev1.PlayerStats{Goals: 2, Points: 4}), p2(false, &enginev1.PlayerStats{Assists: 1})),
				orangeTeam(p3(false, &enginev1.PlayerStats{Goals: 1, Points: 2, Steals: 1, Stuns: 1})),
			}, throw2, defaultDisc),
		// 17: round_over
		mkFrame(17, "round_over", 2, 1,
			[]*enginev1.Team{
				blueTeam(p1(false, &enginev1.PlayerStats{Goals: 2, Points: 4}), p2(false, &enginev1.PlayerStats{Assists: 1})),
				orangeTeam(p3(false, &enginev1.PlayerStats{Goals: 1, Points: 2, Steals: 1, Stuns: 1})),
			}, throw2, defaultDisc),
		// 18: post_match
		mkFrame(18, "post_match", 3, 1,
			[]*enginev1.Team{
				blueTeam(p1(false, &enginev1.PlayerStats{Goals: 2, Points: 4}), p2(false, &enginev1.PlayerStats{Assists: 1})),
				orangeTeam(p3(false, &enginev1.PlayerStats{Goals: 1, Points: 2, Steals: 1, Stuns: 1})),
			}, throw2, defaultDisc),
		// 19: post_match (duplicate, should be no-op)
		mkFrame(19, "post_match", 3, 1,
			[]*enginev1.Team{
				blueTeam(p1(false, &enginev1.PlayerStats{Goals: 2, Points: 4}), p2(false, &enginev1.PlayerStats{Assists: 1})),
				orangeTeam(p3(false, &enginev1.PlayerStats{Goals: 1, Points: 2, Steals: 1, Stuns: 1})),
			}, throw2, defaultDisc),
	}
}

// BenchmarkDetectEvents runs the full sensor pipeline on a realistic 20-frame
// sequence covering: player joins, possession changes, throws, catches, goals,
// stat deltas, round transitions, and match end.
func BenchmarkDetectEvents(b *testing.B) {
	frames := realisticFrameSequence()

	b.ReportAllocs()

	for b.Loop() {
		detector := NewWithDefaultSensors(WithSynchronousProcessing())

		// Drain events in-line since we're in synchronous mode.
		for _, frame := range frames {
			detector.ProcessFrame(frame)
			// Drain events channel.
			for {
				select {
				case <-detector.EventsChan():
				default:
					goto drained
				}
			}
		drained:
		}

		detector.Stop()
	}
}

// BenchmarkSensorDrain measures the overhead of the pending-queue drain pattern
// used by sensors that can emit multiple events per frame (e.g. PlayerJoinSensor
// when multiple players appear simultaneously).
func BenchmarkSensorDrain(b *testing.B) {
	// Build a frame where 3 players appear at once (pending queue).
	frame := &telemetry.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(time.Now()),
		Session: &enginev1.SessionResponse{
			GameStatus: "playing",
			Teams: []*enginev1.Team{
				{
					TeamName: "BLUE TEAM",
					Players: []*enginev1.TeamMember{
						{SlotNumber: 0, DisplayName: "A", AccountNumber: 100},
						{SlotNumber: 1, DisplayName: "B", AccountNumber: 200},
					},
				},
				{
					TeamName: "ORANGE TEAM",
					Players: []*enginev1.TeamMember{
						{SlotNumber: 2, DisplayName: "C", AccountNumber: 300},
					},
				},
			},
		},
	}

	b.ReportAllocs()

	for b.Loop() {
		sensor := NewPlayerJoinSensor()
		// First call queues events; subsequent calls drain.
		for {
			evt := sensor.AddFrame(frame)
			if evt == nil {
				break
			}
		}
	}
}

// BenchmarkProtoEqual measures proto.Equal performance on LastThrowInfo,
// which is used by DiscThrownSensor to detect new throws.
func BenchmarkProtoEqual(b *testing.B) {
	a := &enginev1.LastThrowInfo{
		ArmSpeed:                12.5,
		TotalSpeed:              18.3,
		OffAxisSpinDeg:          2.1,
		WristThrowPenalty:       0.05,
		RotPerSec:               4.2,
		PotSpeedFromRot:         3.1,
		SpeedFromArm:            8.4,
		SpeedFromMovement:       1.2,
		SpeedFromWrist:          6.7,
		WristAlignToThrowDeg:    15.3,
		ThrowAlignToMovementDeg: 22.1,
		OffAxisPenalty:          0.03,
		ThrowMovePenalty:        0.08,
	}
	bb := proto.Clone(a).(*enginev1.LastThrowInfo)

	b.Run("Equal", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if !proto.Equal(a, bb) {
				b.Fatal("expected equal")
			}
		}
	})

	b.Run("NotEqual", func(b *testing.B) {
		c := proto.Clone(a).(*enginev1.LastThrowInfo)
		c.ArmSpeed = 99.9
		b.ReportAllocs()
		for b.Loop() {
			if proto.Equal(a, c) {
				b.Fatal("expected not equal")
			}
		}
	})

	b.Run("ManualFieldCompare", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = manualLastThrowEqual(a, bb)
		}
	})
}

// manualLastThrowEqual is a hand-written field comparison baseline to compare
// against proto.Equal.
func manualLastThrowEqual(a, b *enginev1.LastThrowInfo) bool {
	return a.GetArmSpeed() == b.GetArmSpeed() &&
		a.GetTotalSpeed() == b.GetTotalSpeed() &&
		a.GetOffAxisSpinDeg() == b.GetOffAxisSpinDeg() &&
		a.GetWristThrowPenalty() == b.GetWristThrowPenalty() &&
		a.GetRotPerSec() == b.GetRotPerSec() &&
		a.GetPotSpeedFromRot() == b.GetPotSpeedFromRot() &&
		a.GetSpeedFromArm() == b.GetSpeedFromArm() &&
		a.GetSpeedFromMovement() == b.GetSpeedFromMovement() &&
		a.GetSpeedFromWrist() == b.GetSpeedFromWrist() &&
		a.GetWristAlignToThrowDeg() == b.GetWristAlignToThrowDeg() &&
		a.GetThrowAlignToMovementDeg() == b.GetThrowAlignToMovementDeg() &&
		a.GetOffAxisPenalty() == b.GetOffAxisPenalty() &&
		a.GetThrowMovePenalty() == b.GetThrowMovePenalty()
}
