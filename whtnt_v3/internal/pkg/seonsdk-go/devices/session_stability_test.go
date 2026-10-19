package devices_test

import (
	"encoding/json"
	"testing"

	"status403.com/rsavv/seonsdk-go/devices"
)

func newStabilityDevice(t *testing.T) *devices.Device {
	t.Helper()
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone18,2", IOSVersion: "26.5.2", SkipDC: true,
	})
	if err != nil {
		t.Fatalf("NewDevice: %v", err)
	}
	return dev
}

// TestSessionStableFieldsDoNotMove pins the fields that testdata/freshwhatnot
// reports identically across all three fingerprints of one 116-second app
// session. Re-rolling any of them per fingerprint is a cross-call tell.
func TestSessionStableFieldsDoNotMove(t *testing.T) {
	dev := newStabilityDevice(t)

	first := dev.NewFingerprint()
	wantBrightness := first.Device.ScreenBrightness
	wantPasteboard := first.Device.PasteboardHash
	wantVolume := first.Media.AudioVolumeCurrent
	wantStyle := first.Experimental.UserInterfaceStyle

	for i := 1; i < 8; i++ {
		cfg := dev.NewFingerprint()
		if got := cfg.Device.ScreenBrightness; got != wantBrightness {
			t.Errorf("call %d: screen_brightness = %v, want %v (frozen per device)", i, got, wantBrightness)
		}
		if got := cfg.Media.AudioVolumeCurrent; got != wantVolume {
			t.Errorf("call %d: audio_volume_current = %v, want %v (frozen per device)", i, got, wantVolume)
		}
		if got := cfg.Device.PasteboardHash; got != wantPasteboard {
			t.Errorf("call %d: pasteboard_hash changed, want frozen", i)
		}
		if got := cfg.Experimental.UserInterfaceStyle; got != wantStyle {
			t.Errorf("call %d: user_interface_style = %v, want %v (a user does not toggle appearance mid-signup)",
				i, got, wantStyle)
		}
	}
}

// TestAudioVolumeAlwaysEven pins the 0.02 output-volume grid. Odd values are
// unreachable on a real handset; the corpus reports 30.
func TestAudioVolumeAlwaysEven(t *testing.T) {
	seen := map[int]bool{}
	for i := 0; i < 4000; i++ {
		v := devices.RandomAudioVolume()
		if v%2 != 0 {
			t.Fatalf("RandomAudioVolume returned odd value %d", v)
		}
		if v < 2 || v > 100 {
			t.Fatalf("RandomAudioVolume returned %d, want 2..100", v)
		}
		seen[v] = true
	}
	// The corpus value must be reachable, and the distribution must not have
	// collapsed onto a handful of values.
	if !seen[30] {
		t.Error("volume 30 (the freshwhatnot value) was never produced in 4000 draws")
	}
	if len(seen) < 20 {
		t.Errorf("only %d distinct volumes in 4000 draws; distribution looks collapsed", len(seen))
	}
}

// TestBatteryLevelIsMonotonic guards the jitter regression: a device that is
// not plugged in must never report a higher battery level than it did on an
// earlier call.
func TestBatteryLevelIsMonotonic(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone18,2", IOSVersion: "26.5.2", SkipDC: true,
		InitialBatteryLevel: 80,
	})
	if err != nil {
		t.Fatal(err)
	}

	prev := 101
	for i := 0; i < 25; i++ {
		cfg := dev.NewFingerprint()
		level, ok := cfg.Hardware.BatteryLevel.(int)
		if !ok {
			t.Fatalf("call %d: battery_level is %T, want int", i, cfg.Hardware.BatteryLevel)
		}
		if level > prev {
			t.Errorf("call %d: battery_level rose %d -> %d while not charging", i, prev, level)
		}
		charging, _ := cfg.Hardware.BatteryCharging.(bool)
		if charging {
			t.Errorf("call %d: battery_charging true on an unplugged device", i)
		}
		prev = level
	}
}

// TestFrozenFieldsSurviveRestart covers the persistence round-trip. Before
// these fields were serialised a restored device emitted brightness 0,
// volume 0, an empty appearance and an empty pasteboard hash.
func TestFrozenFieldsSurviveRestart(t *testing.T) {
	dev := newStabilityDevice(t)
	before := dev.NewFingerprint()

	blob, err := dev.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	restored, err := devices.UnmarshalDevice(blob)
	if err != nil {
		t.Fatalf("UnmarshalDevice: %v", err)
	}
	after := restored.NewFingerprint()

	if before.Device.ScreenBrightness != after.Device.ScreenBrightness {
		t.Errorf("screen_brightness: %v before restart, %v after",
			before.Device.ScreenBrightness, after.Device.ScreenBrightness)
	}
	if before.Media.AudioVolumeCurrent != after.Media.AudioVolumeCurrent {
		t.Errorf("audio_volume_current: %v before restart, %v after",
			before.Media.AudioVolumeCurrent, after.Media.AudioVolumeCurrent)
	}
	if before.Device.PasteboardHash != after.Device.PasteboardHash {
		t.Errorf("pasteboard_hash: %q before restart, %q after",
			before.Device.PasteboardHash, after.Device.PasteboardHash)
	}
	if before.Experimental.UserInterfaceStyle != after.Experimental.UserInterfaceStyle {
		t.Errorf("user_interface_style: %v before restart, %v after",
			before.Experimental.UserInterfaceStyle, after.Experimental.UserInterfaceStyle)
	}
}

// TestLegacySnapshotWithoutFrozenFields simulates a snapshot written before
// these fields were persisted: the restored device must regenerate plausible
// values instead of emitting zeroes.
func TestLegacySnapshotWithoutFrozenFields(t *testing.T) {
	dev := newStabilityDevice(t)
	blob, err := dev.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}

	stripped := removeJSONKeys(t, blob,
		"screen_brightness", "audio_volume", "ui_style", "pasteboard_hash")

	restored, err := devices.UnmarshalDevice(stripped)
	if err != nil {
		t.Fatalf("UnmarshalDevice on legacy snapshot: %v", err)
	}
	cfg := restored.NewFingerprint()

	if b, _ := cfg.Device.ScreenBrightness.(int); b <= 0 {
		t.Errorf("legacy restore: screen_brightness = %v, want > 0", cfg.Device.ScreenBrightness)
	}
	if v, _ := cfg.Media.AudioVolumeCurrent.(int); v <= 0 || v%2 != 0 {
		t.Errorf("legacy restore: audio_volume_current = %v, want a positive even value",
			cfg.Media.AudioVolumeCurrent)
	}
	if cfg.Device.PasteboardHash == "" {
		t.Error("legacy restore: pasteboard_hash is empty")
	}
	if s, _ := cfg.Experimental.UserInterfaceStyle.(string); s != "Light" && s != "Dark" {
		t.Errorf("legacy restore: user_interface_style = %v, want Light or Dark",
			cfg.Experimental.UserInterfaceStyle)
	}
}

// removeJSONKeys deletes top-level keys from a JSON object, used to fabricate
// a snapshot from before those fields existed.
func removeJSONKeys(t *testing.T, blob []byte, keys ...string) []byte {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(blob, &m); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	for _, k := range keys {
		delete(m, k)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("remarshal snapshot: %v", err)
	}
	return out
}
