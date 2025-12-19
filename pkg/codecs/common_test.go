package codecs

import (
	"testing"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func createTestFrame(t *testing.T) *telemetry.LobbySessionStateFrame {
	sessionResponse := &apigame.SessionResponse{
		SessionId:        "test-session",
		GameStatus:       "running",
		BluePoints:       0,
		OrangePoints:     0,
		BlueRoundScore:   0,
		OrangeRoundScore: 0,
		Teams:            []*apigame.Team{},
	}

	bonesResponse := &apigame.PlayerBonesResponse{
		UserBones: []*apigame.UserBones{},
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
