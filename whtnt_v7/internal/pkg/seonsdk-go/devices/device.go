package devices

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	tls_client "github.com/bogdanfinn/tls-client"
	seonsdk "status403.com/rsavv/seonsdk-go"
)

// Device represents a persistent virtual iOS device.
//
// All stable identity fields are frozen at creation time (mirroring the iOS
// keychain caching of device_hash, device_udid, and icloud_ubiquity_token).
// Calling NewFingerprint() produces a fresh FingerprintConfig with only the
// fields that would realistically change between scans on a real device:
// battery drift, uptime monotonic increase, brightness/volume re-randomized,
// pasteboard hash regenerated, auto dark-mode variance, etc.
//
// The first NewFingerprint() call reports current_hash_provider=0 (iCloud)
// or 2 (IDFV) — a "fresh" read. Subsequent calls report 1 or 3 — a "cached"
// read, matching what the real SDK does after it has populated the keychain.
type Device struct {
	machine  string
	defaults *DeviceDefaults

	deviceName          string
	userDeviceName      string
	deviceUdid          string
	deviceAdid          string
	icloudUbiquityToken interface{}

	nwInterfaces map[string]interface{}
	defGw        string

	locale       *seonsdk.LocaleConfig
	localePreset LocalePreset
	dateFormat   string

	isBoldTextEnabled           bool
	isReduceMotionEnabled       bool
	isVideoAutoplayEnabled      bool
	isAssisstiveTouchRunning    bool
	isReduceTransparencyEnabled bool
	isDarkerSystemColorsEnabled bool
	preferredContentSize        string
	mfiHearingAidsStatus        int

	isPasscodeEnabled   bool
	isBiometricsEnabled bool

	canSendMail bool
	canSendText bool

	isFlutter     bool
	isReactNative bool

	supportedBiometryType string

	httpClient        tls_client.HttpClient
	proxy             string
	skipDC             bool
	simulateDCTimeout  bool
	dcSyntheticSuccess bool
	dcDeviceIP         string
	dcAcceptLanguage  string
	deviceHashCached  bool

	behaviour         *BehaviourOptions
	location          *LocationOptions
	enableGeolocation bool
	network           NetworkState
	noCellService     bool

	createdAt        time.Time
	initialUptimeSec int64
	initialBattery   int

	// pasteboardHash is frozen at Device creation. On a real iPhone the
	// hash is derived from UIPasteboard.detectPatternsForPatterns + a
	// change counter, so it stays stable across back-to-back SEON scans
	// as long as the clipboard doesn't actually change. Re-rolling per
	// fingerprint would emit a different hash on every call from the same
	// device within a single signup session — an immediate tell.
	pasteboardHash string
	pluggedIn      bool

	// screenBrightness, audioVolume and uiStyle are frozen at Device
	// creation for the same reason as pasteboardHash: they describe user
	// state that does not change between two SEON scans seconds apart.
	//
	// testdata/freshwhatnot holds three fingerprints spanning 116 s of one
	// app session, and reports brightness 44, volume 30 and "Dark" in all
	// three. Re-rolling per fingerprint produced swings like 48 → 85 → 47
	// and Dark → Light → Light inside a few seconds, which is not something
	// a handset does and is trivially correlated across a signup flow.
	screenBrightness float64
	audioVolume      int
	uiStyle          string

	// cellularInterfaces holds the frozen pdp_ip* entries merged into
	// nw_interfaces whenever cell service is available. Frozen for the same
	// reason as the fields above: a PDP context keeps its carrier-assigned
	// address for the life of the attachment.
	cellularInterfaces map[string]interface{}

	mu              sync.Mutex
	generationCount int
}

