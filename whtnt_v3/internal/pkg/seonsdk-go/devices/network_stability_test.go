package devices_test

import (
	"testing"

	"status403.com/rsavv/seonsdk-go/devices"
)

// TestCellularInterfacesAreStable guards two bugs at once: the carrier IP
// being re-rolled per fingerprint, and pdp_ip2 blinking in and out because
// it was drawn at 40% on every call.
func TestCellularInterfacesAreStable(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone18,2", IOSVersion: "26.5.2", SkipDC: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	first, _ := dev.NewFingerprint().Experimental.NwInterfaces.(map[string]interface{})
	if first == nil {
		t.Fatal("nw_interfaces is not a map")
	}
	if _, ok := first["pdp_ip0"]; !ok {
		t.Fatal("pdp_ip0 missing: a SIM-equipped device on WiFi should expose a PDP context")
	}

	for i := 1; i < 8; i++ {
		got, _ := dev.NewFingerprint().Experimental.NwInterfaces.(map[string]interface{})
		if len(got) != len(first) {
			t.Fatalf("call %d: nw_interfaces has %d keys, want %d (interfaces must not appear/disappear)",
				i, len(got), len(first))
		}
		for k, v := range first {
			if got[k] != v {
				t.Errorf("call %d: nw_interfaces[%s] = %v, want %v (frozen per device)", i, k, got[k], v)
			}
		}
	}
}

// TestNwInterfacesNotSharedWithDevice pins that a fingerprint gets its own map.
// Previously the cellular merge mutated the Device's map in place, so the
// device's persisted network layout drifted with every call.
func TestNwInterfacesNotSharedWithDevice(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone18,2", IOSVersion: "26.5.2", SkipDC: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := dev.NewFingerprint()
	nw, _ := cfg.Experimental.NwInterfaces.(map[string]interface{})
	if nw == nil {
		t.Fatal("nw_interfaces is not a map")
	}
	nw["injected_by_caller"] = "1.2.3.4"

	next, _ := dev.NewFingerprint().Experimental.NwInterfaces.(map[string]interface{})
	if _, leaked := next["injected_by_caller"]; leaked {
		t.Error("mutating one fingerprint's nw_interfaces leaked into the device")
	}
}

// TestNoCellServiceDropsCellularInterfaces pins the pairing: a device
// reporting no cell service must not advertise carrier interfaces.
func TestNoCellServiceDropsCellularInterfaces(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone18,2", IOSVersion: "26.5.2", SkipDC: true,
		NoCellService: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := dev.NewFingerprint()
	nw, _ := cfg.Experimental.NwInterfaces.(map[string]interface{})
	for k := range nw {
		if k == "pdp_ip0" || k == "pdp_ip2" {
			t.Errorf("%s present although radio_technology is false", k)
		}
	}
}
