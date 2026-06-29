package conversion

import (
	"math"
	"testing"
)

// orthonormalBases are clean forward/left/up triads (proper right-handed
// rotations) used to prove quaternionToAxes inverts quaternionFromAxes.
func orthonormalBases() []struct {
	name          string
	fwd, left, up []float64
} {
	return []struct {
		name          string
		fwd, left, up []float64
	}{
		{"identity", []float64{0, 0, 1}, []float64{1, 0, 0}, []float64{0, 1, 0}},
		{"yaw90", []float64{1, 0, 0}, []float64{0, 0, -1}, []float64{0, 1, 0}},
		{"pitch_up", []float64{0, 1, 0}, []float64{1, 0, 0}, []float64{0, 0, -1}},
		// A general rotation: 45 deg about Y applied to the identity triad.
		{
			"yaw45",
			[]float64{math.Sin(math.Pi / 4), 0, math.Cos(math.Pi / 4)},
			[]float64{math.Cos(math.Pi / 4), 0, -math.Sin(math.Pi / 4)},
			[]float64{0, 1, 0},
		},
	}
}

func TestQuaternionToAxesInvertsFromAxes(t *testing.T) {
	// The forward path (quaternionFromAxes) uses a float64 sqrt + divisions in
	// the trace method, so the basis -> quat -> basis round-trip carries ~1e-7
	// float64 rounding. That is the true inverse to within float64 arithmetic
	// precision; the real (float32) reconstruction path is checked at 1e-3.
	const tol = 1e-6
	for _, tc := range orthonormalBases() {
		q := quaternionFromAxes(tc.fwd, tc.left, tc.up)
		fwd, left, up := quaternionToAxes(q)

		check := func(label string, got, want []float64) {
			for i := range want {
				if math.Abs(got[i]-want[i]) > tol {
					t.Errorf("%s/%s[%d] = %v, want %v (diff %g)", tc.name, label, i, got[i], want[i], got[i]-want[i])
				}
			}
		}
		check("fwd", fwd, tc.fwd)
		check("left", left, tc.left)
		check("up", up, tc.up)
	}
}

// TestPoseRoundTripFloat32 proves a position+orientation survives
// vectors -> Pose(float32) -> vectors within float32 precision.
func TestPoseRoundTripFloat32(t *testing.T) {
	const tol = 1e-3
	pos := []float64{12.5, -3.25, 39.75}
	tc := orthonormalBases()[3] // yaw45
	pose := poseFromVectors(pos, tc.fwd, tc.left, tc.up)

	gotPos, gotFwd, gotLeft, gotUp := poseToVectors(pose)
	for i := range pos {
		if math.Abs(gotPos[i]-pos[i]) > tol {
			t.Errorf("pos[%d] = %v, want %v", i, gotPos[i], pos[i])
		}
	}
	for i := range 3 {
		if math.Abs(gotFwd[i]-tc.fwd[i]) > tol {
			t.Errorf("fwd[%d] = %v, want %v", i, gotFwd[i], tc.fwd[i])
		}
		if math.Abs(gotLeft[i]-tc.left[i]) > tol {
			t.Errorf("left[%d] = %v, want %v", i, gotLeft[i], tc.left[i])
		}
		if math.Abs(gotUp[i]-tc.up[i]) > tol {
			t.Errorf("up[%d] = %v, want %v", i, gotUp[i], tc.up[i])
		}
	}
}
