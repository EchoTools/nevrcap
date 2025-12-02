package events

import (
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Helper function to create test frames for post_match testing
func createPostMatchTestFrame(gameStatus string, bluePoints, orangePoints int32) *rtapi.LobbySessionStateFrame {
	return &rtapi.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(time.Now()),
		Session: &apigame.SessionResponse{
			GameStatus:   gameStatus,
			BluePoints:   bluePoints,
			OrangePoints: orangePoints,
		},
	}
}