// DeviceOptions configures NewDevice().
type DeviceOptions struct {
	Machine    string // e.g. "iPhone16,2"
	IOSVersion string // e.g. "18.2"

	UserDeviceName      string // user-chosen name (ios_device_name)
	DeviceUdid          string // IDFV — empty = auto-generate uppercase UUID
	DeviceAdid          string // IDFA — empty = auto (zeros when TrackingGranted=false)
	TrackingGranted     bool   // if true and DeviceAdid empty, generates a random UUID
	IcloudUbiquityToken interface{}
	IcloudSignedIn      bool // if true and token nil, auto-generates a token

	// DeviceHashCached, when true, makes NewFingerprint() behave as if the
	// real SDK's keychain already holds a `device_hash` entry — the
	// `computeHashFor` path at IDA 0x1bd84 is then skipped and
	// `implicit_device_hash` is NOT emitted into experimental_fields even
	// when the current_hash_provider is 0 (fresh iCloud token) or 2
	// (fresh IDFV).
	//
	// Models the real-device state where the iCloud token was re-fetched
	// (e.g., user signed in/out of iCloud) but the app's keychain still
	// carries a device_hash from a prior session — verified against an
	// iPhone 17 Pro capture with `current_hash_provider: 0` and NO
	// `implicit_device_hash` key.
	//
	// Default false (implicit emitted whenever provider ∈ {0, 2, 4}).
	DeviceHashCached bool

	Locale *seonsdk.LocaleConfig // nil = en-US default

	// LocalePreset is a one-field shortcut that fills Locale +
	// DCAcceptLanguage + (when BehaviourMonitoring.MagneticDeclination is
	// zero) the magnetic declination with a coherent bundle. Ignored when
	// Locale is set explicitly — the caller has taken ownership of the
	// fields. Presets: LocaleUKEnglish (default), LocaleUSEnglish,
	// LocalePolishPoland, LocaleJapaneseJapan, etc. — see devices/locale.go.
	LocalePreset LocalePreset

	HTTPClient tls_client.HttpClient
	Proxy      string
	SkipDC     bool

	// SimulateDCTimeout, when combined with SkipDC=true, makes performance_network_call
	// report ~2000ms to match the pattern of a genuinely unreachable DC endpoint.
	// See FingerprintConfig.SimulateDCTimeout for details.
	SimulateDCTimeout bool

	// DCSyntheticSuccess, when combined with SkipDC=true, fakes a successful
	// DC roundtrip locally. The resulting fingerprint has a populated
	// dynamic_evaluator_results, empty stored_errors, and realistic
	// performance_network_call timings sampled from the legit corpus band.
	// See FingerprintConfig.DCSyntheticSuccess for the full semantics.
	DCSyntheticSuccess bool

	// DCDeviceIP is the IPv4 the synthetic DC response will claim the SEON
	// server observed for this client. Only used when DCSyntheticSuccess is
	// true; should be a routable IP (typically the proxy's egress).
	DCDeviceIP string

	// DCAcceptLanguage overrides the Accept-Language header that the DC request sends.
	// If empty, it is auto-derived from Locale (e.g. {en, GB} → "en-GB,en;q=0.9").
	// Set this explicitly if you need a non-obvious pattern — e.g. a multi-language
	// user with Japanese + English preferences.
	//
	// Common values:
	//   "en-US,en;q=0.9"   US English iPhone
	//   "en-GB,en;q=0.9"   UK English iPhone
	//   "pl-PL,pl;q=0.9,en-US;q=0.8,en;q=0.7"   Polish iPhone with English fallback
	DCAcceptLanguage string

	// BehaviourMonitoring, when non-nil + Enabled, makes NewFingerprint()
	// synthesise a `behaviour_results` block via devices/behaviour.Simulate.
	// Match real-world usage: set this only on login/phone-signup-style
	// fingerprints (the three Whatnot sites at 0x1001df9a4, 0x10026f1b0 —
	// see docs/06-round-3-corrections.md). On address-mutation or other
	// non-behaviour paths, leave nil so the payload's behaviour_results
	// stays NSNull-equivalent (the real SDK does the same when
	// monitoringIsRunning == 0).
	BehaviourMonitoring *BehaviourOptions

	// Location, when non-nil AND EnableGeolocation is true, makes every
	// NewFingerprint() populate cfg.DeviceLocation and
	// cfg.Experimental.ExperimentalDeviceLocation by calling
	// GenerateDeviceLocation() + GenerateExperimentalDeviceLocation() with
	// these options.
	//
	// Setting Location alone has NO EFFECT unless EnableGeolocation is
	// also set to true — this prevents accidental emission of geo data
	// when the host app (e.g. Whatnot) never calls setGeolocationConfig:.
	// Real Whatnot fingerprints have `geolocation_config.geolocationEnabled:
	// false` and `device_location: null` on every call.
	//
	// Populate this only when simulating a host app that EXPLICITLY enables
	// geolocation (not Whatnot). Examples: social/dating apps that ask for
	// "while using" location permission up front.
	Location *LocationOptions

	// EnableGeolocation — the opt-in switch for emitting device_location
	// and experimental_device_location, and for setting
	// geolocation_config.geolocationEnabled to true.
	//
	// Default (false) reproduces Whatnot-style payloads (geo disabled,
	// device_location null). Only flip to true when simulating an app
	// that actually calls setGeolocationConfig: with geolocation enabled.
	EnableGeolocation bool

	// Network sets the high-level connectivity state that drives the
	// `telephony.network_config` and `experimental_fields.radio_technology`
	// payload fields. Default (zero value) is NetworkWiFi, which matches
	// the hardcoded "WIFI" the legacy builder used to emit. Override per
	// fingerprint via FingerprintOverrides.Network.
	//
	// IMPORTANT: changing this from the default WiFi to cellular states
	// puts `radio_technology: "CTRadioAccessTechnologyLTE"` (or similar)
	// into `experimental_fields`. If your proxy IP looks like a residential
	// or datacenter address and the SEON backend cross-checks the claimed
	// radio_tech vs IP reputation, a mismatch bans. Default WiFi is the
	// safest baseline unless you know your proxy IP looks cellular.
	Network NetworkState

	// NoCellService makes `experimental_fields.radio_technology` false while
	// on WiFi, modelling a SIM-less, eSIM-unprovisioned, or airplane-mode
	// device.
	//
	// The default (false → cell service IS available) matches
	// testdata/freshwhatnot, where all three payloads report
	// `network_config: "WIFI"` together with `radio_technology: true`: a
	// normal iPhone with an active SIM that happens to be on WiFi.
	// radio_technology is isCellServiceAvailable, which does not depend on
	// the primary data path.
	//
	// Note this also gates the cellular entries in
	// `experimental_fields.nw_interfaces` (pdp_ip0 / pdp_ip2) — a device
	// reporting cell service should show carrier interfaces, and the real
	// capture does. Setting NoCellService suppresses both together.
	//
	// Ignored for non-WiFi Network states, where the answer is already
	// determined: on LTE/5G cell service is necessarily available, and on
	// NetworkNone it necessarily is not.
	NoCellService bool

	IsFlutter     bool
	IsReactNative bool

	// Optional overrides — if empty, sensible defaults are used.
	PreferredContentSize  string                 // "" = randomize at creation
	SupportedBiometryType string                 // "" = "FaceID" for iPhone 11+
	NwInterfaces          map[string]interface{} // nil = auto 192.168.x.x
	DefGw                 string                 // "" = derived from NwInterfaces

	// Initial device runtime state. If zero, randomized at creation.
	InitialBatteryLevel int   // 0 = random 20-100
	InitialUptimeSec    int64 // 0 = random 1h-7d
	PluggedIn           bool  // initial plug state
}

