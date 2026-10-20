package behaviour

import (
	"math"
	"testing"
)

// TestRoundQuaternion verifies 2-decimal rounding (0x141b8).
func TestRoundQuaternion(t *testing.T) {
	q := RoundQuaternion(Quaternion{X: 0.1234, Y: 0.5678, Z: -0.9012, W: 0.3499})
	want := Quaternion{X: 0.12, Y: 0.57, Z: -0.90, W: 0.35}
	if math.Abs(q.X-want.X)+math.Abs(q.Y-want.Y)+math.Abs(q.Z-want.Z)+math.Abs(q.W-want.W) > 1e-9 {
		t.Errorf("got %+v, want %+v", q, want)
	}
}

// TestBucketKeyForAttitude verifies "%f,%f,%f,%f" formatting with
// rounded-then-scaled components (0x13e70).
func TestBucketKeyForAttitude(t *testing.T) {
	q := Quaternion{X: 0.1234, Y: 0.5678, Z: -0.9012, W: 0.3499}
	got := BucketKeyForAttitude(q, 10)
	want := "1.200000,5.700000,-9.000000,3.500000"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestAttitudeFromBucketKeyRoundTrip verifies bucketKey → quaternion → key
// round-trips exactly (within bucket precision).
func TestAttitudeFromBucketKeyRoundTrip(t *testing.T) {
	orig := Quaternion{X: 0.12, Y: 0.57, Z: -0.90, W: 0.35}
	key := BucketKeyForAttitude(orig, 10)
	decoded := AttitudeFromBucketKey(key, 10)
	if math.Abs(orig.X-decoded.X)+math.Abs(orig.Y-decoded.Y)+math.Abs(orig.Z-decoded.Z)+math.Abs(orig.W-decoded.W) > 1e-9 {
		t.Errorf("round trip: got %+v, want %+v", decoded, orig)
	}
}

// TestIsAttitudeSimilar verifies the |dot| > 0.96 threshold (0x14204).
func TestIsAttitudeSimilar(t *testing.T) {
	// Identical quaternions: dot = 1.
	a := Quaternion{X: 0, Y: 0, Z: 0, W: 1}
	if !IsAttitudeSimilar(a, a) {
		t.Error("identical quaternions not flagged similar")
	}
	// Orthogonal: dot = 0, not similar.
	b := Quaternion{X: 1, Y: 0, Z: 0, W: 0}
	if IsAttitudeSimilar(a, b) {
		t.Error("orthogonal quaternions flagged similar")
	}
	// Just above the threshold: dot = 0.97.
	c := Quaternion{X: 0.1, Y: 0, Z: 0, W: 0.97}
	if !IsAttitudeSimilar(a, c) {
		t.Error("dot=0.97 not flagged similar")
	}
}

// TestAttitudeTrackerMostFrequent — feed one quaternion repeatedly; the
// most-frequent must converge to its bucket center.
func TestAttitudeTrackerMostFrequent(t *testing.T) {
	tr := NewAttitudeTracker()
	q := Quaternion{X: 0, Y: 0, Z: 0, W: 1}
	for i := 0; i < 20; i++ {
		tr.Update(q)
	}
	// Bucket center = round(0)/1 = 0 for x/y/z, round(1)/1 = 1 for w → same.
	if math.Abs(tr.MostFrequent.W-1.0) > 1e-9 {
		t.Errorf("most-frequent.w: got %v, want 1.0", tr.MostFrequent.W)
	}
}
