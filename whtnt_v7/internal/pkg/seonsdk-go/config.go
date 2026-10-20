package seonsdk

import tls_client "github.com/bogdanfinn/tls-client"

type FingerprintConfig struct {
	SessionID  string
	SkipDC     bool
	Proxy      string
	HTTPClient tls_client.HttpClient

	Locale        LocaleConfig
	Device        DeviceConfig
	Hardware      HardwareConfig
	Media         MediaConfig
	Network       NetworkConfig
	OS            OSConfig
	Telephony     TelephonyConfig
	Accessibility AccessibilityConfig
	Auth          AuthConfig

	GeolocationConfig *GeolocationConfigValues
	DeviceLocation    *DeviceLocationData
	BehaviourResults  *BehaviourData

	Experimental ExperimentalConfig

	Codename  string
	CoreSeed  string
	RequestID string

	StoredErrors            interface{}
	PortScanResult          []int
	DynamicEvaluatorResults interface{}

	PerformanceActualProcessing string
	PerformanceExecTime         string
	PerformanceNetworkCall      string
	TimestampOverride           *int64

	ReturningSession bool // cached identifiers: hashProvider 1/3 instead of 0/2

	// DeviceHashCached, when true, suppresses `implicit_device_hash`
	// emission even when HashProvider is a fresh-compute value (0, 2, 4).
	// Models the real-device state where the iCloud token was re-fetched
	// but `device_hash` persisted in the keychain from a prior session —
	// `computeHashFor` at IDA 0x1bd84 is then skipped, `implicitDeviceHash`
	// stays nil, and the field is absent from the payload. Verified
	// against an iPhone 17 Pro capture with `current_hash_provider: 0` and
	// no `implicit_device_hash` key.
	DeviceHashCached bool

	// SimulateDCTimeout, when true and SkipDC is also true, sets performance_network_call
	// to ~2000ms (matching what a real iOS device produces when the DC endpoint is
	// unreachable and hits the SDK's hardcoded 2s NSURLSession timeout at 0x7918).
	// Useful for server-side setups that need to mimic the "DC always times out"
	// fingerprint pattern that SEON's backend treats as ambiguous.
	//
	// Has no effect when SkipDC is false (real DC timing is used in that case).
	//
	// PREFER DCSyntheticSuccess for new integrations: empirically, every legit
	// iPhone capture in our corpus has a populated dynamic_evaluator_results +
	// empty stored_errors, which DCSyntheticSuccess produces and SimulateDCTimeout
	// does not.
	SimulateDCTimeout bool

	// DCSyntheticSuccess, when true and SkipDC is also true, fakes a successful
	// DC server roundtrip without making any network call. The resulting payload
	// has:
	//   - dynamic_evaluator_results filled with a freshly AES-encrypted
	//     {"request_id":..., "device_ip":...} blob in the same wire format the
	//     real DC server emits;
	//   - stored_errors stays empty {};
	//   - performance_network_call sampled from the legit-corpus 380–560 ms band
	//     (instead of the 2 s timeout-shaped value).
	//
	// Use this on server-side runners that cannot reach the real DC endpoint but
	// want output that matches the dominant pattern in real iPhone traffic.
	// Set DCDeviceIP to the public IP the SEON server would have observed for
	// this client (typically the egress IP of the proxy chain). Has no effect
	// when SkipDC is false. SimulateDCTimeout takes precedence when both are
	// set, but mixing them is a configuration error and should not be done.
	DCSyntheticSuccess bool

	// DCDeviceIP is the public-facing IP that the synthetic DC response will
	// claim the SEON server observed for this client. Only used when
	// DCSyntheticSuccess is true. Should be a routable IPv4 — loopback or
	// RFC-1918 values would never appear in a real DC response.
	DCDeviceIP string

	// DCAcceptLanguage overrides the `Accept-Language` header that the DC request
	// sends. NSURLSession on a real iPhone derives this from [NSLocale preferredLanguages];
	// a US-English device emits `en-US,en;q=0.9`, a Polish device emits
	// `pl,en-US;q=0.9,en;q=0.8`, etc. If empty, defaults to `en-US,en;q=0.9`.
	//
	// Match this to the Locale you're simulating — a Polish-locale fingerprint
	// pairing with an English Accept-Language is a cheap inconsistency tell.
	DCAcceptLanguage string

	// SDKVersion overrides the SeonSDK `bundleVersion` (Info.plist
	// CFBundleShortVersionString) that this fingerprint claims. Empty =
	// use the library's compile-time default (version.DefaultBundleVersion,
	// currently "5.7.1"). Supplying a different value changes both:
	//
	//   source        → "ios-<SDKVersion>"      (from 0x1afd8)
	//   version_code  → "01" + MMmmpp formatted  (from 0x1cb64)
	//   DC payload.source (sent to seondnsresolve.com)
	//
	// Use this if Whatnot ships a newer SeonSDK version and you want to
	// emit matching fingerprints before upgrading the library itself. For
	// the overwhelming majority of callers, leave empty.
	SDKVersion string
}

