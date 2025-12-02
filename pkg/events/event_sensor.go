package events

import "github.com/echotools/nevr-common/v4/gen/go/rtapi"

type Sensor interface {
	AddFrame(*rtapi.LobbySessionStateFrame) *rtapi.LobbySessionEvent
}
