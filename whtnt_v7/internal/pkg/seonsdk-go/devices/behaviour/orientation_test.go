package behaviour

import "testing"

// TestOrientationPortraitUpright — the canonical held-in-hand gravity
// (0, -1, 0) should resolve to orientation 7 (Portrait).
func TestOrientationPortraitUpright(t *testing.T) {
	if got := OrientationFromGravity(0, -1, 0); got != 7 {
		t.Errorf("portrait: got %d, want 7", got)
	}
}

// TestOrientationFaceUp — gravity (0, 0, -1) should resolve to 2 (FaceUp).
func TestOrientationFaceUp(t *testing.T) {
	if got := OrientationFromGravity(0, 0, -1); got != 2 {
		t.Errorf("faceup: got %d, want 2", got)
	}
}

// TestOrientationAllTableEntries — feed each reference vector verbatim and
// require the matching orientation index.
func TestOrientationAllTableEntries(t *testing.T) {
	for _, e := range orientationTable {
		if got := OrientationFromGravity(e.x, e.y, e.z); got != e.idx {
			t.Errorf("%+v: got %d, want %d", e, got, e.idx)
		}
	}
}

// TestOrientationPortraitWithTilt — (0, -0.87, 0.5) is entry 8 exactly.
// A slightly noisy version (±0.02) should still resolve to 8.
func TestOrientationPortraitWithTilt(t *testing.T) {
	cases := []struct {
		x, y, z float64
		want    int64
	}{
		{0, -0.87, 0.5, 8},
		{0.01, -0.86, 0.48, 8},
		{-0.02, -0.85, 0.52, 8},
	}
	for _, c := range cases {
		if got := OrientationFromGravity(c.x, c.y, c.z); got != c.want {
			t.Errorf("g=(%.2f,%.2f,%.2f): got %d, want %d", c.x, c.y, c.z, got, c.want)
		}
	}
}