type LocaleConfig struct {
	RegionTimezone     string
	RegionLanguage     string
	RegionCountry      string
	TimezoneIdentifier string
	CalendarIdentifier string

	// SpeakerName is the LOCALISED "Speaker" word iOS's AVAudioSession
	// emits for the built-in loudspeaker on this device/locale. It flows
	// into `experimental_fields.av_output_info.portName` and
	// `channels[*].channelName` (suffixed with " 1" / " 2").
	//
	// Verified examples:
	//
	//   en-*  → "Speaker"
	//   pl-PL → "Głośnik"   (iPhone 14 Pro capture)
	//   de-DE → "Lautsprecher"
	//   ja-JP → "スピーカー"
	//
	// Must be filled explicitly for custom locales not covered by a
	// LocalePreset. When empty, the payload builder falls back to the
	// English word "Speaker" — mark this as a known-wrong tell if your
	// Locale is non-English.
	SpeakerName string
}

type DeviceConfig struct {
	AccessoriesCount    interface{}
	DeviceAdid          string
	DeviceOrientation   interface{}
	DeviceUdid          string
	IosVersion          string
	PasteboardHash      string
	ScreenBrightness    interface{}
	ScreenHeight        interface{}
	ScreenWidth         interface{}
	IcloudUbiquityToken interface{}
	IsScreenCaptured    interface{}

	// DeviceName and IosDeviceName are no longer emitted in the payload
	// (removed in SDK 5.7.1) but are still used internally for device hash
	// computation via BuildStableDict.
	DeviceName    string
	IosDeviceName string
}

type HardwareConfig struct {
	BatteryLevel    interface{}
	BatteryCharging interface{}
	CpuType         string
	CpuCount        interface{}
	IsEmulator      interface{}
	PhysicalMemory  interface{}
	SystemUptime    interface{}
}

type MediaConfig struct {
	AudioMuteStatus    interface{}
	AudioVolumeCurrent interface{}
	CanSendMail        interface{}
	CanSendText        interface{}
}

type NetworkConfig struct {
	VpnState       interface{}
	ProxyAddress   interface{}
	ProxyState     interface{}
	WifiSsid       interface{}
	WifiMacAddress interface{}
}

type OSConfig struct {
	KernelVersion      string
	KernelName         string
	KernelArch         string
	IsJailbroken       interface{}
	IsIosAppOnMac      interface{}
	IsDebuggerAttached interface{}
}

type TelephonyConfig struct {
	NetworkConfig interface{}
	IsOnCall      interface{}
}

type AccessibilityConfig struct {
	IsBoldTextEnabled           interface{}
	IsReduceMotionEnabled       interface{}
	IsVideoAutoplayEnabled      interface{}
	IsAssisstiveTouchRunning    interface{}
	IsReduceTransparencyEnabled interface{}
	IsDarkerSystemColorsEnabled interface{}
	PreferredContentSize        interface{}
	MfiHearingAidsStatus        interface{}
}