// NewDevice creates a persistent virtual device. All stable identity is
// frozen here and reused across NewFingerprint() calls.
//
// Returns an error if the iOS version cannot run on the given machine (e.g. asking
// for iPhone 16 Plus on iOS 17 — impossible, since iPhone 16 shipped with iOS 18).
// Use PickCompatibleIOSVersion(machine) or CompatibleIOSVersions(machine) if you
// want a valid version for a given device.
func NewDevice(opts DeviceOptions) (*Device, error) {
	if LookupSpec(opts.Machine) == nil {
		return nil, fmt.Errorf("unknown device model: %s", opts.Machine)
	}
	if err := ValidateIOSVersion(opts.Machine, opts.IOSVersion); err != nil {
		return nil, err
	}
	defaults := BuildDefaults(opts.Machine, opts.IOSVersion)
	if defaults == nil {
		return nil, fmt.Errorf("failed to build defaults for %s / %s", opts.Machine, opts.IOSVersion)
	}

	udid := opts.DeviceUdid
	if udid == "" {
		udid = RandomDeviceUdid()
	}
	adid := opts.DeviceAdid
	if adid == "" {
		adid = RandomDeviceAdid(opts.TrackingGranted)
	}
	token := opts.IcloudUbiquityToken
	if token == nil && opts.IcloudSignedIn {
		token = RandomIcloudUbiquityToken()
	}

	userName := opts.UserDeviceName
	if userName == "" {
		userName = "iPhone"
	}

	// Resolve locale: explicit Locale wins; otherwise a LocalePreset (if
	// set to anything other than the zero value's bundle) fills Locale +
	// DCAcceptLanguage coherently; otherwise UK-English is the baseline.
	presetBundle := LocaleFromPreset(opts.LocalePreset)
	locale := opts.Locale
	if locale == nil {
		l := presetBundle.Locale
		locale = &l
	}
	if opts.DCAcceptLanguage == "" && opts.Locale == nil {
		// Only auto-fill DCAcceptLanguage from preset when the caller
		// hasn't set either. If they set Locale explicitly, they're on the
		// resolveAcceptLanguage path (derives from the Locale).
		opts.DCAcceptLanguage = presetBundle.DCAcceptLanguage
	}

	// Validate coherence — loud error on impossible combinations (e.g.
	// RegionCountry = "GB" with TimezoneIdentifier = "Asia/Tokyo"). Loose;
	// empty fields skip their checks.
	if err := ValidateLocale(*locale, opts.DCAcceptLanguage); err != nil {
		return nil, err
	}

	prefSize := opts.PreferredContentSize
	if prefSize == "" {
		prefSize = RandomPreferredContentSize()
	}

	// supported_biometry_type: default by machine model.
	//
	// Earlier comment claimed the field is omitted whenever the auth probe's
	// experimental phase didn't complete (one iPhone 14 Pro capture had
	// performance_probes["8"]["2"] = null + no supported_biometry_type). The
	// 20-fingerprint iPhone 15 / iOS 26.5 corpus shipped in legitfp/decoded.json
	// contradicts that read: every single capture has performance_probes["8"]["2"]
	// populated AND supported_biometry_type = "FaceID". Treating the iPhone 14 Pro
	// row as the canonical pattern was over-fitting to one outlier. Real iPhone 15
	// traffic always populates this, so default it for FaceID-class devices.
	//
	// FaceID started with iPhone X (iPhone10,3/10,6) and is universal on
	// iPhone 11 / 12 / 13 / 14 / 15 / 16 / 17 / 18. Earlier non-X iPhones
	// (iPhone8,*, iPhone9,*, iPhone10,1/10,2/10,4/10,5) ran TouchID. SE 2/3
	// (iPhone12,8 / iPhone14,6) are TouchID. Callers may still override.
	biometry := opts.SupportedBiometryType
	if biometry == "" {
		biometry = defaultSupportedBiometryType(opts.Machine)
	}

	// en0 and def_gw use independent random subnets by default to match the legacy
	// pattern this SDK has emitted historically. On a real iPhone they share a /24,
	// `en0` and `def_gw` MUST share a /24 subnet — any real consumer router
	// hands out client IPs in its own subnet. Generating them independently
	// (the previous behaviour) produced impossible configs like
	// `en0=192.168.3.169` + `def_gw=192.168.4.1`, which SEON flags.
	nw, gw := resolveNwInterfacesAndGateway(opts.NwInterfaces, opts.DefGw)

	initBattery := opts.InitialBatteryLevel
	if initBattery <= 0 {
		initBattery = RandomBatteryLevel()
	}
	initUptime := opts.InitialUptimeSec
	if initUptime <= 0 {
		initUptime = RandomSystemUptime()
	}

	return &Device{
		machine:                     opts.Machine,
		defaults:                    defaults,
		deviceName:                  defaults.DeviceName,
		userDeviceName:              userName,
		deviceUdid:                  udid,
		deviceAdid:                  adid,
		icloudUbiquityToken:         token,
		nwInterfaces:                nw,
		defGw:                       gw,
		locale:                      locale,
		localePreset:                resolveLocalePreset(opts, locale),
		dateFormat:                  DateFormatForLocale(locale.RegionLanguage, locale.RegionCountry),
		isBoldTextEnabled:           rand.Float64() < 0.08,
		isReduceMotionEnabled:       false,
		isVideoAutoplayEnabled:      true,
		isAssisstiveTouchRunning:    false,
		isReduceTransparencyEnabled: false,
		isDarkerSystemColorsEnabled: false,
		preferredContentSize:        prefSize,
		mfiHearingAidsStatus:        0,
		isPasscodeEnabled:           true,
		isBiometricsEnabled:         true,
		canSendMail:                 true,
		canSendText:                 true,
		isFlutter:                   opts.IsFlutter,
		isReactNative:               opts.IsReactNative,
		supportedBiometryType:       biometry,
		httpClient:                  opts.HTTPClient,
		proxy:                       opts.Proxy,
		skipDC:                      opts.SkipDC,
		simulateDCTimeout:           opts.SimulateDCTimeout,
		dcSyntheticSuccess:          opts.DCSyntheticSuccess,
		dcDeviceIP:                  opts.DCDeviceIP,
		dcAcceptLanguage:            resolveAcceptLanguage(opts.DCAcceptLanguage, locale),
		deviceHashCached:            opts.DeviceHashCached,
		behaviour:                   opts.BehaviourMonitoring,
		location:                    opts.Location,
		enableGeolocation:           opts.EnableGeolocation,
		network:                     opts.Network,
		noCellService:               opts.NoCellService,
		screenBrightness:            RandomBrightness(),
		audioVolume:                 RandomAudioVolume(),
		uiStyle:                     RandomUserInterfaceStyle(),
		cellularInterfaces:          NewCellularInterfaces(),
		createdAt:                   time.Now(),
		initialUptimeSec:            initUptime,
		initialBattery:              initBattery,
		pasteboardHash:              FormatPasteboardHash("", rand.Intn(500)+1),
		pluggedIn:                   opts.PluggedIn,
	}, nil
}

