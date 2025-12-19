package events

import (
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
)

var (
	GameStatusPostMatch = "post_match"
	GameStatusRoundOver = "round_over"
)

type detectionFunction func(i int, dst []*telemetry.LobbySessionEvent) []*telemetry.LobbySessionEvent

// detectPostMatchEvent checks if a post_match event should be triggered
// Can use the frame ring buffer to analyze previous frames if needed
func (ed *AsyncDetector) detectPostMatchEvent(i int, dst []*telemetry.LobbySessionEvent) []*telemetry.LobbySessionEvent {
	// Guard against invalid index
	if i < 0 || i >= len(ed.frameBuffer) {
		return dst
	}

	frame := ed.frameBuffer[i]
	if frame == nil || frame.GetSession() == nil {
		return dst
	}

	curStatus := frame.GetSession().GetGameStatus()

	// Check previous game status to detect transitions
	if ed.previousGameStatusFrame != nil && ed.previousGameStatusFrame.GetSession() != nil {
		prevStatus := ed.previousGameStatusFrame.GetSession().GetGameStatus()
		if prevStatus == curStatus {
			return dst // No transition
		}
	}

	// Update previous frame reference
	ed.previousGameStatusFrame = frame

	switch curStatus {
	case GameStatusRoundOver:
		return append(dst, &telemetry.LobbySessionEvent{
			Event: &telemetry.LobbySessionEvent_RoundEnded{
				RoundEnded: &rtapi.RoundEnded{},
			},
		})
	case GameStatusPostMatch:
		return append(dst, &telemetry.LobbySessionEvent{
			Event: &telemetry.LobbySessionEvent_MatchEnded{
				MatchEnded: &rtapi.MatchEnded{},
			},
		})
	}

	return dst
}