type AuthConfig struct {
	IsPasscodeEnabled   interface{}
	IsBiometricsEnabled interface{}
}

type GeolocationConfigValues struct {
	GeolocationEnabled          bool
	GeolocationServiceTimeoutMs int64
	MaxGeoLocationCacheAgeSec   int64
	PrefetchEnabled             bool
}

type DeviceLocationData struct {
	Latitude        interface{}
	Longitude       interface{}
	Accuracy        interface{}
	AccuracyAuthLvl interface{}
	Status          interface{}
	AuthStatus      interface{}
	IsSimulated     interface{}
	IsCached        interface{}
	Timestamp       interface{}
}

type BehaviourData struct {
	// RawResults, when non-nil, is used verbatim as the `behaviour_results`
	// value in the fingerprint payload. This is the path the simulator at
	// devices/behaviour.Simulate uses — its output map already has the
	// exact set of keys the SDK emits (min_user_acc through
	// most_freq_attitude), pre-validated against IDA 0x1300c. When
	// RawResults is set, every scalar field below is ignored.
	//
	// Prefer this field for new integrations. The scalar fields remain for
	// legacy callers that hand-build the stats.
	RawResults map[string]interface{}

	MinUserAcc    interface{}
	MaxUserAcc    interface{}
	MeanUserAcc   interface{}
	MedianUserAcc interface{}
	VarUserAcc    interface{}
	P95UserAcc    interface{}

	MinRotRate    interface{}
	MaxRotRate    interface{}
	MeanRotRate   interface{}
	MedianRotRate interface{}
	VarRotRate    interface{}
	P95RotRate    interface{}

	MinGravity    interface{}
	MaxGravity    interface{}
	MeanGravity   interface{}
	MedianGravity interface{}
	VarGravity    interface{}
	P95Gravity    interface{}

	MinMagField    interface{}
	MaxMagField    interface{}
	MeanMagField   interface{}
	MedianMagField interface{}
	VarMagField    interface{}
	P95MagField    interface{}

	MagDeclination     interface{}
	HeadingChanges     interface{}
	OrientationChanges interface{}
	MonitoringDuration interface{}
	MostFreqAttitude   interface{}
}

type ExperimentalConfig struct {
	ForceTouchSupport    interface{}
	DisplayGamut         interface{}
	UserInterfaceStyle   interface{}
	ThermalState         interface{}
	NativeBounds         interface{}
	IsDisplayZoomEnabled interface{}
	ConnectedAccessories interface{}
	AccessoryHash        interface{}

	IsPluggedIn interface{}
	IsArm64     interface{}

	DateFormat string

	IsLowPowerEnabled   interface{}
	IsReactNative       interface{}
	IsRootFsCompromised interface{}
	HasInvalidCodeSig   interface{}
	Pid                 interface{}
	IsFlutter           interface{}

	IsCellularDataRestricted interface{}
	RadioTechnology          interface{}

	VpnProtocol             interface{}
	ProxyType               interface{}
	ProxyAutoconfigSettings interface{}
	ApIsHotspot             interface{}
	NwInterfaces            interface{}
	DefGw                   interface{}

	AvOutputInfo          interface{}
	SupportedBiometryType interface{}

	ScreenBounds   interface{}
	ScreenScale    interface{}
	NativeScale    interface{}
	MetalInfo      interface{}
	IsCarplayActive interface{}
	IsUnityHost     interface{}
	UnityHostReason interface{}

	ExperimentalDeviceLocation interface{}
}

func DefaultGeolocationConfig() *GeolocationConfigValues {
	return &GeolocationConfigValues{
		GeolocationEnabled:          false,
		GeolocationServiceTimeoutMs: 3000,
		MaxGeoLocationCacheAgeSec:   60,
		PrefetchEnabled:             false,
	}
}
