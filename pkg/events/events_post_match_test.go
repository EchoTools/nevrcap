package events

import (
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Helper function to create test frames for post_match testing
func createPostMatchTestFrame(gameStatus string, bluePoints, orangePoints int32) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(time.Now()),
		Session: &apigame.SessionResponse{
			GameStatus:   gameStatus,
			BluePoints:   bluePoints,
			OrangePoints: orangePoints,
		},
	}
}
