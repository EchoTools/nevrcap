package conversion

import (
	"testing"
	"time"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestReconstructPreservesBonesPresence is the CANONICAL-001 §3 guard.
// A capture where bones were recorded on any frame must emit a 3-field
// line for every frame, including gap frames (empty PlayerBonesResponse).
// A capture where bones were never recorded must emit bare 2-field lines.
func TestReconstructPreservesBonesPresence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(10 * time.Second)
	base := now.Add(-10 * time.Second)

	boneFrame := func(slot int32) *capturepb.Frame {
		return &capturepb.Frame{
			FrameIndex: uint32(slot), //nolint:gosec // test constant
			Payload: &capturepb.Frame_EchoArena{
				EchoArena: &capturepb.EchoArenaFrame{
					PlayerBones: []*capturepb.PlayerBones{{
						Slot: slot,
					}},
				},
			},
		}
	}

	noBonesFrame := func(idx uint32) *capturepb.Frame {
		return &capturepb.Frame{
			FrameIndex:        idx,
			TimestampOffsetMs: 0,
			Payload: &capturepb.Frame_EchoArena{
				EchoArena: &capturepb.EchoArenaFrame{},
			},
		}
	}

	t.Run("hasBones: gap frames get an empty PlayerBonesResponse", func(t *testing.T) {
		t.Parallel()

		hdr := &capturepb.CaptureHeader{
			CreatedAt: timestamppb.New(now),
			GameHeader: &capturepb.CaptureHeader_EchoArena{
				EchoArena: &capturepb.EchoArenaHeader{},
			},
		}
		frames := []*capturepb.Frame{
			noBonesFrame(0),
			boneFrame(1),
			noBonesFrame(2),
		}

		rc := &SessionReconstructor{
			header:   hdr,
			ea:       hdr.GetEchoArena(),
			session:  NewSession(hdr.GetEchoArena(), frames),
			frames:   frames,
			baseTime: base,
			hasBones: true,
		}

		// Frame 0: gap — still carries PlayerBones so the writer emits 3-field.
		f0 := rc.ReconstructFrame(0)
		if f0.GetPlayerBones() == nil {
			t.Fatal("frame 0 (gap): expected non-nil PlayerBones when hasBones is true")
		}
		if len(f0.GetPlayerBones().GetUserBones()) != 0 {
			t.Error("frame 0 (gap): PlayerBonesResponse should have zero user_bones")
		}

		// Frame 1: populated — bones survive.
		f1 := rc.ReconstructFrame(1)
		if f1.GetPlayerBones() == nil {
			t.Fatal("frame 1 (populated): expected non-nil PlayerBones")
		}

		// Frame 2: gap — same as frame 0.
		f2 := rc.ReconstructFrame(2)
		if f2.GetPlayerBones() == nil {
			t.Fatal("frame 2 (gap): expected non-nil PlayerBones when hasBones is true")
		}
	})

	t.Run("no bones: all frames get nil PlayerBones", func(t *testing.T) {
		t.Parallel()

		hdr := &capturepb.CaptureHeader{
			CreatedAt: timestamppb.New(now),
			GameHeader: &capturepb.CaptureHeader_EchoArena{
				EchoArena: &capturepb.EchoArenaHeader{},
			},
		}
		frames := []*capturepb.Frame{
			noBonesFrame(0),
			noBonesFrame(1),
		}

		rc := &SessionReconstructor{
			header:   hdr,
			ea:       hdr.GetEchoArena(),
			session:  NewSession(hdr.GetEchoArena(), frames),
			frames:   frames,
			baseTime: base,
			hasBones: false,
		}

		for i := range 2 {
			if f := rc.ReconstructFrame(i); f.GetPlayerBones() != nil {
				t.Fatalf("frame %d: expected nil PlayerBones, got non-nil", i)
			}
		}
	})
}
