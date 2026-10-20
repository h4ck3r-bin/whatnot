package devices_test

import (
	"strings"
	"testing"

	seonsdk "status403.com/rsavv/seonsdk-go"
	"status403.com/rsavv/seonsdk-go/devices"
)

func TestBuildPayload(t *testing.T) {
	cfg, err := devices.BuildPayload(devices.PayloadOptions{
		Machine:        "iPhone16,2",
		IOSVersion:     "18.0",
		UserDeviceName: "Test iPhone",
		SkipDC:         true,
	})
	if err != nil {
		t.Fatalf("BuildPayload error: %v", err)
	}

	if cfg.Device.DeviceName != "iPhone 15 Pro Max" {
		t.Errorf("DeviceName = %q, want %q", cfg.Device.DeviceName, "iPhone 15 Pro Max")
	}
	if cfg.Device.IosDeviceName != "Test iPhone" {
		t.Errorf("IosDeviceName = %q, want %q", cfg.Device.IosDeviceName, "Test iPhone")
	}
	if cfg.Device.IosVersion != "18.0" {
		t.Errorf("IosVersion = %q", cfg.Device.IosVersion)
	}
	if cfg.Hardware.CpuType != "ARM_64E" {
		t.Errorf("CpuType = %q", cfg.Hardware.CpuType)
	}
	// PhysicalMemory is now the sysctl hw.memsize value after firmware-
	// reserved pages (8 GB class ~= 7,888,867,328), not nominal 8×1024³.
	if cfg.Hardware.PhysicalMemory != uint64(7888867328) {
		t.Errorf("PhysicalMemory = %v, want 7888867328 (8 GB class memsize)", cfg.Hardware.PhysicalMemory)
	}
	if cfg.Device.ScreenWidth != 1290 {
		t.Errorf("ScreenWidth = %v, want 1290 (pixels)", cfg.Device.ScreenWidth)
	}
	if cfg.Device.ScreenHeight != 2796 {
		t.Errorf("ScreenHeight = %v, want 2796 (pixels)", cfg.Device.ScreenHeight)
	}
	// kernel_version is now the iOS build number (sysctl kern.osversion),
	// e.g., "22A3354" for iOS 18.0. Previously we emitted the full Darwin
	// kernel string; a real iPhone only exposes the build number through
	// this probe.
	if kv := cfg.OS.KernelVersion; len(kv) < 3 {
		t.Errorf("KernelVersion should be a non-empty iOS build number string, got %q", kv)
	}
	if cfg.Device.DeviceAdid != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("DeviceAdid = %q", cfg.Device.DeviceAdid)
	}
	if cfg.Device.DeviceUdid == "" {
		t.Error("DeviceUdid should be auto-generated")
	}

	wireString, err := seonsdk.Fingerprint(*cfg)
	if err != nil {
		t.Fatalf("Fingerprint error: %v", err)
	}
	if !strings.HasPrefix(wireString, "iOS;") {
		t.Error("Wire string doesn't start with iOS;")
	}

	sid, payload, err := seonsdk.Decrypt(wireString)
	if err != nil {
		t.Fatalf("Decrypt error: %v", err)
	}
	if sid == "" {
		t.Error("Empty session ID")
	}
	if payload["source"] != "ios-5.7.1" {
		t.Errorf("source = %v", payload["source"])
	}
	if payload["version_code"] != "01050701" {
		t.Errorf("version_code = %v", payload["version_code"])
	}

	t.Logf("Payload has %d top-level keys", len(payload))
	t.Logf("device_name = %v", payload["device_name"])
	t.Logf("device_hash = %v", payload["device_hash"])
}

func TestBuildPayloadUnknownDevice(t *testing.T) {
	_, err := devices.BuildPayload(devices.PayloadOptions{
		Machine:    "UnknownDevice99,9",
		IOSVersion: "18.0",
	})
	if err == nil {
		t.Error("Expected error for unknown device")
	}
}
