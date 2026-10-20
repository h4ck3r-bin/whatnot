package payload

import (
	"encoding/json"
	"fmt"
	"time"

	"status403.com/rsavv/seonsdk-go/crypto"
	"status403.com/rsavv/seonsdk-go/version"
)

type BuildInput struct {
	SessionID                   string
	DeviceHash                  string
	ImplicitDeviceHash          string
	HashProvider                int
	DCDynamicEvaluatorResults   interface{}
	Codename                    string
	CoreSeed                    string
	RequestID                   string

	Locale        interface{}
	Device        interface{}
	Hardware      interface{}
	Media         interface{}
	Network       interface{}
	OS            interface{}
	Telephony     interface{}
	Accessibility interface{}
	Auth          interface{}

	GeolocationConfig interface{}
	DeviceLocation    interface{}
	BehaviourResults  interface{}
	Experimental      map[string]interface{}

	StoredErrors     interface{}
	PortScanResult   interface{}
	// Source overrides the payload's top-level `source` string. If empty,
	// falls back to the package default (version.Source — "ios-5.7.1").
	Source           string
	// VersionCode overrides `version_code`. If empty, falls back to
	// version.VersionCode ("01050701"). Keep Source and VersionCode in
	// sync (both derived from the same SDK bundle version) — emitting a
	// `source` of "ios-6.0.0" with `version_code` "01050701" is a tell.
	VersionCode      string

	PerformanceActualProcessing string
	PerformanceExecTime         string
	PerformanceNetworkCall      string
	TimestampOverride           *int64
}

