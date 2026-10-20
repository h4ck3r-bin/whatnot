// Package version centralises the SEON SDK version string and the
// derived `source` / `version_code` payload fields. All of these are
// computed from a single `BundleVersion` input so that bumping the SDK
// across Whatnot releases is a one-line change.
package version

import (
	"fmt"
	"strconv"
	"strings"
)

// DefaultBundleVersion is the SDK bundle version we ship. When Whatnot
// upgrades the embedded SeonSDK.framework, diff `Info.plist`'s
// `CFBundleShortVersionString` and bump this constant.
//
// Verified: Whatnot ships SeonSDK.framework `5.7.1`
// (Info.plist CFBundleShortVersionString at
// Payload/Whatnot.app/Frameworks/SeonSDK.framework/Info.plist).
const DefaultBundleVersion = "5.7.1"

// SourceFor produces the `source` payload string the SDK emits at IDA
// `0x1afd8` (-[SEONFingerprint getSDKVersion]):
//
//	stringWithFormat:@"ios-%@", bundleVersion
//
// Example: SourceFor("5.7.1") → "ios-5.7.1".
func SourceFor(bundleVersion string) string {
	return "ios-" + bundleVersion
}

// VersionCodeFor produces the `version_code` payload string the SDK emits
// at IDA `0x1cb64` (-[SEONFingerprint versionCode]):
//
//	stringWithFormat:@"%@%02d%02d%02d", @"01",
//	    major % 100, minor % 100, patch % 100
//
// `"01"` is a hardcoded platform prefix (iOS). Components are integer-
// parsed from the bundle version split by ".", `% 100`-masked, and
// zero-padded.
//
// Examples:
//
//	VersionCodeFor("5.6.3")   → "01050603"
//	VersionCodeFor("6.0.0")   → "01060000"
//	VersionCodeFor("12.34.5") → "01123405"
//	VersionCodeFor("5.6")     → "01050600"   (missing patch → 0)
//	VersionCodeFor("")        → "01000000"
func VersionCodeFor(bundleVersion string) string {
	parts := strings.Split(bundleVersion, ".")
	var major, minor, patch int
	if len(parts) > 0 {
		major = parseIntMod100(parts[0])
	}
	if len(parts) > 1 {
		minor = parseIntMod100(parts[1])
	}
	if len(parts) > 2 {
		patch = parseIntMod100(parts[2])
	}
	return fmt.Sprintf("01%02d%02d%02d", major, minor, patch)
}

func parseIntMod100(s string) int {
	// NSString.intValue parses leading digits and ignores trailing junk;
	// strconv.Atoi is stricter. Replicate NSString semantics manually: a
	// leading unsigned run, fall-through to 0 on an empty-or-bad prefix.
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	v, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0
	}
	return v % 100
}

// Convenience constants for the default SDK version. Callers that support
// multiple SDK versions should compute Source / VersionCode per-device
// instead of reading these.
var (
	Source      = SourceFor(DefaultBundleVersion)
	VersionCode = VersionCodeFor(DefaultBundleVersion)
)
