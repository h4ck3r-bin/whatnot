package devices

import (
	"fmt"
	"math/rand"
	"strings"
)

// RandomDeviceUdid generates a random uppercase UUID string matching the IDFV format.
// Verified via IDA at 0x20b10: deviceIdentifier reads keychain "device_udid" or falls back
// to [UIDevice.currentDevice.identifierForVendor UUIDString] — always uppercase.
func RandomDeviceUdid() string {
	return randomUUID()
}

// RandomDeviceAdid generates a device IDFA string.
// Verified via IDA at 0x20c74: [ASIdentifierManager.sharedManager.advertisingIdentifier UUIDString].
// Post-ATT (iOS 14.5+), returns all-zeros when tracking is not authorized.
func RandomDeviceAdid(trackingGranted bool) string {
	if !trackingGranted {
		return "00000000-0000-0000-0000-000000000000"
	}
	return randomUUID()
}

// RandomIcloudUbiquityToken generates a realistic iCloud ubiquity identity token string.
// Verified via IDA at 0x20d28: iCloudToken reads [NSFileManager.defaultManager ubiquityIdentityToken],
// formats bytes as hex with "%02.2hhx", inserts a space when (i & 3) == 0 && i > 0,
// then strips '<' and '>' characters.
// Returns nil-compatible empty string if iCloud is "not signed in" (caller should use nil).
//
// The real iOS emits a 20-byte token — `NSFileManager.ubiquityIdentityToken`
// serialised as 2-char-per-byte lowercase hex with a space every 4 bytes per
// IDA 0x20dd0–0x20e88. Verified against an iPhone 14 Pro capture:
// `"94051589 5b5e1c27 79c2ca45 74bae595 b8fff830"` — exactly 5 groups of 8
// hex chars. Our previous 32-byte default would emit 8 groups, an immediate
// tell. See `-[SEONDeviceProbe iCloudToken]` (0x20cfc).
func RandomIcloudUbiquityToken() string {
	tokenBytes := make([]byte, 20)
	rand.Read(tokenBytes)
	return FormatIcloudToken(tokenBytes)
}

// FormatIcloudToken formats raw bytes in the SEON iCloud token hex format.
// Format: lowercase hex, space inserted every 4 bytes.
// Example: "a1b2c3d4 e5f6a7b8 90abcdef 12345678 ..."
func FormatIcloudToken(data []byte) string {
	var sb strings.Builder
	sb.Grow(len(data)*2 + len(data)/4)
	for i, b := range data {
		if i > 0 && (i&3) == 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "%02x", b)
	}
	return sb.String()
}
