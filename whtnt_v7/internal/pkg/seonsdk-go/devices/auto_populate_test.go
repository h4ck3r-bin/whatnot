package devices_test

import (
	"testing"

	"status403.com/rsavv/seonsdk-go/devices"
)

// TestAutoPopulateNetworkDefault confirms that a device with no explicit
// Network still gets cfg.Telephony.NetworkConfig = "WIFI".
func TestAutoPopulateNetworkDefault(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone16,2", IOSVersion: "18.2", SkipDC: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := dev.NewFingerprint()
	if cfg.Telephony.NetworkConfig != "WIFI" {
		t.Errorf("default network_config: got %v, want WIFI", cfg.Telephony.NetworkConfig)
	}
	// radio_technology is isCellServiceAvailable (IDA 0xca30) — always a
	// Bool, and independent of the primary data path. The default device has
	// a working SIM, so WiFi + cell service → true. This matches
	// testdata/freshwhatnot, where all three payloads pair
	// `network_config: "WIFI"` with `radio_technology: true`.
	if cfg.Experimental.RadioTechnology != true {
		t.Errorf("default radio_technology: got %v, want true (SIM has service while on WiFi)",
			cfg.Experimental.RadioTechnology)
	}
}

// TestAutoPopulateNoCellServiceOnWiFi covers the opposite reading: a SIM-less
// or airplane-mode device on WiFi reports no cell service.
func TestAutoPopulateNoCellServiceOnWiFi(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone16,2", IOSVersion: "18.2", SkipDC: true,
		NoCellService: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := dev.NewFingerprint()
	if cfg.Telephony.NetworkConfig != "WIFI" {
		t.Errorf("network_config: got %v, want WIFI", cfg.Telephony.NetworkConfig)
	}
	if cfg.Experimental.RadioTechnology != false {
		t.Errorf("radio_technology: got %v, want false (NoCellService)",
			cfg.Experimental.RadioTechnology)
	}
}

// TestNoCellServiceIgnoredOffWiFi pins that the flag cannot produce a
// physically impossible pairing: being on LTE necessarily means cell service
// is available, and NetworkNone necessarily means it is not.
func TestNoCellServiceIgnoredOffWiFi(t *testing.T) {
	tests := []struct {
		name    string
		network devices.NetworkState
		want    bool
	}{
		{"LTE keeps cell service", devices.NetworkLTE, true},
		{"5G NSA keeps cell service", devices.Network5GNSA, true},
		{"None has no cell service", devices.NetworkNone, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, noCell := range []bool{false, true} {
				dev, err := devices.NewDevice(devices.DeviceOptions{
					Machine: "iPhone16,2", IOSVersion: "18.2", SkipDC: true,
					Network: tc.network, NoCellService: noCell,
				})
				if err != nil {
					t.Fatal(err)
				}
				cfg := dev.NewFingerprint()
				if cfg.Experimental.RadioTechnology != tc.want {
					t.Errorf("NoCellService=%v: radio_technology = %v, want %v",
						noCell, cfg.Experimental.RadioTechnology, tc.want)
				}
			}
		})
	}
}

// TestAutoPopulateNetworkFromDevice verifies a device-level Network
// propagates to every fingerprint.
func TestAutoPopulateNetworkFromDevice(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone16,2", IOSVersion: "18.2", SkipDC: true,
		Network: devices.NetworkLTE,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		cfg := dev.NewFingerprint()
		if cfg.Telephony.NetworkConfig != "LTE" {
			t.Errorf("call %d: got %v, want LTE", i, cfg.Telephony.NetworkConfig)
		}
		if cfg.Experimental.RadioTechnology != true {
			t.Errorf("call %d: got %v, want true (cell service available)", i, cfg.Experimental.RadioTechnology)
		}
	}
}

// TestAutoPopulateNetworkOverride verifies a per-call Network override wins.
func TestAutoPopulateNetworkOverride(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone16,2", IOSVersion: "18.2", SkipDC: true,
		Network: devices.NetworkLTE,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Per-call override to 5G NSA.
	state5g := devices.Network5GNSA
	cfg := dev.NewFingerprint(devices.FingerprintOverrides{Network: &state5g})
	if cfg.Telephony.NetworkConfig != "5G (NRNSA)" {
		t.Errorf("override 5G: got %v, want 5G (NRNSA)", cfg.Telephony.NetworkConfig)
	}

	// Back to device default next call.
	cfg2 := dev.NewFingerprint()
	if cfg2.Telephony.NetworkConfig != "LTE" {
		t.Errorf("inherit: got %v, want LTE", cfg2.Telephony.NetworkConfig)
	}
}

// TestLocationNoOpWithoutEnableGeolocation — a Location set without
// EnableGeolocation must NOT emit device_location. This is the
// Whatnot-safe default that prevents accidentally enabling geo for an
// app that never calls setGeolocationConfig: in real life.
func TestLocationNoOpWithoutEnableGeolocation(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone16,2", IOSVersion: "18.2", SkipDC: true,
		Location: &devices.LocationOptions{Preset: devices.CityLondon},
		// EnableGeolocation: false (default) → Location is ignored
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := dev.NewFingerprint()
	if cfg.DeviceLocation != nil {
		t.Error("Location set without EnableGeolocation should NOT emit device_location")
	}
	if cfg.Experimental.ExperimentalDeviceLocation != nil {
		t.Error("experimental_device_location should be nil when EnableGeolocation is false")
	}
	if cfg.GeolocationConfig != nil && cfg.GeolocationConfig.GeolocationEnabled {
		t.Error("GeolocationEnabled should stay false by default")
	}
}

