package events

import "github.com/echotools/nevr-common/v4/gen/go/telemetry/v1"

type Sensor interface {
	AddFrame(*telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent
}