// FingerprintOverrides lets the caller tweak per-fingerprint behavior.
// All fields are optional; the zero value means "use time-aware defaults".
type FingerprintOverrides struct {
	// PluggedIn overrides the plug state for this fingerprint only.
	// Default: inherits the device's last known plug state.
	PluggedIn *bool

	// OrientationLandscape, if true, reports a landscape orientation instead of Portrait.
	OrientationLandscape bool

	// IsScreenCaptured overrides the screen-capture state.
	IsScreenCaptured *bool

	// ThermalState overrides the thermal state (0=Nominal, 1=Fair, 2=Serious, 3=Critical).
	ThermalState *int

	// Behaviour, when non-nil, overrides the device's default
	// `DeviceOptions.BehaviourMonitoring` for this single fingerprint.
	// This is how you generate a WITH-behaviour fingerprint on a login /
	// phone-signup call site and a WITHOUT-behaviour one on an
	// address-mutation call site from the SAME *Device.
	//
	// Semantics:
	//   overrides.Behaviour == nil   → inherit device default
	//   overrides.Behaviour != nil   → use this value verbatim (including
	//                                  `&BehaviourOptions{Enabled: false}`
	//                                  to force-disable even if the device
	//                                  was created with monitoring on)
	Behaviour *BehaviourOptions

	// Location overrides the device's default DeviceOptions.Location for
	// this single fingerprint. A pointer-to-empty-struct
	// (`&LocationOptions{}`) is a valid override that produces a default
	// London fix; pass nil to inherit. To force NO location for one call
	// when the device default is set, use SuppressLocation.
	Location *LocationOptions

	// EnableGeolocation, when non-nil, overrides the device's default
	// EnableGeolocation. Use `&true` to turn geo ON for one call (e.g.
	// after user grants location permission mid-session) or `&false` to
	// force it OFF. Pairs with Location.
	EnableGeolocation *bool

	// SuppressLocation, when true, force-disables geolocation emission
	// regardless of the device's EnableGeolocation / Location defaults.
	// Shorthand for `EnableGeolocation = &false`.
	SuppressLocation bool

	// Network overrides DeviceOptions.Network for this fingerprint. Pass a
	// pointer so you can explicitly force NetworkWiFi (the zero value)
	// without being ambiguous.
	Network *NetworkState
}

