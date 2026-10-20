package devices

import (
	"fmt"
	"math/rand"

	"status403.com/rsavv/seonsdk-go/payload"
)

// DeviceDefaults holds pre-computed default values for a device model + iOS version.
type DeviceDefaults struct {
	// device_name (= hardware platform name, NOT user's name)
	DeviceName string
	// Hardware
	ScreenWidth    float64
	ScreenHeight   float64
	NativeWidth    float64
	NativeHeight   float64
	PhysicalMemory uint64
	CpuType        string
	CpuCount       int
	IsArm64        bool
	Has3DTouch     bool
	DisplayGamut   string
	// OS
	KernelName    string
	KernelArch    string
	KernelVersion string
	IosVersion    string
	// Computed
	ScreenSize              uint64
	PhysicalMemoryFormatted string
	// Machine identifier (e.g. "iPhone15,4")
	Machine string
	// Formatting helpers
	NativeBounds map[string]interface{}
	ScreenBounds map[string]interface{}
	ScreenScale  int
	NativeScale  int
}

// BuildDefaults creates a DeviceDefaults from a machine identifier and iOS version.
// Returns nil if the machine is unknown OR if the iOS version is incompatible with
// the device (e.g. iPhone 16 Plus on iOS 17 — impossible, because iPhone 16 shipped
// with iOS 18.0 and cannot downgrade below that).
func BuildDefaults(machine string, iosVersion string) *DeviceDefaults {
	spec := LookupSpec(machine)
	if spec == nil {
		return nil
	}
	if !IsCompatibleIOSVersion(machine, iosVersion) {
		return nil
	}

	platformName := DevicePlatformName(machine)

	// kernel_version on a real iPhone is `sysctl kern.osversion` → the iOS
	// build number string (e.g., "22F76" for iOS 18.5). Verified against a
	// real-device capture. NOT the full Darwin kernel version — that one
	// isn't exposed via the SDK's probe chain. See build_numbers.go.
	//
	// When the iOS version isn't in IOSBuildNumbers, fail LOUDLY rather
	// than emitting the marketing version as a fake build — a fingerprint
	// with `kernel_version: "17.7.7"` is a 100 % ban because no real
	// iPhone ever emits that string. Callers should add the missing
	// version to build_numbers.go (via ipsw.me) before using it.
	kernelVersion := BuildNumberFor(iosVersion)
	if kernelVersion == "" {
		return nil
	}

	scale := 3

	return &DeviceDefaults{
		DeviceName:              platformName,
		Machine:                 machine,
		ScreenWidth:             spec.ScreenWidth,
		ScreenHeight:            spec.ScreenHeight,
		NativeWidth:             spec.NativeWidth,
		NativeHeight:            spec.NativeHeight,
		PhysicalMemory:          spec.PhysicalMemory,
		CpuType:                 spec.CpuType,
		CpuCount:                spec.CpuCount,
		IsArm64:                 spec.IsArm64,
		Has3DTouch:              spec.Has3DTouch,
		DisplayGamut:            spec.DisplayGamut,
		KernelName:              "Darwin",
		KernelArch:              "arm64",
		KernelVersion:           kernelVersion,
		IosVersion:              iosVersion,
		ScreenSize:              payload.ComputeScreenSize(spec.ScreenWidth, spec.ScreenHeight),
		PhysicalMemoryFormatted: payload.FormatPhysicalMemory(spec.PhysicalMemory),
		NativeBounds: map[string]interface{}{
			"native_width":    spec.NativeWidth,
			"native_height":   spec.NativeHeight,
			"native_origin_x": float64(0),
			"native_origin_y": float64(0),
		},
		ScreenBounds: map[string]interface{}{
			"screen_width":    spec.ScreenWidth,
			"screen_height":   spec.ScreenHeight,
			"screen_origin_x": float64(0),
			"screen_origin_y": float64(0),
		},
		ScreenScale: scale,
		NativeScale: scale,
	}
}

// RandomBrightness returns a realistic screen brightness value (0.0–1.0),
// weighted toward real-world population usage:
//
//	10 %  very low  (0.10–0.25)  — battery-saver / night reader
//	60 %  auto mid  (0.35–0.70)  — typical indoor auto-brightness
//	20 %  high      (0.70–0.90)  — deliberate user-set bright
//	10 %  max       (0.90–1.00)  — outdoors / "max brightness always" users
//
// Uniform 0.3–0.8 (the previous distribution) under-represents the
// high-brightness tail and completely missed the night/battery-saver
// minority, both of which are common on real iPhone fleets.
func RandomBrightness() float64 {
	r := rand.Float64()
	switch {
	case r < 0.10:
		return 0.10 + rand.Float64()*0.15
	case r < 0.70:
		return 0.35 + rand.Float64()*0.35
	case r < 0.90:
		return 0.70 + rand.Float64()*0.20
	default:
		return 0.90 + rand.Float64()*0.10
	}
}

// RandomBatteryLevel returns a realistic battery level (20 to 100).
func RandomBatteryLevel() int {
	return 20 + rand.Intn(81) // 20 to 100
}

// RandomSystemUptime returns a realistic uptime in seconds (1 hour to 7 days).
func RandomSystemUptime() int64 {
	return int64(3600 + rand.Intn(604800))
}