// TestAutoPopulateLocationFromDeviceWithGeoEnabled verifies that setting
// both Location AND EnableGeolocation auto-populates DeviceLocation on
// every call with fresh jitter per fingerprint.
func TestAutoPopulateLocationFromDeviceWithGeoEnabled(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone16,2", IOSVersion: "18.2", SkipDC: true,
		Location:          &devices.LocationOptions{Preset: devices.CityLondon},
		EnableGeolocation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := dev.NewFingerprint()
	if a.DeviceLocation == nil {
		t.Fatal("device location should be auto-populated with EnableGeolocation=true")
	}
	b := dev.NewFingerprint()
	if b.DeviceLocation == nil {
		t.Fatal("second call: device location missing")
	}
	// Jitter should re-roll.
	if a.DeviceLocation.Latitude == b.DeviceLocation.Latitude &&
		a.DeviceLocation.Longitude == b.DeviceLocation.Longitude {
		t.Error("jitter didn't re-roll across calls")
	}
}

// TestAutoPopulateLocationSuppress — SuppressLocation overrides an enabled
// device default for a single call.
func TestAutoPopulateLocationSuppress(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone16,2", IOSVersion: "18.2", SkipDC: true,
		Location:          &devices.LocationOptions{Preset: devices.CityLondon},
		EnableGeolocation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := dev.NewFingerprint(devices.FingerprintOverrides{SuppressLocation: true})
	if cfg.DeviceLocation != nil {
		t.Error("SuppressLocation should null cfg.DeviceLocation")
	}
}

// TestAutoPopulateExperimentalDeviceLocation — all 9 experimental fields
// are populated when geolocation is enabled.
func TestAutoPopulateExperimentalDeviceLocation(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone16,2", IOSVersion: "18.2", SkipDC: true,
		Location:          &devices.LocationOptions{Preset: devices.CityLondon},
		EnableGeolocation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := dev.NewFingerprint()
	if cfg.DeviceLocation == nil {
		t.Fatal("device_location not populated")
	}
	if cfg.Experimental.ExperimentalDeviceLocation == nil {
		t.Fatal("experimental_device_location not populated")
	}
	expLoc, ok := cfg.Experimental.ExperimentalDeviceLocation.(map[string]interface{})
	if !ok {
		t.Fatalf("experimental_device_location wrong type: %T", cfg.Experimental.ExperimentalDeviceLocation)
	}
	for _, k := range []string{
		"altitude", "floor_level", "vertical_accuracy", "speed",
		"speed_accuracy", "course", "course_accuracy",
		"is_produced_by_accessory", "ellipsoidal_altitude",
	} {
		if _, exists := expLoc[k]; !exists {
			t.Errorf("missing field %q", k)
		}
	}
	alt, _ := expLoc["altitude"].(float64)
	if alt < 17 || alt > 23 {
		t.Errorf("London altitude out of expected range: %v", alt)
	}
	if cfg.GeolocationConfig == nil || !cfg.GeolocationConfig.GeolocationEnabled {
		t.Error("GeolocationEnabled should flip true when EnableGeolocation=true + Location set")
	}
}

// TestAutoPopulateWifiStubs — wifi_ssid / wifi_mac_address default nil.
func TestAutoPopulateWifiStubs(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone16,2", IOSVersion: "18.2", SkipDC: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := dev.NewFingerprint()
	if cfg.Network.WifiSsid != nil {
		t.Errorf("wifi_ssid: got %v, want nil", cfg.Network.WifiSsid)
	}
	if cfg.Network.WifiMacAddress != nil {
		t.Errorf("wifi_mac_address: got %v, want nil", cfg.Network.WifiMacAddress)
	}
}

// TestAutoPopulateLocationOverrideWithEnable — per-call EnableGeolocation
// + Location override path.
func TestAutoPopulateLocationOverrideWithEnable(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone16,2", IOSVersion: "18.2", SkipDC: true,
		// no device default
	})
	if err != nil {
		t.Fatal(err)
	}
	enableGeo := true
	cfg := dev.NewFingerprint(devices.FingerprintOverrides{
		Location:          &devices.LocationOptions{Preset: devices.CityTokyo, JitterDegrees: 0.001},
		EnableGeolocation: &enableGeo,
	})
	if cfg.DeviceLocation == nil {
		t.Fatal("override should populate cfg.DeviceLocation when EnableGeolocation=&true")
	}
	lat := cfg.DeviceLocation.Latitude.(float64)
	if lat < 35.6 || lat > 35.8 {
		t.Errorf("Tokyo lat out of range: %v", lat)
	}
}

// TestAutoPopulateLocationOverrideDisableFlag — override EnableGeolocation
// to &false to force-disable on a device that has it on by default.
func TestAutoPopulateLocationOverrideDisableFlag(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone16,2", IOSVersion: "18.2", SkipDC: true,
		Location:          &devices.LocationOptions{Preset: devices.CityLondon},
		EnableGeolocation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	disableGeo := false
	cfg := dev.NewFingerprint(devices.FingerprintOverrides{
		EnableGeolocation: &disableGeo,
	})
	if cfg.DeviceLocation != nil {
		t.Error("EnableGeolocation=&false override should suppress device_location")
	}
}
