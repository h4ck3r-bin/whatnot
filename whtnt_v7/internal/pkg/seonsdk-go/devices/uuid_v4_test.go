package devices_test

import (
	"regexp"
	"testing"

	"status403.com/rsavv/seonsdk-go/devices"
)

// rfc4122V4 matches the canonical uppercase RFC 4122 v4 UUID format that
// `[NSUUID UUID].UUIDString` produces on iOS — segment 3 must start with `4`
// (version) and segment 4 must start with `8`, `9`, `A`, or `B` (variant).
var rfc4122V4 = regexp.MustCompile(`^[0-9A-F]{8}-[0-9A-F]{4}-4[0-9A-F]{3}-[89AB][0-9A-F]{3}-[0-9A-F]{12}$`)

// TestRandomDeviceUdid_IsValidV4 confirms IDFV emission matches what real
// iPhones produce. Earlier the generator formatted raw random bytes without
// setting the version/variant bits — empirically 99% of generated UUIDs
// failed RFC 4122 v4 validation. SEON's risk model can flag such mismatches.
func TestRandomDeviceUdid_IsValidV4(t *testing.T) {
	for i := 0; i < 1000; i++ {
		u := devices.RandomDeviceUdid()
		if !rfc4122V4.MatchString(u) {
			t.Errorf("iter %d: %q does not match RFC 4122 v4 format", i, u)
		}
	}
}

// TestRandomDeviceAdid_IsValidV4WhenTrackingGranted — same check on IDFA.
// (When tracking is denied, the SDK returns the all-zeros NIL UUID, which
// IS what real iPhones emit post-ATT and which is intentionally NOT a v4.)
func TestRandomDeviceAdid_IsValidV4WhenTrackingGranted(t *testing.T) {
	for i := 0; i < 1000; i++ {
		u := devices.RandomDeviceAdid(true)
		if !rfc4122V4.MatchString(u) {
			t.Errorf("iter %d: %q does not match RFC 4122 v4 format", i, u)
		}
	}
}

// TestRandomDeviceAdid_NilUUIDWhenTrackingDenied — verifies the post-ATT
// nil-UUID return value is unchanged. Real iPhones emit exactly this string
// when the user has denied tracking.
func TestRandomDeviceAdid_NilUUIDWhenTrackingDenied(t *testing.T) {
	const nilUUID = "00000000-0000-0000-0000-000000000000"
	for i := 0; i < 100; i++ {
		if got := devices.RandomDeviceAdid(false); got != nilUUID {
			t.Fatalf("iter %d: tracking-denied IDFA should be %q, got %q", i, nilUUID, got)
		}
	}
}
