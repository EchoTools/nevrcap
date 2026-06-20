package events

import telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"

type Sensor interface {
	AddFrame(*telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent
}

// Resettable is an optional interface that sensors can implement to clear
// their state when the detector is reset between sessions.
type Resettable interface {
	Reset()
}
