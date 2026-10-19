package behaviour

import (
	"fmt"
	"math"
	"sort"
)

// Quaternion mirrors the anonymous CMQuaternion struct SEON passes around
// (0x1BB1521EC52D44A8E7628F5261DCEC8) — four doubles (x, y, z, w).
type Quaternion struct {
	X, Y, Z, W float64
}

// RoundQuaternion matches -[roundQuaternion:] at 0x141b8 — round each
// component to 2 decimal places (`round(c*100)/100`).
func RoundQuaternion(q Quaternion) Quaternion {
	return Quaternion{
		X: math.Round(q.X*100) / 100,
		Y: math.Round(q.Y*100) / 100,
		Z: math.Round(q.Z*100) / 100,
		W: math.Round(q.W*100) / 100,
	}
}

// IsAttitudeSimilar matches -[isAttitude:similarToAttitude:] at 0x14204:
// `|dot(a, b)| > 0.96`. The SDK uses this for the at-ear transition test and
// for the "attitude returned to most-frequent" test.
func IsAttitudeSimilar(a, b Quaternion) bool {
	return math.Abs(a.X*b.X+a.Y*b.Y+a.Z*b.Z+a.W*b.W) > 0.96
}

// BucketKeyForAttitude matches -[bucketKeyForAttitude:] at 0x13e70 exactly:
// round each component to 0.01, multiply by `bucketSize` (= 10 per bootstrap
// at 0x125c0), and format with "%f,%f,%f,%f" (Apple's default %f precision).
// Apple's default %f produces 6 decimals; Go's default %f also produces 6.
func BucketKeyForAttitude(q Quaternion, bucketSize int) string {
	r := RoundQuaternion(q)
	bs := float64(bucketSize)
	return fmt.Sprintf("%f,%f,%f,%f", r.X*bs, r.Y*bs, r.Z*bs, r.W*bs)
}

// AttitudeFromBucketKey matches -[attitudeFromBucketKey:] at 0x13ee4.
// Splits the key by ',', parses each component, divides by bucketSize to
// recover the bucket-center quaternion.
func AttitudeFromBucketKey(key string, bucketSize int) Quaternion {
	var x, y, z, w float64
	_, _ = fmt.Sscanf(key, "%f,%f,%f,%f", &x, &y, &z, &w)
	bs := float64(bucketSize)
	return Quaternion{X: x / bs, Y: y / bs, Z: z / bs, W: w / bs}
}

// DictionaryFromQuaternion matches -[dictionaryFromQuaternion:] at 0x14034 —
// produces `{"x": x, "y": y, "z": z, "w": w}`. Ordering is determined by
// the payload-level MarshalSortedJSON so this map's iteration doesn't matter.
func DictionaryFromQuaternion(q Quaternion) map[string]interface{} {
	return map[string]interface{}{
		"x": q.X,
		"y": q.Y,
		"z": q.Z,
		"w": q.W,
	}
}

// AttitudeTracker mirrors the `attitudeBucket` field on SEONBehaviourProbe
// plus the bits of -[updateMostFrequentAttitude:] at 0x13ce4 that aren't
// covered by `BucketKeyForAttitude`. Each call to Update increments the
// bucket counter for the supplied quaternion and refreshes the
// `mostFrequentAttitude` to whichever bucket has the highest count.
type AttitudeTracker struct {
	Buffer             *DictionaryBuffer
	BucketSize         int
	MostFrequent       Quaternion
	firstSample        bool
}

// NewAttitudeTracker creates a tracker with the SDK's capacities
// (attitudeBucket = 100 entries, bucketSize = 10 — both set in bootstrap at
// 0x12520 and startBehaviourMonitoring at 0x125c0).
func NewAttitudeTracker() *AttitudeTracker {
	return &AttitudeTracker{
		Buffer:      NewDictionaryBuffer(100),
		BucketSize:  10,
		firstSample: true,
	}
}

// Update — called per motion sample while NOT at-ear (the SDK skips this
// branch when `isAtEar == 1`, see 0x13cfc). Panics on invalid input only if
// the caller supplies NaN quaternions; in normal simulator flow this can't
// happen.
func (t *AttitudeTracker) Update(q Quaternion) {
	key := BucketKeyForAttitude(q, t.BucketSize)
	prev, _ := t.Buffer.Data()[key].(int)
	t.Buffer.AddEntry(key, prev+1)

	// Recompute the most-frequent bucket by scanning the data map (same as
	// Apple's keysSortedByValueUsingComparator + firstObject — O(n) where n
	// is capped at 100). The caller may only read MostFrequent after Update.
	bestKey := key
	bestCount := int(-1)
	for k, v := range t.Buffer.Data() {
		c, _ := v.(int)
		if c > bestCount || (c == bestCount && k < bestKey) {
			bestCount = c
			bestKey = k
		}
	}
	t.MostFrequent = AttitudeFromBucketKey(bestKey, t.BucketSize)
	t.firstSample = false
}

// sortedKeys is exposed for deterministic iteration in tests.
func (t *AttitudeTracker) sortedKeys() []string {
	keys := make([]string, 0, len(t.Buffer.Data()))
	for k := range t.Buffer.Data() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
