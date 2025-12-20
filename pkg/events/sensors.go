package events

// DefaultSensors returns all available event sensors
func DefaultSensors() []Sensor {
	return []Sensor{
		// Player events
		NewPlayerJoinSensor(),
		NewPlayerLeaveSensor(),
		NewPlayerTeamSwitchSensor(),
		NewEmoteSensor(),

		// Scoreboard events
		NewScoreboardSensor(),
		NewGoalScoredSensor(),

		// Disc events
		NewDiscPossessionSensor(),
		NewDiscThrownSensor(),
		NewDiscCaughtSensor(),

		// Stat events
		NewStatEventSensor(),

		// Game state events
		NewRoundStartSensor(),
		NewRoundEndSensor(),
		NewMatchEndSensor(),
		NewPauseSensor(),
	}
}

// NewWithDefaultSensors creates an AsyncDetector with all default sensors
func NewWithDefaultSensors(opts ...Option) *AsyncDetector {
	opts = append(opts, WithSensors(DefaultSensors()...))
	return New(opts...)
}
