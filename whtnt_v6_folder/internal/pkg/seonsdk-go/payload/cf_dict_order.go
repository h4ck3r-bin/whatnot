package payload

import "fmt"

// CFBasicHash table sizes — from Apple's open-source CoreFoundation
// (CFBasicHash.c, __CFBasicHashTableSizes). Index = capacity level; value =
// bucket count at that level.
//
// NSMutableDictionary at level N holds up to __CFBasicHashTableCapacities[N]
// key-value pairs before promoting to level N+1.
//
//	level 0: size=0    capacity=0
//	level 1: size=3    capacity=3
//	level 2: size=7    capacity=6
//	level 3: size=13   capacity=11
//	level 4: size=23   capacity=19
//	level 5: size=41   capacity=32   ← 20-field stable dict lives here
//	level 6: size=71   capacity=52
//	level 7: size=127  capacity=85
var cfBasicHashTableSizes = [...]uint64{
	0, 3, 7, 13, 23, 41, 71, 127, 191, 251, 383, 631, 1087, 1723,
}

var cfBasicHashTableCapacities = [...]int{
	0, 3, 6, 11, 19, 32, 52, 85, 118, 155, 237, 390, 672, 1065,
}

// cfBucketCountForSize returns the bucket count an NSMutableDictionary uses
// when holding exactly `n` elements. Matches Apple's CFBasicHash growth
// rule: pick the smallest level whose capacity ≥ n. A 20-element dict
// lands at level 5 → 41 buckets.
func cfBucketCountForSize(n int) uint64 {
	for i, cap := range cfBasicHashTableCapacities {
		if cap >= n {
			return cfBasicHashTableSizes[i]
		}
	}
	panic(fmt.Sprintf("cfBucketCountForSize: n=%d exceeds supported levels", n))
}

// CFBucketOrder simulates NSMutableDictionary's bucket layout and returns
// the keys in the order `NSJSONSerialization options=0` would emit them.
//
// Takes `insertionOrder` — the order keys were originally added to the
// dict (for `dictionaryWithObjects:forKeys:count:`, this is the array
// order; for the 20-field stable dict it's the order decompiled from IDA
// 0x1a8cc: device_name, ios_device_name, cpu_type, …, screen_size).
//
// Algorithm: linear open-addressing probe (CFBasicHash default for
// post-10.9 CoreFoundation).
//
//  1. For each key in insertion order:
//     a. bucket = CFStringHashASCII(key) % bucketCount
//     b. while buckets[bucket] is occupied → bucket = (bucket+1) %
//     bucketCount
//     c. place key at bucket
//
//  2. Iterate buckets 0..bucketCount-1; emit the key at each occupied
//     bucket. That sequence is the iteration order NSDictionary uses and
//     the byte order NSJSONSerialization walks.
//
// Returns the iteration-order slice. Its len equals len(insertionOrder).
func CFBucketOrder(insertionOrder []string) []string {
	n := len(insertionOrder)
	if n == 0 {
		return nil
	}
	bucketCount := cfBucketCountForSize(n)

	buckets := make([]string, bucketCount)
	occupied := make([]bool, bucketCount)

	for _, k := range insertionOrder {
		h := CFStringHashASCII(k)
		idx := h % bucketCount
		// Linear probe on collision.
		for occupied[idx] {
			idx = (idx + 1) % bucketCount
		}
		buckets[idx] = k
		occupied[idx] = true
	}

	ordered := make([]string, 0, n)
	for i := uint64(0); i < bucketCount; i++ {
		if occupied[i] {
			ordered = append(ordered, buckets[i])
		}
	}
	return ordered
}

// StableDictInsertionOrder is the exact 20-key order the SEON SDK passes to
// `+[NSDictionary dictionaryWithObjects:forKeys:count:]` when building the
// stable dict whose SHA-256 becomes `implicit_device_hash`. Captured
// verbatim from IDA 0x1a9b4–0x1accc.
//
// Used to drive CFBucketOrder deterministically — passing a different
// insertion order would produce a different probe sequence and therefore a
// different iteration order, so a different hash.
var StableDictInsertionOrder = []string{
	"device_name",
	"ios_device_name",
	"cpu_type",
	"cpu_count",
	"user_interface_style",
	"is_biometrics_enabled",
	"date_format",
	"physical_memory",
	"region_language",
	"region_country",
	"calendar_identifier",
	"can_send_mail",
	"can_send_text",
	"is_darker_system_colors_enabled",
	"is_bold_text_enabled",
	"is_reduce_motion_enabled",
	"is_video_autoplay_enabled",
	"is_reduce_transparency_enabled",
	"preferred_content_size",
	"screen_size",
}

// CFStableDictOrder is StableDictInsertionOrder put through CFBucketOrder
// once at package-init time. All ComputeDeviceHash calls reuse this slice
// rather than recomputing the bucket layout every fingerprint.
var CFStableDictOrder = CFBucketOrder(StableDictInsertionOrder)

// MarshalCFCompact produces the byte stream that
// `NSJSONSerialization options=0` emits over an NSMutableDictionary whose
// keys are the given order. Compact form — no whitespace, no sorted keys,
// values serialized to match Apple's NSNumber/NSString/NSNull semantics.
func MarshalCFCompact(dict map[string]interface{}, order []string) ([]byte, error) {
	var buf cfBuf
	buf.WriteByte('{')
	first := true
	for _, k := range order {
		v, present := dict[k]
		if !present {
			// Keys in `order` but missing from `dict` are skipped. Should
			// not happen for the 20-field stable dict but we guard for
			// safety against forward-compat drift.
			continue
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		writeCompactString(&buf, k)
		buf.WriteByte(':')
		if err := writeCompactValue(&buf, v); err != nil {
			return nil, err
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
