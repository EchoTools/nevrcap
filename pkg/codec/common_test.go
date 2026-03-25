package codec

import (
	"testing"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func createTestFrame(t *testing.T) *telemetry.LobbySessionStateFrame {
	sessionResponse := &enginev1.SessionResponse{
		SessionId:        "test-session",
		GameStatus:       "running",
		BluePoints:       0,
		OrangePoints:     0,
		BlueRoundScore:   0,
		OrangeRoundScore: 0,
		Teams:            []*enginev1.Team{},
	}

	bonesResponse := &enginev1.PlayerBonesResponse{
		UserBones: []*enginev1.UserBones{},
		ErrCode:   0,
	}

	return &telemetry.LobbySessionStateFrame{
		FrameIndex:  0,
		Timestamp:   timestamppb.Now(),
		Events:      []*telemetry.LobbySessionEvent{},
		Session:     sessionResponse,
		PlayerBones: bonesResponse,
	}
}