func BuildPayload(in *BuildInput) (map[string]interface{}, error) {
	m := make(map[string]interface{})

	m["geolocation_config"] = in.GeolocationConfig

	exp := in.Experimental
	if exp == nil {
		exp = make(map[string]interface{})
	}
	exp["current_hash_provider"] = in.HashProvider
	// implicit_device_hash is only present when the real SDK's
	// computeHashFor... ran this session (i.e., device_hash wasn't served
	// from the keychain cache). Verified against a real iPhone 14 Pro
	// fingerprint: current_hash_provider=1 (iCloud cached) → NO
	// implicit_device_hash field at all.
	//
	// Providers 0, 2, 4 = freshly computed (implicit_device_hash set).
	// Providers 1, 3    = keychain-cached   (implicit_device_hash absent).
	// See IDA 0x1bdb8 (cache hit branch) vs the `else` branch that calls
	// computeHashFor...:setImplicitDeviceHash.
	if in.ImplicitDeviceHash != "" && !isCachedHashProvider(in.HashProvider) {
		exp["implicit_device_hash"] = in.ImplicitDeviceHash
	}
	m["experimental_fields"] = exp

	if in.Source != "" {
		m["source"] = in.Source
	} else {
		m["source"] = version.Source
	}
	m["session_id"] = nilIfEmpty(in.SessionID)
	m["device_hash"] = in.DeviceHash
	m["codename"] = in.Codename
	m["core_seed"] = in.CoreSeed
	m["request_id"] = in.RequestID

	setKeysFromMap(m, in.Device, KeysDevice)
	setKeysFromMap(m, in.Hardware, KeysHardware)
	setKeysFromMap(m, in.Locale, KeysLocale)
	setKeysFromMap(m, in.Media, KeysMedia)
	setKeysFromMap(m, in.Network, KeysNetwork)
	setKeysFromMap(m, in.OS, KeysOS)
	setKeysFromMap(m, in.Telephony, KeysTelephony)
	setKeysFromMap(m, in.Accessibility, KeysAccessibility)
	setKeysFromMap(m, in.Auth, KeysAuth)

	// location_timestamp: matches constructReport at IDA 0x1bf9c..0x1c07c.
	//   1) scan non-empty   → location_timestamp = scan["timestamp"] or 0
	//   2) scan empty (geo disabled / empty) → location_timestamp = -1
	//      (NSConstantIntegerNumber at &off_43D90 — raw bytes verified)
	if in.DeviceLocation != nil {
		m["device_location"] = in.DeviceLocation
		if locMap, ok := in.DeviceLocation.(map[string]interface{}); ok {
			if ts, ok := locMap["timestamp"]; ok {
				m["location_timestamp"] = ts
			} else {
				m["location_timestamp"] = 0
			}
		} else {
			m["location_timestamp"] = 0
		}
	} else {
		m["device_location"] = nil
		// Geo scan empty → location_timestamp = -1. Matches IDA 0x1c07c
		// (NSConstantIntegerNumber at &off_43D90 — raw bytes verified).
		m["location_timestamp"] = int64(-1)
	}

	m["behaviour_results"] = in.BehaviourResults

	if cpuType, ok := m["cpu_type"]; ok && cpuType != nil {
		if _, ok2 := m["kernel_arch"]; ok2 {
			m["kernel_arch"] = cpuType
		}
	}

	if in.VersionCode != "" {
		m["version_code"] = in.VersionCode
	} else {
		m["version_code"] = version.VersionCode
	}

	// stored_errors matches the real SDK: NSMutableDictionary copy returned from
	// -[SEONErrorLogger getErrorLog] at 0xa0ac. Empty state is `{}`, populated state
	// has keys "<class>_<method>:<message>" (format at 0x9e98) with count values.
	if in.StoredErrors != nil {
		m["stored_errors"] = in.StoredErrors
	} else {
		m["stored_errors"] = map[string]interface{}{}
	}

	if in.PortScanResult != nil {
		m["port_scan_result"] = in.PortScanResult
	} else {
		m["port_scan_result"] = []interface{}{}
	}

	if in.DCDynamicEvaluatorResults != nil {
		m["dynamic_evaluator_results"] = in.DCDynamicEvaluatorResults
	}

	var ts int64
	if in.TimestampOverride != nil {
		ts = *in.TimestampOverride
	} else {
		ts = time.Now().UnixMilli()
	}
	m["timestamp"] = ts

	m["performance_actual_processing"] = in.PerformanceActualProcessing
	m["performance_exec_time"] = in.PerformanceExecTime
	m["performance_network_call"] = in.PerformanceNetworkCall

	// performance_probes — per-probe init/scan/experimental timings. Always
	// present on real iPhone fingerprints (IDA 0x1c8d0). Absent = instant tell.
	geoEnabled := false
	if in.GeolocationConfig != nil {
		if gc, ok := in.GeolocationConfig.(map[string]interface{}); ok {
			if v, ok := gc["geolocationEnabled"].(bool); ok {
				geoEnabled = v
			}
		}
	}
	m["performance_probes"] = BuildPerformanceProbes(nil, geoEnabled)

	return m, nil
}

// isCachedHashProvider reports whether a device-hash provider ID indicates
// the hash was served from the SEON keychain cache (1 = iCloud cached,
// 3 = IDFV cached). Fresh-compute providers are 0, 2, and 4.
func isCachedHashProvider(p int) bool {
	return p == 1 || p == 3
}