// NewFingerprint builds a fresh FingerprintConfig for the device.
//
// Stable identity is reused from the Device struct. The rest either drifts
// with elapsed time or is frozen for the device's lifetime:
//   - battery drifts monotonically ~0.3%/min (or up when plugged in)
//   - system_uptime increases monotonically by the real elapsed time
//   - brightness, volume, pasteboard_hash and UI style are FROZEN at Device
//     creation — testdata/freshwhatnot reports identical values for all
//     three fingerprints in one 116 s app session, so re-rolling them per
//     call is a cross-fingerprint tell
//   - core_seed, performance_probes and metal_info.duration_ms ARE re-rolled
//     per fingerprint; those are genuinely per-scan measurements
//   - current_hash_provider flips from fresh (0/2) to cached (1/3) after the first call
func (d *Device) NewFingerprint(overrides ...FingerprintOverrides) *seonsdk.FingerprintConfig {
	var ov FingerprintOverrides
	if len(overrides) > 0 {
		ov = overrides[0]
	}

	d.mu.Lock()
	d.generationCount++
	gen := d.generationCount
	if ov.PluggedIn != nil {
		d.pluggedIn = *ov.PluggedIn
	}
	pluggedIn := d.pluggedIn
	elapsed := time.Since(d.createdAt)
	d.mu.Unlock()

	batteryLevel := computeBatteryDrift(d.initialBattery, elapsed, pluggedIn)
	uptimeSec := d.initialUptimeSec + int64(elapsed.Seconds())

	// Verified via IDA:
	//   - isBatteryCharging returns true only when batteryState==2 (Charging, not Full)   — 0x1e018
	//   - isPluggedIn        returns true when batteryState ∈ {2, 3} (Charging or Full)   — 0x1e094
	// So when battery is at 100% + plugged in: charging=false, plugged=true (Full state).
	batteryCharging := pluggedIn && batteryLevel < 100

	// All four are frozen at Device creation — see the struct fields for the
	// corpus evidence.
	brightness := d.screenBrightness
	volume := d.audioVolume
	pasteboardHash := d.pasteboardHash
	uiStyle := d.uiStyle

	orientation := "Portrait"
	if ov.OrientationLandscape {
		if rand.Float64() < 0.5 {
			orientation = "Landscape Left"
		} else {
			orientation = "Landscape right"
		}
	}

	isScreenCaptured := false
	if ov.IsScreenCaptured != nil {
		isScreenCaptured = *ov.IsScreenCaptured
	}

	thermalState := 0
	if ov.ThermalState != nil {
		thermalState = *ov.ThermalState
	}

	cfg := &seonsdk.FingerprintConfig{
		HTTPClient:         d.httpClient,
		SkipDC:             d.skipDC,
		SimulateDCTimeout:  d.simulateDCTimeout,
		DCSyntheticSuccess: d.dcSyntheticSuccess,
		DCDeviceIP:         d.dcDeviceIP,
		DCAcceptLanguage:   d.dcAcceptLanguage,
		Proxy:              d.proxy,
		ReturningSession:   gen > 1,
		DeviceHashCached:   d.deviceHashCached,
		Codename:           d.defaults.Machine,

		Locale: *d.locale,

		Device: seonsdk.DeviceConfig{
			AccessoriesCount:    0,
			DeviceName:          d.deviceName,
			IosDeviceName:       d.userDeviceName,
			DeviceAdid:          d.deviceAdid,
			DeviceOrientation:   orientation,
			DeviceUdid:          d.deviceUdid,
			IosVersion:          d.defaults.IosVersion,
			PasteboardHash:      pasteboardHash,
			ScreenBrightness:    int(brightness * 100),
			ScreenHeight:        int(d.defaults.ScreenHeight),
			ScreenWidth:         int(d.defaults.ScreenWidth),
			IcloudUbiquityToken: d.icloudUbiquityToken,
			IsScreenCaptured:    isScreenCaptured,
		},

		Hardware: seonsdk.HardwareConfig{
			BatteryLevel:    batteryLevel,
			BatteryCharging: batteryCharging,
			CpuType:         d.defaults.CpuType,
			CpuCount:        d.defaults.CpuCount,
			IsEmulator:      false,
			PhysicalMemory:  d.defaults.PhysicalMemory,
			SystemUptime:    uptimeSec,
		},

		Media: seonsdk.MediaConfig{
			// Verified via IDA:
			//   -[SEONMediaProbe audioMuteStatus] (0x1f000) returns self->_audioMuteStatus directly.
			//   The ivar is declared readonly (TB,R,N,V_audioMuteStatus) and NOTHING writes to it
			//   in the binary. +[SEONSharkfoodMuteSwitchDetector shared] is never called (no
			//   _objc_msgSend$shared stub exists for this selector), so the detector singleton
			//   is never instantiated and SEONMediaProbe.detector is never set via setDetector:.
			//   Result: audio_mute_status is always false in real SDK payloads. Dead-code feature.
			AudioMuteStatus:    false,
			AudioVolumeCurrent: volume,
			CanSendMail:        d.canSendMail,
			CanSendText:        d.canSendText,
		},

		Network: seonsdk.NetworkConfig{
			VpnState:     "NOT_CONNECTED",
			ProxyAddress: nil,
			ProxyState:   "NOT_CONNECTED",
			// WifiSsid / WifiMacAddress — correct iOS 14+ stub values for
			// apps WITHOUT the com.apple.developer.networking.HotspotHelper
			// entitlement (Whatnot doesn't have it). Verified at IDA:
			//   -[SEONNetworkProbe wifiSSID]  0x86e4 → CNCopyCurrentNetworkInfo
			//   -[SEONNetworkProbe wifiMACAddress] 0x8094 → CNCopyCurrentNetworkInfo + kCNNetworkInfoKeyBSSID
			// Both call sites wrap with +[SEONStringUtil ensureNonNil:]
			// (0x11eac) which returns NSNull.null when the CF value is nil
			// or an empty string. Since CNCopyCurrentNetworkInfo returns
			// NULL on iOS 14+ for non-entitled apps, both fields end up
			// null on the wire — which is what our Go `nil` serialises to.
			WifiSsid:       nil,
			WifiMacAddress: nil,
		},

		OS: seonsdk.OSConfig{
			KernelVersion:      d.defaults.KernelVersion,
			KernelName:         d.defaults.KernelName,
			KernelArch:         d.defaults.KernelArch,
			IsJailbroken:       false,
			IsIosAppOnMac:      false,
			IsDebuggerAttached: false,
		},

		// Telephony + ExperimentalConfig.RadioTechnology are set below by
		// TelephonyForNetwork(...).Apply(cfg).
		Telephony: seonsdk.TelephonyConfig{},

		Accessibility: seonsdk.AccessibilityConfig{
			IsBoldTextEnabled:           d.isBoldTextEnabled,
			IsReduceMotionEnabled:       d.isReduceMotionEnabled,
			IsVideoAutoplayEnabled:      d.isVideoAutoplayEnabled,
			IsAssisstiveTouchRunning:    d.isAssisstiveTouchRunning,
			IsReduceTransparencyEnabled: d.isReduceTransparencyEnabled,
			IsDarkerSystemColorsEnabled: d.isDarkerSystemColorsEnabled,
			PreferredContentSize:        d.preferredContentSize,
			MfiHearingAidsStatus:        d.mfiHearingAidsStatus,
		},

		Auth: seonsdk.AuthConfig{
			IsPasscodeEnabled:   d.isPasscodeEnabled,
			IsBiometricsEnabled: d.isBiometricsEnabled,
		},

		GeolocationConfig: seonsdk.DefaultGeolocationConfig(),

		Experimental: seonsdk.ExperimentalConfig{
			ForceTouchSupport:    forceTouchForSpec(d.defaults),
			DisplayGamut:         d.defaults.DisplayGamut,
			UserInterfaceStyle:   uiStyle,
			ThermalState:         thermalState,
			NativeBounds:         d.defaults.NativeBounds,
			IsDisplayZoomEnabled: false,
			ConnectedAccessories: 0,
			IsPluggedIn:          pluggedIn,
			IsArm64:              d.defaults.IsArm64,
			DateFormat:           d.dateFormat,
			// is_low_power_enabled is `NSProcessInfo.isLowPowerModeEnabled`.
			// iOS prompts the user to enable Low Power Mode at 20 %; in
			// practice many users accept the prompt once battery drops under
			// that threshold and LPM stays on until the next charge. We
			// flip it on when battery drifts below 25 % — gives us extra
			// fingerprint variance (the global low-power-off fleet already
			// exists, we want a plausible minority tail that emits true).
			IsLowPowerEnabled:        batteryLevel < 25,
			IsReactNative:            d.isReactNative,
			IsRootFsCompromised:      false,
			HasInvalidCodeSig:        false,
			Pid:                      1,
			IsFlutter:                d.isFlutter,
			IsCellularDataRestricted: false,
			RadioTechnology:          nil,
			VpnProtocol:              nil,
			ProxyType:                nil,
			ApIsHotspot:              false,
			NwInterfaces:             d.nwInterfaces,
			DefGw:                    d.defGw,
			SupportedBiometryType:    biometryForExperimental(d.supportedBiometryType),
			AvOutputInfo:             BuildAvOutputInfoFromLocale(*d.locale),
			ScreenBounds:             d.defaults.ScreenBounds,
			ScreenScale:              d.defaults.ScreenScale,
			NativeScale:              d.defaults.NativeScale,
			MetalInfo:                BuildMetalInfo(d.defaults.Machine, d.defaults.PhysicalMemory, uptimeSec),
			IsCarplayActive:          false,
			IsUnityHost:              false,
			UnityHostReason:          nil,
		},

		PortScanResult: []int{},
	}

	// Network state (default: WiFi). Always applied — every fingerprint
	// needs a telephony.network_config value. A per-call override wins; a
	// per-device default wins otherwise.
	network := d.network
	if ov.Network != nil {
		network = *ov.Network
	}
	tel := TelephonyForNetworkWithCellService(network, !d.noCellService)
	tel.Apply(cfg)

	// Merge the device's frozen cellular entries into a COPY. Mutating
	// cfg.Experimental.NwInterfaces directly would write through to
	// d.nwInterfaces — the config holds the same map — corrupting the
	// device's own (and persisted) network layout and re-rolling the carrier
	// IP on every call.
	if nw, ok := cfg.Experimental.NwInterfaces.(map[string]interface{}); ok {
		merged := make(map[string]interface{}, len(nw)+len(d.cellularInterfaces))
		for k, v := range nw {
			merged[k] = v
		}
		if tel.RadioTechnology {
			for k, v := range d.cellularInterfaces {
				merged[k] = v
			}
		}
		cfg.Experimental.NwInterfaces = merged
	}

	// Location auto-populate — opt-in via BOTH a non-nil Location and
	// EnableGeolocation=true. Either condition alone is a no-op. This
	// ensures geo is only emitted when the caller has explicitly said
	// "I'm simulating a host app that enables geolocation".
	//
	// When both are set, `device_location`, `experimental_device_location`,
	// and `geolocation_config.geolocationEnabled=true` are emitted together
	// — matching the real SDK's pairing at IDA 0x1bd30 + 0x173d0.
	//
	// Per-call overrides still work:
	//   overrides.Location != nil          → use overrides' LocationOptions
	//   overrides.SuppressLocation = true  → force no geo this call
	locOpts := d.location
	if ov.Location != nil {
		locOpts = ov.Location
	}
	emitGeo := d.enableGeolocation
	if ov.EnableGeolocation != nil {
		emitGeo = *ov.EnableGeolocation
	}
	if ov.SuppressLocation {
		emitGeo = false
	}
	if emitGeo && locOpts != nil {
		cfg.DeviceLocation = GenerateDeviceLocation(*locOpts)
		cfg.Experimental.ExperimentalDeviceLocation = GenerateExperimentalDeviceLocation(*locOpts)
		if cfg.GeolocationConfig == nil {
			cfg.GeolocationConfig = seonsdk.DefaultGeolocationConfig()
		}
		cfg.GeolocationConfig.GeolocationEnabled = true
	}

	// Behaviour — opt-in via options (Device default or override).
	behOpts := d.behaviour
	if ov.Behaviour != nil {
		behOpts = ov.Behaviour
	}
	if bd := buildBehaviourData(behOpts, d.machine); bd != nil {
		cfg.BehaviourResults = bd
	}

	return cfg
}

