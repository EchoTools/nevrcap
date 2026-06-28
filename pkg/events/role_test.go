package events

import "testing"

func TestClassifyRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		teamIdx int
		jersey  int32
		want    RoleClass
	}{
		{"blue team", 0, 0, RoleClassBlue},
		{"blue team high jersey", 0, 42, RoleClassBlue},
		{"orange team", 1, 1, RoleClassOrange},
		{"team index 2 is spectator", 2, 3, RoleClassSpectator},
		{"team index 5 is spectator", 5, 7, RoleClassSpectator},
		{"jersey -1 on blue team is spectator", 0, -1, RoleClassSpectator},
		{"jersey -1 on orange team is spectator", 1, -1, RoleClassSpectator},
		{"jersey -1 on spectator team is spectator", 2, -1, RoleClassSpectator},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ClassifyRole(tt.teamIdx, tt.jersey); got != tt.want {
				t.Errorf("ClassifyRole(%d, %d) = %v, want %v", tt.teamIdx, tt.jersey, got, tt.want)
			}
		})
	}
}
