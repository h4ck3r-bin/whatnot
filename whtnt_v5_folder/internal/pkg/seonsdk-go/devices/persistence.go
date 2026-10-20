package devices

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	seonsdk "status403.com/rsavv/seonsdk-go"
)

// deviceState is the on-disk representation of a Device. Everything that
// influences fingerprint output and survives a real iOS keychain (plus the
// time-dependent drift anchors) is persisted. Fields explicitly NOT
// persisted:
//
//   - httpClient: runtime-only, not JSON-serialisable.
//   - mu: sync.Mutex zero value is valid when reconstructed.
//
// Marshal/Unmarshal round-trip produces a Device byte-for-byte identical
// to the original for all fingerprint-affecting purposes.
type deviceState struct {
	Version int `json:"version"`

	Machine             string      `json:"machine"`
	IOSVersion          string      `json:"ios_version"`
	DeviceName          string      `json:"device_name"`
	UserDeviceName      string      `json:"user_device_name"`
	DeviceUdid          string      `json:"device_udid"`
	DeviceAdid          string      `json:"device_adid"`
	IcloudUbiquityToken interface{} `json:"icloud_ubiquity_token,omitempty"`

	NwInterfaces map[string]interface{} `json:"nw_interfaces,omitempty"`
	DefGw        string                 `json:"def_gw"`

	Locale     *seonsdk.LocaleConfig `json:"locale,omitempty"`
	DateFormat string                `json:"date_format"`

	IsBoldTextEnabled           bool `json:"is_bold_text_enabled"`
	IsReduceMotionEnabled       bool `json:"is_reduce_motion_enabled"`
	IsVideoAutoplayEnabled      bool `json:"is_video_autoplay_enabled"`
	IsAssisstiveTouchRunning    bool `json:"is_assisstive_touch_running"`
	IsReduceTransparencyEnabled bool `json:"is_reduce_transparency_enabled"`
	IsDarkerSystemColorsEnabled bool `json:"is_darker_system_colors_enabled"`
	PreferredContentSize        string `json:"preferred_content_size"`
	MfiHearingAidsStatus        int    `json:"mfi_hearing_aids_status"`

	IsPasscodeEnabled   bool `json:"is_passcode_enabled"`
	IsBiometricsEnabled bool `json:"is_biometrics_enabled"`

	CanSendMail bool `json:"can_send_mail"`
	CanSendText bool `json:"can_send_text"`

	IsFlutter     bool `json:"is_flutter"`
	IsReactNative bool `json:"is_react_native"`

	SupportedBiometryType string `json:"supported_biometry_type"`

	Proxy             string `json:"proxy,omitempty"`
	SkipDC            bool   `json:"skip_dc"`
	SimulateDCTimeout bool   `json:"simulate_dc_timeout"`
	DCAcceptLanguage  string `json:"dc_accept_language,omitempty"`

	Behaviour *BehaviourOptions `json:"behaviour,omitempty"`
	Location  *LocationOptions  `json:"location,omitempty"`
	Network   NetworkState      `json:"network"`
	// NoCellService must round-trip: a restored device that flipped
	// radio_technology would emit a different experimental_fields shape
	// (and lose its cellular nw_interfaces) after a bot restart.
	NoCellService bool `json:"no_cell_service,omitempty"`

	CreatedAtUnixNano int64 `json:"created_at_unix_nano"`
	InitialUptimeSec  int64 `json:"initial_uptime_sec"`
	InitialBattery    int   `json:"initial_battery"`
	PluggedIn         bool  `json:"plugged_in"`

	// Frozen user-state fields. These must round-trip or a restored device
	// would report a different brightness/volume/appearance than the same
	// device did before the bot restarted.
	ScreenBrightness float64 `json:"screen_brightness"`
	AudioVolume      int     `json:"audio_volume"`
	UIStyle          string  `json:"ui_style,omitempty"`
	PasteboardHash   string  `json:"pasteboard_hash,omitempty"`

	// CellularInterfaces must round-trip too: a restored device whose
	// carrier IP changed would look like it re-attached to the network
	// between two calls seconds apart.
	CellularInterfaces map[string]interface{} `json:"cellular_interfaces,omitempty"`

	GenerationCount int `json:"generation_count"`
}

