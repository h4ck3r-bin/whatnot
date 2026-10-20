package devices

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"

	seonsdk "status403.com/rsavv/seonsdk-go"
)

type PayloadOptions struct {
	Machine    string // e.g. "iPhone16,2"
	IOSVersion string // e.g. "18.0"

	UserDeviceName string // user-chosen name (ios_device_name), e.g. "John's iPhone"
	DeviceUdid     string // IDFV — leave empty to auto-generate uppercase UUID
	DeviceAdid     string // IDFA — defaults to all-zeros (post-ATT)

	IcloudUbiquityToken interface{} // nil = not signed into iCloud; string = hex token
	IcloudSignedIn      bool        // if true and IcloudUbiquityToken is nil, auto-generate a token

	TrackingGranted  bool // if true, generates a random IDFA instead of all-zeros
	ReturningSession bool // if true, hashProvider uses cached values (1/3 instead of 0/2)

	Locale *seonsdk.LocaleConfig
	Proxy  string
	SkipDC bool

	IsFlutter     bool
	IsReactNative bool
}

func BuildPayload(opts PayloadOptions) (*seonsdk.FingerprintConfig, error) {
	defaults := BuildDefaults(opts.Machine, opts.IOSVersion)
	if defaults == nil {
		return nil, fmt.Errorf("unknown device model: %s", opts.Machine)
	}

	deviceAdid := opts.DeviceAdid
	if deviceAdid == "" {
		deviceAdid = RandomDeviceAdid(opts.TrackingGranted)
	}

	deviceUdid := opts.DeviceUdid
	if deviceUdid == "" {
		deviceUdid = RandomDeviceUdid()
	}

	icloudToken := opts.IcloudUbiquityToken
	if icloudToken == nil && opts.IcloudSignedIn {
		icloudToken = RandomIcloudUbiquityToken()
	}

	userDeviceName := opts.UserDeviceName
	if userDeviceName == "" {
		userDeviceName = "iPhone"
	}

	locale := opts.Locale
	if locale == nil {
		locale = &seonsdk.LocaleConfig{
			RegionTimezone:     "-05:00",
			RegionLanguage:     "en",
			RegionCountry:      "US",
			TimezoneIdentifier: "America/New_York",
			CalendarIdentifier: "gregorian",
		}
	}

	brightness := RandomBrightness()
	batteryLevel := RandomBatteryLevel()
	uptime := RandomSystemUptime()
	volume := RandomAudioVolume()
	pasteboardHash := FormatPasteboardHash("", rand.Intn(500)+1)

	// en0 and def_gw are generated with independent random subnets to match the legacy
	// pattern this SDK has been emitting. On a real iPhone they would share a /24 (the
	// gateway is always the .1 of the device's own subnet), but changing this now
	// invalidates SEON's learned trust cluster for existing users. Pass matching values
	// explicitly if you need a real-subnet-style fingerprint.
	// Match the /24 between en0 and def_gw — impossible pairs (different
	// subnets) are an instant tell.
	nwInterfaces, defGw := resolveNwInterfacesAndGateway(nil, "")

	hasCellular := rand.Float64() < 0.85
	if hasCellular {
		AddCellularInterfaces(nwInterfaces)
	}

	cfg := &seonsdk.FingerprintConfig{
		SkipDC:           opts.SkipDC,
		Proxy:            opts.Proxy,
		ReturningSession: opts.ReturningSession,

		Locale: *locale,

		Device: seonsdk.DeviceConfig{
			AccessoriesCount:    0,
			DeviceName:          defaults.DeviceName,
			DeviceAdid:          deviceAdid,
			DeviceOrientation:   "Portrait",
			DeviceUdid:          deviceUdid,
			IosDeviceName:       userDeviceName,
			IosVersion:          defaults.IosVersion,
			PasteboardHash:      pasteboardHash,
			ScreenBrightness:    int(brightness * 100),
			ScreenHeight:        int(defaults.ScreenHeight),
			ScreenWidth:         int(defaults.ScreenWidth),
			IcloudUbiquityToken: icloudToken,
			IsScreenCaptured:    false,
		},

		Hardware: seonsdk.HardwareConfig{
			BatteryLevel:    batteryLevel,
			BatteryCharging: false,
			CpuType:         defaults.CpuType,
			CpuCount:        defaults.CpuCount,
			IsEmulator:      false,
			PhysicalMemory:  defaults.PhysicalMemory,
			SystemUptime:    uptime,
		},

		Media: seonsdk.MediaConfig{
			AudioMuteStatus:    false,
			AudioVolumeCurrent: volume,
			CanSendMail:        true,
			CanSendText:        true,
		},

		Network: seonsdk.NetworkConfig{
			VpnState:       "NOT_CONNECTED",
			ProxyAddress:   nil,
			ProxyState:     "NOT_CONNECTED",
			WifiSsid:       nil,
			WifiMacAddress: nil,
		},

		OS: seonsdk.OSConfig{
			KernelVersion:      defaults.KernelVersion,
			KernelName:         defaults.KernelName,
			KernelArch:         defaults.KernelArch,
			IsJailbroken:       false,
			IsIosAppOnMac:      false,
			IsDebuggerAttached: false,
		},

		Telephony: seonsdk.TelephonyConfig{
			NetworkConfig: "WIFI",
			IsOnCall:      false,
		},

		Accessibility: seonsdk.AccessibilityConfig{
			IsBoldTextEnabled:           rand.Float64() < 0.08,
			IsReduceMotionEnabled:       false,
			IsVideoAutoplayEnabled:      true,
			IsAssisstiveTouchRunning:    false,
			IsReduceTransparencyEnabled: false,
			IsDarkerSystemColorsEnabled: false,
			PreferredContentSize:        RandomPreferredContentSize(),
			MfiHearingAidsStatus:        0,
		},

		Auth: seonsdk.AuthConfig{
			IsPasscodeEnabled:   true,
			IsBiometricsEnabled: true,
		},

		GeolocationConfig: seonsdk.DefaultGeolocationConfig(),

		Experimental: seonsdk.ExperimentalConfig{
			ForceTouchSupport:        forceTouchForSpec(defaults),
			DisplayGamut:             defaults.DisplayGamut,
			UserInterfaceStyle:       RandomUserInterfaceStyle(),
			ThermalState:             0,
			NativeBounds:             defaults.NativeBounds,
			IsDisplayZoomEnabled:     false,
			ConnectedAccessories:     0,
			IsPluggedIn:              false,
			IsArm64:                  defaults.IsArm64,
			DateFormat:               DateFormatForLocale(locale.RegionLanguage, locale.RegionCountry),
			IsLowPowerEnabled:        false,
			IsReactNative:            opts.IsReactNative,
			IsRootFsCompromised:      false,
			HasInvalidCodeSig:        false, // fcntl(F_ADDSIGS=59, NULL) returns -1 on all devices → (−1==0)=false
			Pid:                      1,
			IsFlutter:                opts.IsFlutter,
			IsCellularDataRestricted: false,
			RadioTechnology:          hasCellular,
			VpnProtocol:              nil,
			ProxyType:                nil,
			ApIsHotspot:              false,
			NwInterfaces:             nwInterfaces,
			DefGw:                    defGw,
			// supported_biometry_type — OMITTED by default. Real iPhone
			// captures show this field absent from the payload when the
			// authentication probe's experimental phase didn't complete
			// (performance_probes["8"]["2"] = null, per IDA 0x197c0 +
			// merge at 0x1cd44). Since our BuildPerformanceProbes always
			// reports auth's experimental as null, we match by omitting
			// this field. Set explicitly (e.g., "FaceID") to force-emit.
			SupportedBiometryType:    nil,
		},

		PortScanResult: []int{},
	}

	return cfg, nil
}

func forceTouchForSpec(defaults *DeviceDefaults) string {
	if defaults.Has3DTouch {
		return "Available"
	}
	return "Unavailable"
}

// randomUUID returns an uppercase RFC 4122 v4 UUID — matching what
// `[NSUUID UUID].UUIDString` produces on a real iPhone for both
// `identifierForVendor` (IDFV / device_udid) and `advertisingIdentifier`
// (IDFA / device_adid when tracking is granted).
//
// Earlier versions of this helper formatted raw random bytes without
// setting the version (4) and variant (10) bits, which produced UUIDs
// like `977CB1E4-0DD6-A6DA-2ACE-8F4F3A5BD78D`. Real iOS UUIDs ALWAYS
// have segment 3 starting with `4` and segment 4 starting with `8/9/A/B`;
// any service that runs an RFC-4122 v4 validator on the IDFV would flag
// the malformed value as a synthetic-fingerprint signal. Verified against
// an iPhone 14 Pro capture where IDFV =`E705F82C-97CF-411F-89B4-...`
// (segment 3 = `411F`, segment 4 = `89B4`).
func randomUUID() string {
	return strings.ToUpper(uuid.New().String())
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
