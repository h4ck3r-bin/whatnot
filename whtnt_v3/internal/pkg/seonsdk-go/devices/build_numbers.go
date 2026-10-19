package devices

import "math/rand"

// IOSBuildNumbers maps iOS marketing version strings (as returned by
// UIDevice.systemVersion — e.g., "18.5", "17.6.1") to all known build
// numbers Apple has shipped for that version. The build number is what
// `sysctl kern.osversion` returns — the string that ends up in the
// `kernel_version` payload field per IDA 0x15be8
// (-[SEONOSProbe kernel_version]).
//
// Format: <darwin-major><release-letter><patch-digits>. Example:
// "22F76" = Darwin 22-family, release F, build 76 → iOS 18.5 GM.
//
// Source: ipsw.me IPSW + mesu catalogs, cross-checked against iPhone
// XS / XR / 11 / 12 / 13 / SE3 / 14 Pro / 15 Pro / 16 Pro / 17 Pro.
// Multiple builds per version appear when different hardware families
// shipped distinct restores for the same marketing version.
//
// Coverage: every iOS version shipped to any supported iPhone from
// 12.0 (iPhone XS launch) to the current 26.4.x, plus the 17.7.x and
// 18.7.x OTA-only security tracks.
//
// Policy: an iOS version NOT in this map is unsupported. NewDevice()
// returns an error rather than emit a fake build number — real iPhones
// always emit one of the values below for their respective version.
var IOSBuildNumbers = map[string][]string{
	// iOS 12 — iPhone XS/XR/XS Max launch. Min supported by SDK = iOS 12.0.
	"12.0":   {"16A366"},
	"12.0.1": {"16A405"},
	"12.1":   {"16B92", "16B93", "16B94"},
	"12.1.1": {"16C50"},
	"12.1.2": {"16C101", "16C104"},
	"12.1.3": {"16D40"},
	"12.1.4": {"16D57"},
	"12.2":   {"16E227"},
	"12.3":   {"16F156"},
	"12.3.1": {"16F203"},
	"12.4":   {"16G77"},
	"12.4.1": {"16G102"},

	// iOS 13 — iPhone 11 launch.
	"13.0":   {"17A577"},
	"13.1":   {"17A844"},
	"13.1.1": {"17A854"},
	"13.1.2": {"17A860", "17A861"},
	"13.1.3": {"17A878"},
	"13.2":   {"17B84"},
	"13.2.2": {"17B102"},
	"13.2.3": {"17B111"},
	"13.3":   {"17C54"},
	"13.3.1": {"17D50"},
	"13.4":   {"17E255"},
	"13.4.1": {"17E262", "17E8258"},
	"13.5":   {"17F75"},
	"13.5.1": {"17F80"},
	"13.6":   {"17G68"},
	"13.6.1": {"17G80"},
	"13.7":   {"17H35"},

	// iOS 14 — iPhone 12 launch.
	"14.0":   {"18A373"},
	"14.0.1": {"18A393"},
	"14.1":   {"18A8395"},
	"14.2":   {"18B92", "18B111"},
	"14.2.1": {"18B121"},
	"14.3":   {"18C66"},
	"14.4":   {"18D52"},
	"14.4.1": {"18D61"},
	"14.4.2": {"18D70"},
	"14.5":   {"18E199"},
	"14.5.1": {"18E212"},
	"14.6":   {"18F72"},
	"14.7":   {"18G69"},
	"14.7.1": {"18G82"},
	"14.8":   {"18H17"},

	// iOS 15 — iPhone 13 launch.
	"15.0":   {"19A346"},
	"15.0.1": {"19A348"},
	"15.0.2": {"19A404"},
	"15.1":   {"19B74"},
	"15.1.1": {"19B81"},
	"15.2":   {"19C56", "19C57"},
	"15.2.1": {"19C63"},
	"15.3":   {"19D50"},
	"15.3.1": {"19D52"},
	"15.4":   {"19E241"},
	"15.4.1": {"19E258"},
	"15.5":   {"19F77"},
	"15.6":   {"19G71"},
	"15.6.1": {"19G82"},

	// iOS 16 — iPhone 14 launch.
	"16.0":   {"20A362"},
	"16.0.1": {"20A371"},
	"16.0.2": {"20A380"},
	"16.0.3": {"20A392"},
	"16.1":   {"20B82"},
	"16.1.1": {"20B101"},
	"16.1.2": {"20B110"},
	"16.2":   {"20C65"},
	"16.3":   {"20D47"},
	"16.3.1": {"20D67"},
	"16.4":   {"20E247"},
	"16.4.1": {"20E252"},
	"16.5":   {"20F66"},
	"16.5.1": {"20F75"},
	"16.6":   {"20G75"},
	"16.6.1": {"20G81"},

	// iOS 17 — iPhone 15 launch.
	"17.0":   {"21A329"},
	"17.0.1": {"21A340"},
	"17.0.2": {"21A350", "21A351"},
	"17.0.3": {"21A360"},
	"17.1":   {"21B74", "21B80"},
	"17.1.1": {"21B91"},
	"17.1.2": {"21B101"},
	"17.2":   {"21C62"},
	"17.2.1": {"21C66"},
	"17.3":   {"21D50"},
	"17.3.1": {"21D61"},
	"17.4":   {"21E219"},
	"17.4.1": {"21E236", "21E237"},
	"17.5":   {"21F79"},
	"17.5.1": {"21F90"},
	"17.6":   {"21G80"},
	"17.6.1": {"21G93", "21G101"},

	// iOS 17.7.x — Apple's "stay on iOS 17" security track. Offered to
	// users who opted out of iOS 18. OTA-only on iPhones (no full IPSW
	// restore) but the build numbers are shared across devices; they
	// appear in iPad mesu catalogs (iPad 6, iPad Pro 10.5") which can't
	// upgrade to 18.
	"17.7":   {"21H16"},
	"17.7.1": {"21H216"},
	"17.7.2": {"21H221"},
	"17.7.3": {"21H312"},
	"17.7.4": {"21H414"},
	"17.7.5": {"21H420"},
	"17.7.6": {"21H423"},
	"17.7.7": {"21H433"},
	"17.7.8": {"21H440"},
	"17.7.9": {"21H446"},

	// iOS 18 — iPhone 16 launch.
	"18.0":   {"22A3354"},
	"18.0.1": {"22A3370"},
	"18.1":   {"22B83"},
	"18.1.1": {"22B91"},
	"18.2":   {"22C152"},
	"18.2.1": {"22C161"},
	"18.3":   {"22D63", "22D64"},
	"18.3.1": {"22D72"},
	"18.3.2": {"22D82"},
	"18.4":   {"22E240"},
	"18.4.1": {"22E252"},
	"18.5":   {"22F76"},
	"18.6":   {"22G86"},
	"18.6.1": {"22G90"},
	"18.6.2": {"22G100"},

	// iOS 18.7.x — security track shipped after iOS 26 became the
	// primary release.
	"18.7":   {"22H20"},
	"18.7.1": {"22H31"},
	"18.7.2": {"22H124"},
	"18.7.3": {"22H217"},
	"18.7.4": {"22H218"},
	"18.7.5": {"22H311"},
	"18.7.6": {"22H320"},
	"18.7.7": {"22H333", "22H340"},

	// iOS 26 — iPhone 17 launch (Sep 2025).
	"26.0":   {"23A341", "23A345"},
	"26.0.1": {"23A355"},
	"26.1":   {"23B85"},
	"26.2":   {"23C55"},
	"26.2.1": {"23C71"},
	"26.3":   {"23D127"},
	"26.3.1": {"23D8133"},
	"26.4":   {"23E246"},
	"26.4.1": {"23E254"},
	// 26.5.2 is taken verbatim from testdata/freshwhatnot: all three real
	// payloads report `ios_version: "26.5.2"` with `kernel_version: "23F84"`,
	// and the HTTP `x-system-version` header agrees. The low build suffix on
	// a .2 patch is unusual but it is what the device emitted — do not
	// "correct" it to a more plausible-looking string.
	//
	// 26.5 and 26.5.1 are deliberately absent: freshwhatnot.har carries no
	// build number for them, and BuildDefaults is designed to fail loudly
	// rather than emit a guessed `kernel_version`. Add them only from a
	// capture or ipsw.me, never by inference.
	"26.5.2": {"23F84"},
}

// BuildNumberFor picks a build number for a given iOS version. When
// multiple builds are known it returns one at random so different
// Devices with the same iOS version don't collapse onto an identical
// string. Returns empty string for unmapped versions; NewDevice()
// treats that as a hard error and refuses to emit a fake.
func BuildNumberFor(iosVersion string) string {
	builds, ok := IOSBuildNumbers[iosVersion]
	if !ok || len(builds) == 0 {
		return ""
	}
	if len(builds) == 1 {
		return builds[0]
	}
	return builds[rand.Intn(len(builds))]
}

// SupportedIOSVersions returns every iOS version currently mapped in
// IOSBuildNumbers. Useful for callers picking a random version that's
// guaranteed not to hit an unmapped-version error in NewDevice().
func SupportedIOSVersions() []string {
	out := make([]string, 0, len(IOSBuildNumbers))
	for v := range IOSBuildNumbers {
		out = append(out, v)
	}
	return out
}
