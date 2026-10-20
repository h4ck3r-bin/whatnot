package devices_test

import (
	"testing"
	"time"

	seonsdk "status403.com/rsavv/seonsdk-go"
	"status403.com/rsavv/seonsdk-go/devices"
	"status403.com/rsavv/seonsdk-go/devices/behaviour"
)

// TestBehaviourToggle verifies that a single *Device can produce fingerprints
// with AND without behaviour_results depending on FingerprintOverrides.
// This mirrors the Whatnot usage pattern where the same SEONFingerprint
// singleton is used for both login/phone-signup (with behaviour — driven by
// prior startBehaviourMonitoring) and address mutations (without).
func TestBehaviourToggle(t *testing.T) {
	opts := devices.DeviceOptions{
		Machine:    "iPhone16,2",
		IOSVersion: "18.2",
		SkipDC:     true,
	}

	t.Run("device without default, override enables", func(t *testing.T) {
		dev, err := devices.NewDevice(opts)
		if err != nil {
			t.Fatal(err)
		}

		// No overrides → no behaviour.
		a := dev.NewFingerprint()
		if a.BehaviourResults != nil {
			t.Error("no overrides should yield nil BehaviourResults")
		}

		// Override enables for this single call.
		b := dev.NewFingerprint(devices.FingerprintOverrides{
			Behaviour: &devices.BehaviourOptions{
				Enabled:  true,
				Duration: 2 * time.Second,
				Seed:     7,
				Activity: behaviour.ActivityIdle,
			},
		})
		if b.BehaviourResults == nil || b.BehaviourResults.RawResults == nil {
			t.Fatal("override should yield populated BehaviourResults")
		}
		if _, ok := b.BehaviourResults.RawResults["min_user_acc"]; !ok {
			t.Error("expected simulator keys in RawResults")
		}

		// Back to default next call — no behaviour.
		c := dev.NewFingerprint()
		if c.BehaviourResults != nil {
			t.Error("subsequent call without overrides should be back to no behaviour")
		}
	})

	t.Run("device with default, override disables", func(t *testing.T) {
		deviceOpts := opts
		deviceOpts.BehaviourMonitoring = &devices.BehaviourOptions{
			Enabled:  true,
			Duration: 2 * time.Second,
			Seed:     99,
			Activity: behaviour.ActivityIdle,
		}
		dev, err := devices.NewDevice(deviceOpts)
		if err != nil {
			t.Fatal(err)
		}

		// Default call → behaviour present.
		a := dev.NewFingerprint()
		if a.BehaviourResults == nil {
			t.Fatal("default should yield populated BehaviourResults")
		}

		// Force-disable for this single call via override with Enabled=false.
		b := dev.NewFingerprint(devices.FingerprintOverrides{
			Behaviour: &devices.BehaviourOptions{Enabled: false},
		})
		if b.BehaviourResults != nil {
			t.Error("override with Enabled=false should suppress BehaviourResults")
		}

		// Next call back to the device default.
		c := dev.NewFingerprint()
		if c.BehaviourResults == nil {
			t.Error("no override should re-inherit device default (behaviour on)")
		}
	})

	// Integration: both fingerprints should encrypt / decrypt cleanly and
	// the WITH-behaviour one should carry behaviour_results in the payload.
	t.Run("wire-level check", func(t *testing.T) {
		dev, err := devices.NewDevice(opts)
		if err != nil {
			t.Fatal(err)
		}

		cfgWith := dev.NewFingerprint(devices.FingerprintOverrides{
			Behaviour: &devices.BehaviourOptions{
				Enabled: true, Duration: 1 * time.Second, Seed: 1,
			},
		})
		cfgWithout := dev.NewFingerprint()

		wireWith, err := seonsdk.Fingerprint(*cfgWith)
		if err != nil {
			t.Fatal(err)
		}
		wireWithout, err := seonsdk.Fingerprint(*cfgWithout)
		if err != nil {
			t.Fatal(err)
		}

		_, payloadWith, err := seonsdk.Decrypt(wireWith)
		if err != nil {
			t.Fatal(err)
		}
		_, payloadWithout, err := seonsdk.Decrypt(wireWithout)
		if err != nil {
			t.Fatal(err)
		}

		// With-behaviour payload must contain stat keys.
		br, ok := payloadWith["behaviour_results"].(map[string]interface{})
		if !ok {
			t.Fatal("with-behaviour: behaviour_results missing or wrong type")
		}
		if _, ok := br["min_user_acc"]; !ok {
			t.Error("with-behaviour: min_user_acc missing")
		}

		// Without-behaviour payload: behaviour_results should be absent
		// or explicitly null (both paths match real-SDK "not running"
		// semantics; our builder leaves the key out entirely).
		if v, ok := payloadWithout["behaviour_results"]; ok && v != nil {
			if m, isMap := v.(map[string]interface{}); isMap && len(m) > 0 {
				t.Errorf("without-behaviour: expected absent or empty, got %v", v)
			}
		}
	})
}

