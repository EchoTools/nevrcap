package conversion

import (
	"os"
	"testing"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// writeLegacyV1File writes a v1-format file (zstd-compressed,
// varint-length-delimited protobuf messages) for testing.
func writeLegacyV1File(t *testing.T, filename string, header *telemetry.TelemetryHeader, frames ...*telemetry.LobbySessionStateFrame) {
	t.Helper()

	f, err := os.Create(filename)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	enc, err := zstd.NewWriter(f, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		f.Close()
		t.Fatalf("create zstd encoder: %v", err)
	}

	writeMsg := func(msg proto.Message) {
		t.Helper()
		data, err := proto.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
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
		if _, err := enc.Write(buf[:i]); err != nil {
			t.Fatalf("write varint: %v", err)
		}
		if _, err := enc.Write(data); err != nil {
			t.Fatalf("write data: %v", err)
		}
	}

	writeMsg(header)
	for _, frame := range frames {
		writeMsg(frame)
	}

	if err := enc.Close(); err != nil {
		t.Fatalf("close encoder: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
}

func createTestFrame(t *testing.T) *telemetry.LobbySessionStateFrame {
	t.Helper()
	return &telemetry.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.Now(),
		Events:     []*telemetry.LobbySessionEvent{},
		Session: &enginev1.SessionResponse{
			SessionId:        "test-session",
			GameStatus:       "running",
			BluePoints:       0,
			OrangePoints:     0,
			BlueRoundScore:   0,
			OrangeRoundScore: 0,
			Teams:            []*enginev1.Team{},
		},
		PlayerBones: &enginev1.PlayerBonesResponse{
			UserBones: []*enginev1.UserBones{},
			ErrCode:   0,
		},
	}
}
