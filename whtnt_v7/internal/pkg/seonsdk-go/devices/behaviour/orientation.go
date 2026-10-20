package behaviour

import "math"

// orientationTable is the 14-entry (gravity-vector → orient_idx) lookup table
// extracted from `unk_2D3F8` in SeonSDK 5.6.3 (layout verified via raw
// byte dump — see calculatePhysicalDeviceOrientationWithGravity at 0x1423c).
//
// Each entry maps a canonical unit gravity vector to the orientation index
// the SDK emits when gravity is closest to that direction (by cosine angle,
// acos of dot product).
var orientationTable = []orientationEntry{
	{2, 0, 0, -1},       // FaceUp
	{3, 0, 0, 1},        // FaceDown
	{4, 1, 0, 0},        // LandscapeRight
	{14, 0.87, 0, 0.5},  // LandscapeRight tilted +30°
	{15, 0.87, 0, -0.5}, // LandscapeRight tilted -30°
	{5, -1, 0, 0},       // LandscapeLeft
	{12, -0.87, 0, 0.5}, // LandscapeLeft tilted +30°
	{13, -0.87, 0, -0.5},
	{6, 0, 1, 0},        // PortraitUpsideDown
	{10, 0, 0.87, 0.5},  // PortraitUD tilted
	{11, 0, 0.87, -0.5},
	{7, 0, -1, 0},       // Portrait (default held-in-hand upright)
	{8, 0, -0.87, 0.5},  // Portrait tilted forward
	{9, 0, -0.87, -0.5}, // Portrait tilted back
}

type orientationEntry struct {
	idx        int64
	x, y, z    float64
}

// OrientationFromGravity reproduces -[calculatePhysicalDeviceOrientationWithGravity:]
// at 0x1423c. Returns the orientation index whose reference vector is closest
// (smallest acos-angle) to the supplied gravity vector.
//
// The SDK also force-feeds `gravityZ.addValue(gravity.z)` here — callers in
// the simulator must do the same emission on the gravity buffer when routing
// a motion sample through this function.
//
// Initial `previousOrientation` semantic is -1, so the very first call
// unconditionally emits an orientation change (matches 0x12794 where
// `previousOrientation = -1` is set at startBehaviourMonitoring).
func OrientationFromGravity(gx, gy, gz float64) int64 {
	mag := math.Sqrt(gx*gx + gy*gy + gz*gz)
	if mag == 0 {
		return -1
	}
	bestIdx := int64(-1)
	bestAngle := math.Pi + 1 // larger than any acos output
	for _, e := range orientationTable {
		refMag := math.Sqrt(e.x*e.x + e.y*e.y + e.z*e.z)
		if refMag == 0 {
			continue
		}
		dot := gx*e.x + gy*e.y + gz*e.z
		// Apple's code casts to float32 before acos (verified in decompile);
		// the rounding-error equivalence doesn't change the argmin outcome
		// under typical gravity noise, so we stay in float64.
		cos := dot / (mag * refMag)
		if cos > 1 {
			cos = 1
		} else if cos < -1 {
			cos = -1
		}
		angle := math.Acos(cos)
		if angle < bestAngle {
			bestAngle = angle
			bestIdx = e.idx
		}
	}
	return bestIdx
}

// Orientation-change-payload value constants, extracted from `off_43D60` /
// `off_43D78` / `off_43D90` (NSConstantIntegerNumber literals at those
// addresses — confirmed via IDA py_eval).
const (
	OrientAtEarStart   = int64(1)  // off_43D60 — phone brought to ear
	OrientAtEarEnd     = int64(0)  // off_43D78 — phone removed from ear
	OrientInitial      = int64(-1) // off_43D90 — previousOrientation sentinel
)
