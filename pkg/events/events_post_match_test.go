package events

import (
	"time"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Helper function to create test frames for post_match testing
func createPostMatchTestFrame(gameStatus string, bluePoints, orangePoints int32) *telemetry.LobbySessionStateFrame {
	return &telemetry.LobbySessionStateFrame{
		FrameIndex: 0,
		Timestamp:  timestamppb.New(time.Now()),
		Session: &enginev1.SessionResponse{
			GameStatus:   gameStatus,
			BluePoints:   bluePoints,
			OrangePoints: orangePoints,
		},
	}
}