// currentStateVersion is bumped whenever the persisted layout changes in a
// non-backwards-compatible way. Unmarshal errors when it reads a higher
// version than it understands.
const currentStateVersion = 1

// MarshalJSON serialises the Device into a stable JSON representation. Use
// this to persist a device across bot process restarts so the next
// fingerprint continues the same identity (IDFV, iCloud token, battery
// drift anchor, ReturningSession=true, etc.) instead of looking like a
// fresh first launch. Call `UnmarshalDevice()` on the returned bytes to
// reconstruct.
//
// The marshal lock-holds the same mutex that guards NewFingerprint; safe
// to call concurrently with generation.
func (d *Device) MarshalJSON() ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	s := deviceState{
		Version:                     currentStateVersion,
		Machine:                     d.machine,
		IOSVersion:                  d.defaults.IosVersion,
		DeviceName:                  d.deviceName,
		UserDeviceName:              d.userDeviceName,
		DeviceUdid:                  d.deviceUdid,
		DeviceAdid:                  d.deviceAdid,
		IcloudUbiquityToken:         d.icloudUbiquityToken,
		NwInterfaces:                d.nwInterfaces,
		DefGw:                       d.defGw,
		Locale:                      d.locale,
		DateFormat:                  d.dateFormat,
		IsBoldTextEnabled:           d.isBoldTextEnabled,
		IsReduceMotionEnabled:       d.isReduceMotionEnabled,
		IsVideoAutoplayEnabled:      d.isVideoAutoplayEnabled,
		IsAssisstiveTouchRunning:    d.isAssisstiveTouchRunning,
		IsReduceTransparencyEnabled: d.isReduceTransparencyEnabled,
		IsDarkerSystemColorsEnabled: d.isDarkerSystemColorsEnabled,
		PreferredContentSize:        d.preferredContentSize,
		MfiHearingAidsStatus:        d.mfiHearingAidsStatus,
		IsPasscodeEnabled:           d.isPasscodeEnabled,
		IsBiometricsEnabled:         d.isBiometricsEnabled,
		CanSendMail:                 d.canSendMail,
		CanSendText:                 d.canSendText,
		IsFlutter:                   d.isFlutter,
		IsReactNative:               d.isReactNative,
		SupportedBiometryType:       d.supportedBiometryType,
		Proxy:                       d.proxy,
		SkipDC:                      d.skipDC,
		SimulateDCTimeout:           d.simulateDCTimeout,
		DCAcceptLanguage:            d.dcAcceptLanguage,
		Behaviour:                   d.behaviour,
		Location:                    d.location,
		Network:                     d.network,
		NoCellService:               d.noCellService,
		CreatedAtUnixNano:           d.createdAt.UnixNano(),
		InitialUptimeSec:            d.initialUptimeSec,
		InitialBattery:              d.initialBattery,
		ScreenBrightness:            d.screenBrightness,
		AudioVolume:                 d.audioVolume,
		UIStyle:                     d.uiStyle,
		PasteboardHash:              d.pasteboardHash,
		CellularInterfaces:          d.cellularInterfaces,
		PluggedIn:                   d.pluggedIn,
		GenerationCount:             d.generationCount,
	}
	return json.Marshal(&s)
}