func setKeysFromMap(dst map[string]interface{}, src interface{}, keys []string) {
	srcMap, ok := src.(map[string]interface{})
	if !ok || srcMap == nil {
		for _, k := range keys {
			dst[k] = nil
		}
		return
	}
	for _, k := range keys {
		if v, exists := srcMap[k]; exists {
			dst[k] = v
		} else {
			dst[k] = nil
		}
	}
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func BuildProbeMap(keys []string, values []interface{}) map[string]interface{} {
	m := make(map[string]interface{}, len(keys))
	for i, k := range keys {
		if i < len(values) {
			m[k] = values[i]
		} else {
			m[k] = nil
		}
	}
	return m
}

// ComputeDeviceHash reproduces SEONFingerprint's computeHash cascade at IDA
// 0x1a8cc + 0x1a674.
//
// Priority cascade for device_hash (IDA 0x1ae78–0x1af74):
//
//	if icloud_ubiquity_token → SHA-256(token), provider=0 (or 1 if returning)
//	else if device_udid      → SHA-256(udid),  provider=2 (or 3 if returning)
//	else                     → implicit_device_hash, provider=4
//
// NOTE — key order for implicit_device_hash (2026-04-19):
//
// The real SDK serialises the 20-field stable dict via
// `NSJSONSerialization options=0` which walks NSMutableDictionary in
// hash-bucket order (CFBasicHash, 41 buckets for 20 entries). Our previous
// attempt at porting CFString + CFBasicHash to Go produced an iteration
// order that *should* match real iPhones but hasn't been verified against
// a Frida capture. Empirically that attempt raised ban rates vs simple
// alphabetical order — probably because CFBasicHash's probe strategy,
// tombstone handling, or CFString hash edge case differs from the
// open-source reference we ported.
//
// Until a real-device implicit_device_hash is available to pin the correct
// byte stream, we stay on Go's alphabetical `json.Marshal(map)` output.
// The CFString + bucket-order scaffolding remains in cf_hash.go /
// cf_dict_order.go so we can drop it in once Frida data lands.
func ComputeDeviceHash(icloudUbiquityToken interface{}, deviceUdid string, stableDict map[string]interface{}, returningSession bool) (deviceHash string, implicitHash string, hashProvider int) {
	stableJSON, err := json.Marshal(stableDict)
	if err != nil {
		stableJSON = []byte("{}")
	}
	implicitHash = crypto.SHA256Hex(string(stableJSON))

	tokenStr := interfaceToString(icloudUbiquityToken)
	if tokenStr != "" {
		provider := 0
		if returningSession {
			provider = 1
		}
		return crypto.SHA256Hex(tokenStr), implicitHash, provider
	}

	if deviceUdid != "" {
		provider := 2
		if returningSession {
			provider = 3
		}
		return crypto.SHA256Hex(deviceUdid), implicitHash, provider
	}

	return implicitHash, implicitHash, 4
}

func BuildStableDict(
	deviceName, iosDeviceName, cpuType string,
	cpuCount interface{},
	physicalMemoryFormatted string,
	regionLanguage, regionCountry, calendarIdentifier string,
	canSendMail, canSendText, isBiometricsEnabled interface{},
	userInterfaceStyle, dateFormat interface{},
	isDarkerSystemColorsEnabled, isBoldTextEnabled, isReduceMotionEnabled interface{},
	isVideoAutoplayEnabled, isReduceTransparencyEnabled interface{},
	preferredContentSize interface{},
	screenSize interface{},
) map[string]interface{} {
	return map[string]interface{}{
		"device_name":                     deviceName,
		"ios_device_name":                 iosDeviceName,
		"cpu_type":                        cpuType,
		"cpu_count":                       cpuCount,
		"user_interface_style":            userInterfaceStyle,
		"is_biometrics_enabled":           isBiometricsEnabled,
		"date_format":                     dateFormat,
		"physical_memory":                 physicalMemoryFormatted,
		"region_language":                 regionLanguage,
		"region_country":                  regionCountry,
		"calendar_identifier":             calendarIdentifier,
		"can_send_mail":                   canSendMail,
		"can_send_text":                   canSendText,
		"is_darker_system_colors_enabled": isDarkerSystemColorsEnabled,
		"is_bold_text_enabled":            isBoldTextEnabled,
		"is_reduce_motion_enabled":        isReduceMotionEnabled,
		"is_video_autoplay_enabled":       isVideoAutoplayEnabled,
		"is_reduce_transparency_enabled":  isReduceTransparencyEnabled,
		"preferred_content_size":          preferredContentSize,
		"screen_size":                     screenSize,
	}
}

func FormatPhysicalMemory(bytes uint64) string {
	gb := float64(bytes) / 1073741824.0
	return fmt.Sprintf("%.2f GB", gb)
}

func ComputeScreenSize(width, height float64) uint64 {
	return uint64(width * height)
}

func SHA256HexFromString(s string) string {
	return crypto.SHA256Hex(s)
}

func interfaceToString(v interface{}) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
