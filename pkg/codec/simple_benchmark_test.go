package codec

import (
	"testing"
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func BenchmarkOptimizedWriteFrame(b *testing.B) {
	tempFile := b.TempDir() + "/benchmark.echoreplay"

	codec, err := NewEchoReplayWriter(tempFile)
	if err != nil {
		b.Fatal(err)
	}
	defer codec.Close()

	frame := &telemetry.LobbySessionStateFrame{
		Timestamp: timestamppb.New(time.Now()),
		Session: &enginev1.SessionResponse{
			SessionId: "A1B2C3D4-E5F6-7890-ABCD-EF1234567890",
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

func BenchmarkFixProtojsonUint64Encoding(b *testing.B) {
	// Realistic JSON with multiple players having userids
	input := []byte(`{"sessionid":"ABC-123","rules_changed_at":"1702857600000000000","teams":[{"team":"BLUE","players":[{"name":"Player1","userid":"4355631379520676917","level":50},{"name":"Player2","userid":"1234567890123456789","level":30}]},{"team":"ORANGE","players":[{"name":"Player3","userid":"9876543210987654321","level":45}]}]}`)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_ = FixProtojsonUint64Encoding(input)
	}
}
