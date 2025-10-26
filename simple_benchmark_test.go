package nevrcap

import (
	"os"
	"testing"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"github.com/gofrs/uuid/v5"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func BenchmarkOptimizedWriteFrame(b *testing.B) {
	tempFile := "/tmp/benchmark.echoreplay"
	defer os.Remove(tempFile)

	codec, err := NewEchoReplayCodecWriter(tempFile)
	if err != nil {
		b.Fatal(err)
	}
	defer codec.Close()

	frame := &rtapi.LobbySessionStateFrame{
		Timestamp: timestamppb.New(time.Now()),
		Session: &apigame.SessionResponse{
			SessionId: uuid.Must(uuid.NewV4()).String(),
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
