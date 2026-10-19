package devices_test

import (
	"testing"
	"time"

	"status403.com/rsavv/seonsdk-go/devices"
	"status403.com/rsavv/seonsdk-go/devices/behaviour"
)

// TestDevicePersistenceRoundTrip — verify that MarshalJSON + UnmarshalDevice
// preserves every field that affects fingerprint output. The restored
// device's next NewFingerprint() should emit the SAME device_hash, SAME
// IDFV, SAME iCloud token, AND ReturningSession=true because
// GenerationCount is preserved.
func TestDevicePersistenceRoundTrip(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine:    "iPhone16,2",
		IOSVersion: "18.2",
		SkipDC:     true,
		LocalePreset: devices.LocaleUKEnglish,
		IcloudSignedIn: true,
		Location: &devices.LocationOptions{Preset: devices.CityLondon},
		Network:  devices.NetworkLTE,
		BehaviourMonitoring: &devices.BehaviourOptions{
			Enabled:  true,
			Duration: 3 * time.Second,
			Seed:     42,
			Activity: behaviour.ActivityTyping,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Advance generation count so we can verify it's preserved.
	_ = dev.NewFingerprint()
	_ = dev.NewFingerprint()

	// Snapshot identity fields.
	origUdid := dev.DeviceUdid()
	origAdid := dev.DeviceAdid()
	origIcloud := dev.IcloudUbiquityToken()
	origMachine := dev.Machine()
	origGen := dev.GenerationCount()

	data, err := dev.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if len(data) < 100 {
		t.Fatalf("marshalled size suspicious: %d bytes", len(data))
	}

	restored, err := devices.UnmarshalDevice(data)
	if err != nil {
		t.Fatalf("UnmarshalDevice: %v", err)
	}
	if restored.Machine() != origMachine {
		t.Errorf("machine: got %q, want %q", restored.Machine(), origMachine)
	}
	if restored.DeviceUdid() != origUdid {
		t.Errorf("device_udid not preserved: got %q, want %q", restored.DeviceUdid(), origUdid)
	}
	if restored.DeviceAdid() != origAdid {
		t.Errorf("device_adid not preserved: got %q, want %q", restored.DeviceAdid(), origAdid)
	}
	if restored.IcloudUbiquityToken() != origIcloud {
		t.Errorf("icloud_token not preserved: got %v, want %v", restored.IcloudUbiquityToken(), origIcloud)
	}
	if restored.GenerationCount() != origGen {
		t.Errorf("generation_count not preserved: got %d, want %d", restored.GenerationCount(), origGen)
	}

	// Restored device's next call should see ReturningSession=true because
	// GenerationCount is already > 0.
	cfg := restored.NewFingerprint()
	if !cfg.ReturningSession {
		t.Error("restored device first call should report ReturningSession=true")
	}
}

// TestDevicePersistenceMissingMachine — corrupt the Machine field and
// verify UnmarshalDevice rejects cleanly.
func TestDevicePersistenceMissingMachine(t *testing.T) {
	badJSON := []byte(`{"version":1,"machine":"iPhoneNope,1","ios_version":"18.2"}`)
	_, err := devices.UnmarshalDevice(badJSON)
	if err == nil {
		t.Fatal("expected error for unknown machine")
	}
}

// TestDevicePersistenceFutureVersion — forward-incompatible version field
// should fail loudly rather than produce garbage.
func TestDevicePersistenceFutureVersion(t *testing.T) {
	badJSON := []byte(`{"version":99,"machine":"iPhone16,2","ios_version":"18.2"}`)
	_, err := devices.UnmarshalDevice(badJSON)
	if err == nil {
		t.Fatal("expected error for future state version")
	}
}
