package devices_test

import (
	"testing"

	seonsdk "status403.com/rsavv/seonsdk-go"
	"status403.com/rsavv/seonsdk-go/devices"
)

// TestTelephonyForNetworkPairs verifies the network_config / radio_technology
// pairs match the real SEON telephony probe. Per IDA 0xca30,
// `radio_technology` is ALWAYS `numberWithBool:isCellServiceAvailable` —
// a boolean — never a CTRadioAccessTechnology string. Verified against a
// real iPhone 14 Pro capture: `radio_technology: false` on WiFi.
func TestTelephonyForNetworkPairs(t *testing.T) {
	cases := []struct {
		state           devices.NetworkState
		wantNetworkConf string
		wantRadioTech   bool
	}{
		{devices.NetworkWiFi, "WIFI", false},
		{devices.NetworkLTE, "LTE", true},
		{devices.Network5GNSA, "5G (NRNSA)", true},
		{devices.Network5GSA, "5G (NR)", true},
		{devices.NetworkHSPA, "HSDPA", true},
		{devices.Network3G, "WCDMA", true},
		{devices.NetworkEDGE, "EDGE", true},
		{devices.NetworkNone, "None", false},
	}
	for _, c := range cases {
		got := devices.TelephonyForNetwork(c.state)
		if got.NetworkConfig != c.wantNetworkConf {
			t.Errorf("%v: network_config = %q, want %q", c.state, got.NetworkConfig, c.wantNetworkConf)
		}
		if got.RadioTechnology != c.wantRadioTech {
			t.Errorf("%v: radio_technology = %v, want %v", c.state, got.RadioTechnology, c.wantRadioTech)
		}
	}
}

// TestTelephonyApply verifies the Apply method writes to the right
// FingerprintConfig fields and doesn't touch unrelated ones.
func TestTelephonyApply(t *testing.T) {
	cfg := &seonsdk.FingerprintConfig{}
	cfg.Device.DeviceName = "shouldNotChange"

	tel := devices.TelephonyForNetwork(devices.NetworkLTE)
	tel.Apply(cfg)

	if cfg.Telephony.NetworkConfig != "LTE" {
		t.Errorf("NetworkConfig: got %v, want LTE", cfg.Telephony.NetworkConfig)
	}
	if cfg.Experimental.RadioTechnology != true {
		t.Errorf("RadioTechnology: got %v, want true", cfg.Experimental.RadioTechnology)
	}
	if cfg.Device.DeviceName != "shouldNotChange" {
		t.Error("Apply mutated unrelated field")
	}
}

// TestRandomCellularNetwork verifies the distribution never returns WiFi
// or NetworkNone.
func TestRandomCellularNetwork(t *testing.T) {
	for i := 0; i < 500; i++ {
		got := devices.RandomCellularNetwork()
		if got == devices.NetworkWiFi || got == devices.NetworkNone {
			t.Fatalf("RandomCellular returned non-cellular state: %v", got)
		}
	}
}
