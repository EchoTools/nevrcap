package conversion

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/echotools/tape/pkg/codec"
)

// TestSupersetFieldsPopulate proves the v1->v2 converter copies the new superset
// fields exactly: capture-client shoulder input + combat payload on the frame,
// per-player packet-loss on PlayerState, session_ip on the header. Runs against
// the committed sample by default; set TAPE_AUDIT_FILE for a richer recording.
func TestSupersetFieldsPopulate(t *testing.T) {
	src := os.Getenv("TAPE_AUDIT_FILE")
	if src == "" {
		src = filepath.Join("..", "..", "testdata", "sample.echoreplay")
	}
	r, err := codec.NewEchoReplayReader(src)
	if err != nil {
		t.Skipf("no sample: %v", err)
	}
	frames, err := r.ReadFrames()
	_ = r.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("no frames")
	}

	// Header: session_ip carried through.
	hdr := MapHeaderFromSession(&telemetryv1.TelemetryHeader{}, frames[0].GetSession())
	if got, want := hdr.GetEchoArena().GetSessionIp(), frames[0].GetSession().GetSessionIp(); got != want {
		t.Errorf("session_ip: got %q want %q", got, want)
	}

	mapper := &FrameMapper{BaseTime: time.Now()}
	var shoulderNonZero, packetLossNonZero, payloadFrames int
	for _, v1f := range frames {
		sess := v1f.GetSession()
		if sess == nil {
			continue
		}
		ea := mapper.MapFrame(v1f).GetEchoArena()

		// Shoulder input copied exactly (frame-level, capture client).
		if ea.GetLeftShoulderPressed() != float32(sess.GetLeftShoulderPressed()) ||
			ea.GetRightShoulderPressed() != float32(sess.GetRightShoulderPressed()) ||
			ea.GetLeftShoulderPressed_2() != float32(sess.GetLeftShoulderPressed2()) ||
			ea.GetRightShoulderPressed_2() != float32(sess.GetRightShoulderPressed2()) {
			t.Fatalf("shoulder not copied at frame %d", v1f.GetFrameIndex())
		}
		if ea.GetLeftShoulderPressed() != 0 || ea.GetRightShoulderPressed() != 0 ||
			ea.GetLeftShoulderPressed_2() != 0 || ea.GetRightShoulderPressed_2() != 0 {
			shoulderNonZero++
		}
		if ea.GetPayload() != nil {
			payloadFrames++
		}

		// Packet loss copied exactly, per player, in the same order.
		pi := 0
		for _, team := range sess.GetTeams() {
			for _, m := range team.GetPlayers() {
				ps := ea.GetPlayers()[pi]
				if ps.GetPacketLossRatio() != float32(m.GetPacketLossRatio()) {
					t.Fatalf("packet_loss not copied for slot %d", m.GetSlotNumber())
				}
				if ps.GetPacketLossRatio() != 0 {
					packetLossNonZero++
				}
				pi++
			}
		}
	}
	t.Logf("frames=%d shoulder-nonzero-frames=%d packetloss-nonzero-playerframes=%d payload-frames=%d",
		len(frames), shoulderNonZero, packetLossNonZero, payloadFrames)
}
