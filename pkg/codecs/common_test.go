package codecs

import (
	"testing"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func createTestFrame(t *testing.T) *rtapi.LobbySessionStateFrame {
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

	return &rtapi.LobbySessionStateFrame{
		FrameIndex:  0,
		Timestamp:   timestamppb.Now(),
		Events:      []*rtapi.LobbySessionEvent{},
		Session:     sessionResponse,
		PlayerBones: bonesResponse,
	}
}