// biometryForExperimental converts a stored Device.supportedBiometryType
// into the value to emit in experimental_fields.supported_biometry_type.
// Empty string → nil (field omitted), matching the real-SDK case where the
// authentication probe's experimental phase didn't complete. See the
// comment on NewDevice's biometry resolution for why this is the default.
func biometryForExperimental(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// defaultSupportedBiometryType returns the biometry kind iOS would report
// for the given machine model. Returns empty for models we don't know about,
// which preserves the legacy "omit the field" behaviour for unknown devices.
//
// FaceID models: iPhone X (10,3 / 10,6) and every iPhone 11+ EXCEPT the
// SE 2 (iPhone12,8) and SE 3 (iPhone14,6), which are TouchID home-button
// devices. Pre-X iPhones (iPhone8,*, iPhone9,*, iPhone10,1/2/4/5) are
// TouchID. iPad models are deliberately out of scope here; they don't
// appear in the corpus.
func defaultSupportedBiometryType(machine string) string {
	switch machine {
	// TouchID — pre-X iPhones.
	case "iPhone8,1", "iPhone8,2", "iPhone8,4",
		"iPhone9,1", "iPhone9,2", "iPhone9,3", "iPhone9,4",
		"iPhone10,1", "iPhone10,2", "iPhone10,4", "iPhone10,5":
		return "TouchID"
	// TouchID — SE 2 (iPhone12,8) and SE 3 (iPhone14,6).
	case "iPhone12,8", "iPhone14,6":
		return "TouchID"
	}
	// FaceID for everything else we know about (iPhone X and 11+ non-SE).
	if _, known := ChipIdentifiers[machine]; known {
		return "FaceID"
	}
	return ""
}

// GenerationCount returns how many fingerprints have been generated for this device.
func (d *Device) GenerationCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.generationCount
}

