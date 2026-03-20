package events

import telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"

type Sensor interface {
	AddFrame(*telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent
}