// TestBehaviourOptions_HeadingActivityPlumbing confirms that
// BehaviourOptions.HeadingActivity is forwarded into
// behaviour.SimulateOptions and actually changes the emitted timing.
// Without this plumbing, callers' HeadingActivity field is silently
// ignored and the simulator falls back to HandheldStill.
func TestBehaviourOptions_HeadingActivityPlumbing(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine:    "iPhone16,2",
		IOSVersion: "18.7.6",
		SkipDC:     true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// HandheldStill (mean 1.1s) over 17s → ~10 entries (buffer cap)
	stillCount := 0
	// OnTable (mean 18s) over 17s → ~0-2 entries
	tableCount := 0
	const trials = 50
	for i := 0; i < trials; i++ {
		fpStill := dev.NewFingerprint(devices.FingerprintOverrides{
			Behaviour: &devices.BehaviourOptions{
				Enabled:         true,
				Duration:        17 * time.Second,
				Seed:            int64(i + 1),
				Activity:        behaviour.ActivityIdle,
				HeadingActivity: behaviour.HeadingActivityHandheldStill,
			},
		})
		fpTable := dev.NewFingerprint(devices.FingerprintOverrides{
			Behaviour: &devices.BehaviourOptions{
				Enabled:         true,
				Duration:        17 * time.Second,
				Seed:            int64(i + 1000),
				Activity:        behaviour.ActivityIdle,
				HeadingActivity: behaviour.HeadingActivityOnTable,
			},
		})
		stillCount += headingEntries(fpStill)
		tableCount += headingEntries(fpTable)
	}
	stillMean := float64(stillCount) / float64(trials)
	tableMean := float64(tableCount) / float64(trials)
	if stillMean < 8 || stillMean > 10 {
		t.Errorf("HandheldStill mean entries %.2f outside [8, 10] — plumbing may be broken", stillMean)
	}
	if tableMean > 3 {
		t.Errorf("OnTable mean entries %.2f > 3 — plumbing may be broken (HandheldStill leaking through?)", tableMean)
	}
}

// TestBehaviourOptions_EmitDurationPlumbing confirms EmitDuration flows
// through. Default false → emits 0 (matches 20/20 of the iPhone 15 / iOS 26.5
// corpus); opt-in true → emits Duration in milliseconds (legacy Whatnot
// pattern).
func TestBehaviourOptions_EmitDurationPlumbing(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone16,2", IOSVersion: "18.7.6", SkipDC: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defaultFp := dev.NewFingerprint(devices.FingerprintOverrides{
		Behaviour: &devices.BehaviourOptions{
			Enabled:  true,
			Duration: 5 * time.Second,
			Seed:     1,
		},
	})
	if got := defaultFp.BehaviourResults.RawResults["monitoring_duration"]; got != int64(0) {
		t.Errorf("default monitoring_duration: got %v, want 0 (corpus pattern)", got)
	}

	durFp := dev.NewFingerprint(devices.FingerprintOverrides{
		Behaviour: &devices.BehaviourOptions{
			Enabled:      true,
			Duration:     5 * time.Second,
			Seed:         1,
			EmitDuration: true,
		},
	})
	if got := durFp.BehaviourResults.RawResults["monitoring_duration"]; got != int64(5000) {
		t.Errorf("opt-in monitoring_duration: got %v, want 5000 (= Duration.Milliseconds())", got)
	}
}

func headingEntries(fp *seonsdk.FingerprintConfig) int {
	if fp.BehaviourResults == nil || fp.BehaviourResults.RawResults == nil {
		return 0
	}
	hc, _ := fp.BehaviourResults.RawResults["heading_changes"].(map[string]interface{})
	return len(hc)
}

// TestBehaviourProfileBindsToMachine — pinned per-chip profile wiring.
// Currently disabled: the per-chip σ values were unverified against real
// devices and raised ban rates, so simulateOptionsForMachine falls back
// to behaviour.DefaultProfile for every device. When Frida samples
// calibrate the per-chip σ, re-enable this test.
func TestBehaviourProfileBindsToMachine(t *testing.T) {
	t.Skip("per-chip profile disabled until Frida calibration — all machines use DefaultProfile")
	mkDev := func(machine, ios string) *devices.Device {
		dev, err := devices.NewDevice(devices.DeviceOptions{
			Machine:    machine,
			IOSVersion: ios,
			SkipDC:     true,
		})
		if err != nil {
			t.Fatalf("NewDevice %s/%s: %v", machine, ios, err)
		}
		return dev
	}

	ov := devices.FingerprintOverrides{
		Behaviour: &devices.BehaviourOptions{
			Enabled:  true,
			Duration: 5 * time.Second,
			Seed:     1337,
			Activity: behaviour.ActivityIdle,
		},
	}

	a13 := mkDev("iPhone12,8", "15.0").NewFingerprint(ov)       // SE 2nd — A13
	a18pro := mkDev("iPhone17,1", "18.0").NewFingerprint(ov)    // iPhone 16 Pro — A18 Pro

	if a13.BehaviourResults == nil || a18pro.BehaviourResults == nil {
		t.Fatal("expected behaviour_results on both devices")
	}

	a13Var := a13.BehaviourResults.RawResults["var_user_acc"].(float64)
	a18Var := a18pro.BehaviourResults.RawResults["var_user_acc"].(float64)

	if a13Var == a18Var {
		t.Fatalf("var_user_acc identical across A13 and A18 Pro — profile not flowing from Device.machine")
	}
	if !(a13Var > a18Var) {
		t.Errorf("A13 (noisier) var %g should exceed A18 Pro var %g", a13Var, a18Var)
	}
}