// UnmarshalDevice rebuilds a Device from bytes previously produced by
// Device.MarshalJSON(). The restored device has:
//
//   - every keychain-cached identifier (IDFV, ADID, iCloud token, derived
//     device_hash) restored verbatim — so subsequent NewFingerprint() calls
//     report the SAME device_hash as the original persisted device;
//   - battery/uptime drift anchors preserved — elapsed time is measured
//     from the ORIGINAL creation timestamp, so on restore the battery
//     continues drifting from where it was, not from the initial value;
//   - GenerationCount preserved — which means the very first post-restore
//     call correctly reports ReturningSession semantics (hashProvider 1/3
//     instead of 0/2) rather than looking like a fresh launch.
//
// The HTTPClient is intentionally NOT persisted (not JSON-serialisable).
// The restored Device's httpClient is nil, which makes dc.SendDCRequest
// build a fresh per-call tls_client.HttpClient — matching the real SDK's
// per-fingerprint NSURLSession behaviour at IDA 0x7910/0x7940. If you
// need a shared client (custom auth, cookie jar), attach it after via
// (*Device).SetHTTPClient().
func UnmarshalDevice(data []byte) (*Device, error) {
	var s deviceState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshal device state: %w", err)
	}
	if s.Version > currentStateVersion {
		return nil, fmt.Errorf("unmarshal device state: persisted version %d newer than supported %d", s.Version, currentStateVersion)
	}
	if LookupSpec(s.Machine) == nil {
		return nil, fmt.Errorf("unmarshal device state: unknown machine %q", s.Machine)
	}
	defaults := BuildDefaults(s.Machine, s.IOSVersion)
	if defaults == nil {
		return nil, fmt.Errorf("unmarshal device state: failed to rebuild defaults for %s / %s",
			s.Machine, s.IOSVersion)
	}
	d := &Device{
		machine:                     s.Machine,
		defaults:                    defaults,
		deviceName:                  s.DeviceName,
		userDeviceName:              s.UserDeviceName,
		deviceUdid:                  s.DeviceUdid,
		deviceAdid:                  s.DeviceAdid,
		icloudUbiquityToken:         s.IcloudUbiquityToken,
		nwInterfaces:                s.NwInterfaces,
		defGw:                       s.DefGw,
		locale:                      s.Locale,
		dateFormat:                  s.DateFormat,
		isBoldTextEnabled:           s.IsBoldTextEnabled,
		isReduceMotionEnabled:       s.IsReduceMotionEnabled,
		isVideoAutoplayEnabled:      s.IsVideoAutoplayEnabled,
		isAssisstiveTouchRunning:    s.IsAssisstiveTouchRunning,
		isReduceTransparencyEnabled: s.IsReduceTransparencyEnabled,
		isDarkerSystemColorsEnabled: s.IsDarkerSystemColorsEnabled,
		preferredContentSize:        s.PreferredContentSize,
		mfiHearingAidsStatus:        s.MfiHearingAidsStatus,
		isPasscodeEnabled:           s.IsPasscodeEnabled,
		isBiometricsEnabled:         s.IsBiometricsEnabled,
		canSendMail:                 s.CanSendMail,
		canSendText:                 s.CanSendText,
		isFlutter:                   s.IsFlutter,
		isReactNative:               s.IsReactNative,
		supportedBiometryType:       s.SupportedBiometryType,
		httpClient:                  nil, // restored devices start without a cached session; see SetHTTPClient.
		proxy:                       s.Proxy,
		skipDC:                      s.SkipDC,
		simulateDCTimeout:           s.SimulateDCTimeout,
		dcAcceptLanguage:            s.DCAcceptLanguage,
		behaviour:                   s.Behaviour,
		location:                    s.Location,
		network:                     s.Network,
		noCellService:               s.NoCellService,
		createdAt:                   time.Unix(0, s.CreatedAtUnixNano),
		initialUptimeSec:            s.InitialUptimeSec,
		initialBattery:              s.InitialBattery,
		pluggedIn:                   s.PluggedIn,
		screenBrightness:            s.ScreenBrightness,
		audioVolume:                 s.AudioVolume,
		uiStyle:                     s.UIStyle,
		pasteboardHash:              s.PasteboardHash,
		cellularInterfaces:          s.CellularInterfaces,
		generationCount:             s.GenerationCount,
		mu:                          sync.Mutex{},
	}

	// Snapshots written before these fields were persisted carry zero
	// values. Regenerate rather than emit brightness 0 / volume 0 / an empty
	// style / an empty pasteboard hash, none of which a real handset
	// reports. The regenerated value is then stable for the restored
	// device's lifetime, which is the property that matters.
	if d.screenBrightness <= 0 {
		d.screenBrightness = RandomBrightness()
	}
	if d.audioVolume <= 0 {
		d.audioVolume = RandomAudioVolume()
	}
	if d.uiStyle == "" {
		d.uiStyle = RandomUserInterfaceStyle()
	}
	if d.pasteboardHash == "" {
		d.pasteboardHash = FormatPasteboardHash("", rand.Intn(500)+1)
	}
	if len(d.cellularInterfaces) == 0 {
		d.cellularInterfaces = NewCellularInterfaces()
	}

	return d, nil
}
