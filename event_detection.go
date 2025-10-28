package nevrcap

import (
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
)

var (
	GameStatusPostMatch = "post_match"
	GameStatusRoundOver = "round_over"
)

type detectionFunction func(i int) []*rtapi.LobbySessionEvent

// detectPostMatchEvent checks if a post_match event should be triggered
// Can use the frame ring buffer to analyze previous frames if needed
func (ed *EventDetector) detectPostMatchEvent(i int) []*rtapi.LobbySessionEvent {
	// Check if already in post_match state
	s := ed.frameBuffer[i].GetSession().GetGameStatus()
	if ed.curGameStatus == s {
		return nil
	}

	switch s {
	case GameStatusRoundOver:
		return []*rtapi.LobbySessionEvent{{
			Event: &rtapi.LobbySessionEvent_RoundEnded{
				RoundEnded: &rtapi.RoundEnded{},
			},
		}}
	case GameStatusPostMatch:
		return []*rtapi.LobbySessionEvent{{
			Event: &rtapi.LobbySessionEvent_MatchEnded{
				MatchEnded: &rtapi.MatchEnded{},
			},
		}}
	}

	return nil
}

// detectEvents analyzes frames in the ring buffer and returns detected events
func (ed *EventDetector) detectEvents() []*rtapi.LobbySessionEvent {
	var newEvents []*rtapi.LobbySessionEvent
	newestIndex := ed.latestFrameIndex()
	for _, fn := range [...]detectionFunction{
		ed.detectPostMatchEvent,
	} {
		if events := fn(newestIndex); events != nil {
			newEvents = append(newEvents, events...)
		}
	}

	return newEvents
}