// Machine returns the machine identifier (e.g. "iPhone16,2").
func (d *Device) Machine() string { return d.machine }

// Locale returns a copy of the Device's resolved LocaleConfig (frozen at
// construction — calendar, language, country, timezone).
func (d *Device) Locale() seonsdk.LocaleConfig { return *d.locale }

// LocalePreset returns the preset the Device was constructed with, or — when
// the caller passed a custom LocaleConfig with no explicit preset — the
// best-matching preset by RegionCountry. Falls back to LocaleUKEnglish when
// no country maps.
func (d *Device) LocalePreset() LocalePreset { return d.localePreset }

// resolveLocalePreset picks the preset to store on the Device. If the caller
// supplied a LocalePreset directly, that wins. If the caller supplied a
// custom Locale instead, we reverse-map by RegionCountry so helpers keyed on
// LocalePreset (BehaviourMagField, BehaviourMagDeclination) still work.
func resolveLocalePreset(opts DeviceOptions, locale *seonsdk.LocaleConfig) LocalePreset {
	// Explicit Locale from the caller → reverse-lookup by country code.
	if opts.Locale != nil {
		return presetForCountry(locale.RegionCountry)
	}
	return opts.LocalePreset
}

// presetForCountry maps an ISO-3166 alpha-2 country code to a LocalePreset.
// Unknown country → LocaleUKEnglish (the package's zero-value default).
func presetForCountry(country string) LocalePreset {
	switch country {
	case "GB":
		return LocaleUKEnglish
	case "US":
		return LocaleUSEnglish
	case "IE":
		return LocaleIrishEnglish
	case "DE":
		return LocaleGermanGermany
	case "FR":
		return LocaleFrenchFrance
	case "ES":
		return LocaleSpanishSpain
	case "IT":
		return LocaleItalianItaly
	case "PL":
		return LocalePolishPoland
	case "NL":
		return LocaleDutchNetherlands
	case "JP":
		return LocaleJapaneseJapan
	case "KR":
		return LocaleKoreanKorea
	case "AU":
		return LocaleAustraliaEnglish
	case "CA":
		return LocaleCanadaEnglish
	}
	return LocaleUKEnglish
}

