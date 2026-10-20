package seonsdk

import (
	"encoding/json"
	"testing"
)

// TestFingerprintJSONMatchesFingerprint guards a field-shift regression.
//
// KeysDevice has 11 entries and BuildProbeMap zips positionally.
// FingerprintJSON used to pass 13 values (DeviceName and IosDeviceName were
// wrongly included), shifting every device field by one: device_adid carried
// the device NAME, screen_brightness carried the iOS version string,
// icloud_ubiquity_token carried a number. Fingerprint() was always correct, so
// the two must agree field-for-field.
func TestFingerprintJSONMatchesFingerprint(t *testing.T) {
	cfg := fingerprintJSONTestConfig()

	raw, err := FingerprintJSON(cfg)
	if err != nil {
		t.Fatalf("FingerprintJSON: %v", err)
	}
	var viaJSON map[string]interface{}
	if err := json.Unmarshal(raw, &viaJSON); err != nil {
		t.Fatalf("FingerprintJSON produced invalid JSON: %v", err)
	}

	wire, err := Fingerprint(cfg)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	_, viaWire, err := Decrypt(wire)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	// Fields that legitimately differ per call.
	volatile := map[string]bool{
		"request_id": true, "session_id": true, "timestamp": true,
		"core_seed": true, "dynamic_evaluator_results": true,
		"performance_actual_processing": true, "performance_exec_time": true,
		"performance_network_call": true, "performance_probes": true,
		"behaviour_results": true, "experimental_fields": true,
	}

	for k, want := range viaWire {
		if volatile[k] {
			continue
		}
		got, ok := viaJSON[k]
		if !ok {
			t.Errorf("%s: present via Fingerprint, missing via FingerprintJSON", k)
			continue
		}
		wj, _ := json.Marshal(want)
		gj, _ := json.Marshal(got)
		if string(wj) != string(gj) {
			t.Errorf("%s: FingerprintJSON has %s, Fingerprint has %s", k, gj, wj)
		}
	}

	// Spot-check the specific fields the shift corrupted.
	if v, _ := viaJSON["device_adid"].(string); v != cfg.Device.DeviceAdid {
		t.Errorf("device_adid = %q, want %q (the shift put the device NAME here)", v, cfg.Device.DeviceAdid)
	}
	if _, ok := viaJSON["screen_brightness"].(float64); !ok {
		t.Errorf("screen_brightness is %T, want a number", viaJSON["screen_brightness"])
	}
	if _, ok := viaJSON["screen_height"].(float64); !ok {
		t.Errorf("screen_height is %T, want a number", viaJSON["screen_height"])
	}
	if _, ok := viaJSON["is_screen_captured"].(bool); !ok {
		t.Errorf("is_screen_captured is %T, want a bool", viaJSON["is_screen_captured"])
	}
}

// fingerprintJSONTestConfig is a minimal valid config — enough for both entry
// points to build a payload, with distinctive device values so a positional
// shift is obvious.
func fingerprintJSONTestConfig() FingerprintConfig {
	ts := int64(1713225600000)
	return FingerprintConfig{
		SessionID: "8E3B001F-5A2C-4D9A-B6E3-A1F2C3D4E5F6",
		SkipDC:    true,
		Locale: LocaleConfig{
			RegionTimezone: "+02:00", RegionLanguage: "en", RegionCountry: "US",
			TimezoneIdentifier: "America/New_York", CalendarIdentifier: "gregorian",
		},
		Device: DeviceConfig{
			AccessoriesCount:    0,
			DeviceName:          "iPhone 17 Pro Max",
			DeviceAdid:          "00000000-0000-0000-0000-000000000000",
			DeviceOrientation:   "Portrait",
			DeviceUdid:          "A1B2C3D4-E5F6-7890-ABCD-EF1234567890",
			IosDeviceName:       "iPhone18,2",
			IosVersion:          "26.5.2",
			PasteboardHash:      "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			ScreenBrightness:    44,
			ScreenHeight:        2868,
			ScreenWidth:         1320,
			IcloudUbiquityToken: "77d5e09f 60432647 e096c87f 1a38ff15 1a6c3ceb",
			IsScreenCaptured:    false,
		},
		Hardware: HardwareConfig{
			BatteryLevel: 70, BatteryCharging: false, CpuType: "ARM_64E", CpuCount: 6,
			IsEmulator: false, PhysicalMemory: uint64(12262113280), SystemUptime: int64(303620),
		},
		Media: MediaConfig{AudioMuteStatus: false, AudioVolumeCurrent: 30, CanSendMail: true, CanSendText: true},
		Network: NetworkConfig{
			VpnState: "NOT_CONNECTED", ProxyAddress: nil, ProxyState: "NOT_CONNECTED",
			WifiSsid: nil, WifiMacAddress: nil,
		},
		OS: OSConfig{
			KernelVersion: "23F84", KernelName: "Darwin", KernelArch: "ARM_64E",
			IsJailbroken: false, IsIosAppOnMac: false, IsDebuggerAttached: false,
		},
		Telephony: TelephonyConfig{NetworkConfig: "WIFI", IsOnCall: false},
		Accessibility: AccessibilityConfig{
			IsVideoAutoplayEnabled: true, PreferredContentSize: "L", MfiHearingAidsStatus: 0,
		},
		Auth:              AuthConfig{IsPasscodeEnabled: true, IsBiometricsEnabled: true},
		PortScanResult:    []int{},
		TimestampOverride: &ts,
	}
}