// RandomAudioVolume returns a realistic media-output volume (2–100).
//
// The value is ALWAYS EVEN. iOS exposes output volume on a 0.02 grid, so a
// real device only ever reports multiples of 2 — the freshwhatnot capture
// reports 30. Emitting odd values put roughly half of every fingerprint's
// volumes on a step no iPhone can produce.
//
// Volume 0 is only valid when audio_mute_status is true; since the builder
// defaults mute to false, we floor at 2 to avoid the contradiction.
//
// Distribution weighted toward real-world population usage:
//
//	25 %  very quiet  (2–6)         — near-silent, common at signup
//	20 %  quiet       (8–24)        — ambient / commute
//	35 %  mid         (30–64)       — typical music / video listening
//	15 %  loud        (70–90)       — active listening
//	 5 %  max         (96–100)      — "always max" users
func RandomAudioVolume() int {
	r := rand.Float64()
	switch {
	case r < 0.25:
		return 2 + 2*rand.Intn(3) // 2–6
	case r < 0.45:
		return 8 + 2*rand.Intn(9) // 8–24
	case r < 0.80:
		return 30 + 2*rand.Intn(18) // 30–64
	case r < 0.95:
		return 70 + 2*rand.Intn(11) // 70–90
	default:
		return 96 + 2*rand.Intn(3) // 96–100
	}
}

// FormatPasteboardHash generates a realistic-looking pasteboard hash.
// In the real SDK this is SHA-256 of detected patterns + UIPasteboard.changeCount.
func FormatPasteboardHash(patterns string, changeCount int) string {
	input := fmt.Sprintf("%s%d", patterns, changeCount)
	return payload.SHA256HexFromString(input)
}

// RandomPreferredContentSize returns a realistic content size category.
// SEON maps UIContentSizeCategory to short codes (verified at 0x6374).
// Distribution based on real-world usage: ~75% L (default), rest varies.
func RandomPreferredContentSize() string {
	r := rand.Float64()
	switch {
	case r < 0.02:
		return "S"
	case r < 0.07:
		return "M"
	case r < 0.75:
		return "L" // default, most common
	case r < 0.88:
		return "XL"
	case r < 0.95:
		return "XXL"
	case r < 0.98:
		return "XXXL"
	default:
		return "XS"
	}
}

// RandomUserInterfaceStyle returns "Light" or "Dark" with realistic distribution.
// Verified from IDA at 0x21f7c: values are "Unspecified", "Light", "Dark".
// Real-world: roughly 70% light, 30% dark.
func RandomUserInterfaceStyle() string {
	if rand.Float64() < 0.70 {
		return "Light"
	}
	return "Dark"
}

// DateFormatForLocale returns the NSDateFormatter medium-style date pattern for a locale.
// Verified at 0xa540: NSDateFormatter with dateStyle=Medium, timeStyle=None.
// The format varies by locale and is one of the 20 inputs to implicit_device_hash.
var DateFormats = map[string]string{
	"en":    "MMM d, y",
	"en_US": "MMM d, y",
	"en_GB": "d MMM y",
	"en_AU": "d MMM y",
	"en_CA": "MMM d, y",
	"de":    "dd.MM.y",
	"de_DE": "dd.MM.y",
	"de_AT": "dd.MM.y",
	"de_CH": "dd.MM.y",
	"fr":    "d MMM y",
	"fr_FR": "d MMM y",
	"fr_CA": "d MMM y",
	"es":    "d MMM y",
	"es_ES": "d MMM y",
	"es_MX": "d MMM y",
	"it":    "d MMM y",
	"it_IT": "d MMM y",
	"pt":    "d 'de' MMM 'de' y",
	"pt_BR": "d 'de' MMM 'de' y",
	"pt_PT": "dd/MM/y",
	"ja":    "y/MM/dd",
	"ja_JP": "y/MM/dd",
	"zh":    "y年M月d日",
	"zh_CN": "y年M月d日",
	"zh_TW": "y年M月d日",
	"ko":    "y. M. d.",
	"ko_KR": "y. M. d.",
	"nl":    "d MMM y",
	"nl_NL": "d MMM y",
	"sv":    "d MMM y",
	"sv_SE": "d MMM y",
	"da":    "d. MMM y",
	"da_DK": "d. MMM y",
	"nb":    "d. MMM y",
	"nb_NO": "d. MMM y",
	"fi":    "d.M.y",
	"fi_FI": "d.M.y",
	"pl":    "d MMM y",
	"pl_PL": "d MMM y",
	"ru":    "d MMM y г.",
	"ru_RU": "d MMM y г.",
	"tr":    "d MMM y",
	"tr_TR": "d MMM y",
	"ar":    "d MMM y",
	"ar_SA": "d MMM y",
	"th":    "d MMM y",
	"th_TH": "d MMM y",
	"vi":    "d MMM, y",
	"vi_VN": "d MMM, y",
	"uk":    "d MMM y р.",
	"uk_UA": "d MMM y р.",
	"id":    "d MMM y",
	"id_ID": "d MMM y",
	"ms":    "d MMM y",
	"ms_MY": "d MMM y",
	"hi":    "d MMM y",
	"hi_IN": "d MMM y",
}

// DateFormatForLanguage returns the date format for a given language code.
// Falls back to "MMM d, y" (en_US) if the language is not mapped.
func DateFormatForLanguage(lang string) string {
	if f, ok := DateFormats[lang]; ok {
		return f
	}
	return "MMM d, y"
}

// DateFormatForLocale returns the date format for a language+country pair,
// matching what NSDateFormatter.dateStyle=Medium produces on a real device.
// Tries "lang_COUNTRY" first, then bare "lang", then falls back to en_US.
func DateFormatForLocale(lang, country string) string {
	if country != "" {
		key := lang + "_" + country
		if f, ok := DateFormats[key]; ok {
			return f
		}
	}
	return DateFormatForLanguage(lang)
}