// DeviceUdid returns the cached IDFV.
func (d *Device) DeviceUdid() string { return d.deviceUdid }

// DeviceAdid returns the cached IDFA.
func (d *Device) DeviceAdid() string { return d.deviceAdid }

// IcloudUbiquityToken returns the cached iCloud token (or nil).
func (d *Device) IcloudUbiquityToken() interface{} { return d.icloudUbiquityToken }

// IosVersion returns the iOS version this device was constructed with
// (e.g. "18.7.6"). Frozen at NewDevice time.
func (d *Device) IosVersion() string { return d.defaults.IosVersion }

// UserDeviceName returns the user-chosen device name (ios_device_name —
// e.g. "John's iPhone"). Frozen at NewDevice time.
func (d *Device) UserDeviceName() string { return d.userDeviceName }

// SetPluggedIn updates the device's plug state. This influences battery drift
// (plugged → battery climbs; unplugged → battery drains).
func (d *Device) SetPluggedIn(plugged bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pluggedIn = plugged
}

// SetHTTPClient attaches a shared tls_client.HttpClient to this Device. When
// non-nil, DC calls reuse it instead of building a fresh client per
// fingerprint — diverges from the real SDK's one-shot behaviour, so only
// use this when you explicitly want connection reuse (shared cookie jar,
// custom proxy auth, rate-limiting, etc.).
//
// Pass nil to clear a previously-attached client; subsequent DC calls
// will revert to fresh-per-call sessions (the real-SDK-matching default).
func (d *Device) SetHTTPClient(s tls_client.HttpClient) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.httpClient = s
}

// resolveAcceptLanguage derives what NSURLSession would send for `Accept-Language`
// on a real iPhone whose primary language is (RegionLanguage, RegionCountry).
//
// If the caller passed an explicit override it wins verbatim. Otherwise:
//   - English primary (`en-XX`):  "en-XX,en;q=0.9"
//   - Non-English with country:   "LL-CC,LL;q=0.9,en-US;q=0.8,en;q=0.7"
//     (the standard iOS pattern — primary locale, then language family, then
//     English fallback — matches what mitmproxy captures from UK/DE/PL/JP devices)
//   - Missing country or language: "en-US,en;q=0.9" (safe default)
func resolveAcceptLanguage(override string, locale *seonsdk.LocaleConfig) string {
	if override != "" {
		return override
	}
	if locale == nil {
		return "en-US,en;q=0.9"
	}
	lang := locale.RegionLanguage
	country := locale.RegionCountry
	if lang == "" || country == "" {
		return "en-US,en;q=0.9"
	}
	if lang == "en" {
		return fmt.Sprintf("en-%s,en;q=0.9", country)
	}
	return fmt.Sprintf("%s-%s,%s;q=0.9,en-US;q=0.8,en;q=0.7", lang, country, lang)
}

// computeBatteryDrift returns a realistic battery level based on real elapsed
// time since device creation. Drains ~0.3%/min when unplugged, charges ~0.5%/min
// when plugged in. Clamped to [1, 100].
func computeBatteryDrift(initial int, elapsed time.Duration, pluggedIn bool) int {
	minutes := elapsed.Minutes()
	ratePerMin := -0.3
	if pluggedIn {
		ratePerMin = 0.5
	}
	// No per-call jitter. A ±1% random term used to be added here "to avoid
	// deterministic values", but the drift over a few seconds is ~0.03%, so
	// the jitter dominated and made the level bounce — observed 55 → 56 → 55
	// across 6 s with battery_charging:false, i.e. a handset charging while
	// unplugged. Cross-device variety already comes from initial, which is
	// randomised per Device; within one device the level must be a
	// monotonic function of elapsed time.
	drift := ratePerMin * minutes
	level := float64(initial) + drift
	if level < 1 {
		level = 1
	}
	if level > 100 {
		level = 100
	}
	return int(level)
}
