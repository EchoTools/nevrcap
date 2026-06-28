package events

// RoleClass is a proto-version-agnostic classification of a player's team
// membership, derived solely from their team index and jersey number. It is the
// single source of truth for role resolution: the v1 telemetry.Role placed on
// PlayerJoined events (pkg/events) and the v2 capturepb.Role placed on the
// InitialRoster (pkg/conversion) are both derived from it, so the two paths can
// never disagree for the same player.
type RoleClass int

const (
	// RoleClassSpectator marks a non-playing participant: a jersey number of -1,
	// or any team index other than blue (0) or orange (1).
	RoleClassSpectator RoleClass = iota
	// RoleClassBlue marks a member of the blue team (team index 0).
	RoleClassBlue
	// RoleClassOrange marks a member of the orange team (team index 1).
	RoleClassOrange
)

// ClassifyRole resolves a team index and jersey number to a RoleClass. A jersey
// number of -1 marks a spectator regardless of team index, as does any team
// index other than 0 (blue) or 1 (orange).
func ClassifyRole(teamIdx int, jersey int32) RoleClass {
	if jersey == -1 {
		return RoleClassSpectator
	}
	switch teamIdx {
	case 0:
		return RoleClassBlue
	case 1:
		return RoleClassOrange
	default:
		return RoleClassSpectator
	}
}
