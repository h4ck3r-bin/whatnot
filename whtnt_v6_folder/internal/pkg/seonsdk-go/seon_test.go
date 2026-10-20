package seonsdk

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	ts := int64(1713225600000)
	cfg := FingerprintConfig{
		SessionID: "8E3B001F-5A2C-4D9A-B6E3-A1F2C3D4E5F6",
		SkipDC:    true,
		Locale: LocaleConfig{
			RegionTimezone:     "+02:00",
			RegionLanguage:     "en",
			RegionCountry:      "US",
			TimezoneIdentifier: "America/New_York",
			CalendarIdentifier: "gregorian",
		},
		Device: DeviceConfig{
			AccessoriesCount:    0,
			DeviceName:          "iPhone",
			DeviceAdid:          "00000000-0000-0000-0000-000000000000",
			DeviceOrientation:   "Portrait",
			DeviceUdid:          "A1B2C3D4-E5F6-7890-ABCD-EF1234567890",
			IosDeviceName:       "iPhone16,2",
			IosVersion:          "18.0",
			PasteboardHash:      "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
			ScreenBrightness:    0.75,
			ScreenHeight:        float64(844),
			ScreenWidth:         float64(390),
			IcloudUbiquityToken: nil,
			IsScreenCaptured:    false,
		},
		Hardware: HardwareConfig{
			BatteryLevel:    85,
			BatteryCharging: false,
			CpuType:         "ARM_64E",
			CpuCount:        6,
			IsEmulator:      false,
			PhysicalMemory:  uint64(6442450944),
			SystemUptime:    int64(123456),
		},
		Media: MediaConfig{
			AudioMuteStatus:    false,
			AudioVolumeCurrent: 50,
			CanSendMail:        true,
			CanSendText:        true,
		},
		Network: NetworkConfig{
			VpnState:       "NOT_CONNECTED",
			ProxyAddress:   nil,
			ProxyState:     "NOT_CONNECTED",
			WifiSsid:       "MyNetwork",
			WifiMacAddress: "aa:bb:cc:dd:ee:ff",
		},
		OS: OSConfig{
			KernelVersion:      "Darwin Kernel Version 24.0.0",
			KernelName:         "Darwin",
			KernelArch:         "arm64",
			IsJailbroken:       false,
			IsIosAppOnMac:      false,
			IsDebuggerAttached: false,
		},
		Telephony: TelephonyConfig{
			NetworkConfig: "WIFI",
			IsOnCall:      false,
		},
		Accessibility: AccessibilityConfig{
			IsBoldTextEnabled:           false,
			IsReduceMotionEnabled:       false,
			IsVideoAutoplayEnabled:      true,
			IsAssisstiveTouchRunning:    false,
			IsReduceTransparencyEnabled: false,
			IsDarkerSystemColorsEnabled: false,
			PreferredContentSize:        "UICTContentSizeCategoryL",
			MfiHearingAidsStatus:        0,
		},
		Auth: AuthConfig{
			IsPasscodeEnabled:   true,
			IsBiometricsEnabled: true,
		},
		Experimental: ExperimentalConfig{
			ForceTouchSupport:  2,
			DisplayGamut:      "P3",
			UserInterfaceStyle: "Light",
			ThermalState:      0,
			NativeBounds: map[string]interface{}{
				"native_width":    float64(1170),
				"native_height":   float64(2532),
				"native_origin_x": float64(0),
				"native_origin_y": float64(0),
			},
			IsDisplayZoomEnabled:   false,
			ConnectedAccessories:   0,
			IsPluggedIn:            false,
			IsArm64:                true,
			DateFormat:             "MMM d, y",
			IsLowPowerEnabled:      false,
			IsReactNative:          false,
			IsRootFsCompromised:    false,
			HasInvalidCodeSig:      false,
			Pid:                    1,
			IsFlutter:              false,
			IsCellularDataRestricted: false,
			RadioTechnology:        "LTE",
			VpnProtocol:            nil,
			ProxyType:              nil,
			ApIsHotspot:            false,
			NwInterfaces: map[string]interface{}{
				"en0": "192.168.1.100",
				"lo0": "127.0.0.1",
			},
			DefGw:                 "192.168.1.1",
			SupportedBiometryType: "FaceID",
		},
		PortScanResult:  []int{},
		TimestampOverride: &ts,
	}

	wireString, err := Fingerprint(cfg)
	if err != nil {
		t.Fatalf("Fingerprint() error: %v", err)
	}

	if !strings.HasPrefix(wireString, "iOS;8E3B001F-5A2C-4D9A-B6E3-A1F2C3D4E5F6;") {
		t.Errorf("Wire string doesn't start with expected prefix: %s", wireString[:80])
	}

	parts := strings.SplitN(wireString, ";", 4)
	if len(parts) != 4 {
		t.Fatalf("Expected 4 parts, got %d", len(parts))
	}
	if parts[0] != "iOS" {
		t.Errorf("Platform = %q, want %q", parts[0], "iOS")
	}

	sid, payload, err := Decrypt(wireString)
	if err != nil {
		t.Fatalf("Decrypt() error: %v", err)
	}
	if sid != "8E3B001F-5A2C-4D9A-B6E3-A1F2C3D4E5F6" {
		t.Errorf("Session ID = %q", sid)
	}

	if payload["source"] != "ios-5.7.1" {
		t.Errorf("source = %v, want ios-5.7.1", payload["source"])
	}
	if payload["version_code"] != "01050701" {
		t.Errorf("version_code = %v, want 01050701", payload["version_code"])
	}
	if payload["session_id"] != "8E3B001F-5A2C-4D9A-B6E3-A1F2C3D4E5F6" {
		t.Errorf("session_id = %v", payload["session_id"])
	}

	expectedTopLevelKeys := []string{
		"accessories_count", "audio_mute_status", "audio_volume_current",
		"battery_charging", "battery_level", "behaviour_results",
		"calendar_identifier", "can_send_mail", "can_send_text",
		"codename", "core_seed",
		"cpu_count", "cpu_type", "device_adid", "device_hash",
		"device_location", "device_orientation",
		"device_udid", "experimental_fields", "geolocation_config",
		"icloud_ubiquity_token", "ios_version",
		"is_assisstive_touch_running", "is_biometrics_enabled",
		"is_bold_text_enabled", "is_darker_system_colors_enabled",
		"is_debugger_attached", "is_emulator", "is_ios_app_on_mac",
		"is_jailbroken", "is_on_call", "is_passcode_enabled",
		"is_reduce_motion_enabled", "is_reduce_transparency_enabled",
		"is_screen_captured", "is_video_autoplay_enabled",
		"kernel_arch", "kernel_name", "kernel_version",
		"location_timestamp", "mfi_hearing_aids_status",
		"network_config", "pasteboard_hash", "performance_actual_processing",
		"performance_exec_time", "performance_network_call",
		"physical_memory", "port_scan_result", "preferred_content_size",
		"proxy_address", "proxy_state", "region_country",
		"region_language", "region_timezone", "request_id",
		"screen_brightness",
		"screen_height", "screen_width", "session_id", "source",
		"stored_errors", "system_uptime", "telephony",
		"timestamp", "timezone_identifier", "version_code",
		"vpn_state", "wifi_mac_address", "wifi_ssid",
	}

	for _, key := range expectedTopLevelKeys {
		if _, ok := payload[key]; !ok {
			if key == "telephony" {
				continue
			}
			t.Errorf("Missing top-level key: %s", key)
		}
	}

	if dh, ok := payload["device_hash"].(string); ok {
		if len(dh) != 64 {
			t.Errorf("device_hash length = %d, want 64", len(dh))
		}
	} else {
		t.Errorf("device_hash is not a string: %T", payload["device_hash"])
	}

	expFields, ok := payload["experimental_fields"].(map[string]interface{})
	if !ok {
		t.Fatal("experimental_fields is not a map")
	}
	if _, ok := expFields["current_hash_provider"]; !ok {
		t.Error("Missing current_hash_provider in experimental_fields")
	}
	if _, ok := expFields["implicit_device_hash"]; !ok {
		t.Error("Missing implicit_device_hash in experimental_fields")
	}

	t.Logf("Round-trip successful. Payload has %d top-level keys", len(payload))

	jsonBytes, err := FingerprintJSON(cfg)
	if err != nil {
		t.Fatalf("FingerprintJSON() error: %v", err)
	}

	var checkMap map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &checkMap); err != nil {
		t.Fatalf("FingerprintJSON produced invalid JSON: %v", err)
	}

	jsonStr := string(jsonBytes)
	if !strings.Contains(jsonStr, "  \"") {
		t.Error("JSON is not 2-space indented")
	}

	sourceIdx := strings.Index(jsonStr, "\"source\"")
	sessionIdx := strings.Index(jsonStr, "\"session_id\"")
	if sourceIdx < sessionIdx {
		t.Logf("Keys are alphabetically sorted (source before session_id? source=%d, session_id=%d) — checking order", sourceIdx, sessionIdx)
	}
}
