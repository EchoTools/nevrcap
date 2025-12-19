package events

type Sensor interface {
	AddFrame(*telemetry.LobbySessionStateFrame) *telemetry.LobbySessionEvent
}
