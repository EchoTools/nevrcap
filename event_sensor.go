package nevrcap

import "github.com/echotools/nevr-common/v4/gen/go/rtapi"

type EventSensor interface {
	AddFrame(*rtapi.LobbySessionStateFrame) *rtapi.LobbySessionEvent
}
