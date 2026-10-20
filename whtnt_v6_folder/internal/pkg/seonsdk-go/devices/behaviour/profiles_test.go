package behaviour

import (
	"testing"
	"time"
)

// TestProfileForChipKnown — every chip identifier seonsdk-go supports should
// map to a non-default profile (so per-device calibration actually fires).
func TestProfileForChipKnown(t *testing.T) {
	knownChips := []string{
		"T8010", "T8015", "T8020", "T8030", "T8101",
		"T8110", "T8120", "T8122", "T8130", "T8140", "T8150",
	}
	for _, chip := range knownChips {
		p := ProfileForChip(chip)
		if p == DefaultProfile && chip != "T8110" {
			t.Errorf("chip %q returned DefaultProfile — profile map missing entry", chip)
		}
		// sanity — none of the σ values should be zero or wildly out of range
		if p.UserAccSigma <= 0 || p.UserAccSigma > 0.05 {
			t.Errorf("chip %q: UserAccSigma %f out of band", chip, p.UserAccSigma)
		}
		if p.RotRateSigma <= 0 || p.RotRateSigma > 0.10 {
			t.Errorf("chip %q: RotRateSigma %f out of band", chip, p.RotRateSigma)
		}
	}
}

// TestProfileForChipUnknown — unknown chip falls back to DefaultProfile.
func TestProfileForChipUnknown(t *testing.T) {
	p := ProfileForChip("T9999")
	if p != DefaultProfile {
		t.Errorf("unknown chip: got %+v, want DefaultProfile", p)
	}
}

// TestProfileMonotonicNewerQuieter — newer chip families should have tighter
// σ than older ones. Spot-check A10 vs A15 vs A18 Pro.
func TestProfileMonotonicNewerQuieter(t *testing.T) {
	a10 := ProfileForChip("T8010")
	a15 := ProfileForChip("T8110")
	a18pro := ProfileForChip("T8140")

	if !(a10.UserAccSigma > a15.UserAccSigma && a15.UserAccSigma > a18pro.UserAccSigma) {
		t.Errorf("UserAccSigma not monotonically tighter: A10=%f A15=%f A18Pro=%f",
			a10.UserAccSigma, a15.UserAccSigma, a18pro.UserAccSigma)
	}
	if !(a10.RotRateSigma > a15.RotRateSigma && a15.RotRateSigma > a18pro.RotRateSigma) {
		t.Errorf("RotRateSigma not monotonically tighter: A10=%f A15=%f A18Pro=%f",
			a10.RotRateSigma, a15.RotRateSigma, a18pro.RotRateSigma)
	}
}

// TestSimulateProfileAffectsVariance — two windows with identical seed/activity
// but different profiles should produce different var_user_acc values, with
// the noisier profile producing higher variance.
func TestSimulateProfileAffectsVariance(t *testing.T) {
	mkOpts := func(p DeviceProfile) SimulateOptions {
		return SimulateOptions{
			Duration:  5 * time.Second,
			Seed:      42,
			Activity:  ActivityIdle,
			StartUnix: 1700000000,
			Profile:   p,
		}
	}

	a10 := Simulate(mkOpts(ProfileForChip("T8010")))
	a17 := Simulate(mkOpts(ProfileForChip("T8122")))

	aVar := a10["var_user_acc"].(float64)
	xVar := a17["var_user_acc"].(float64)

	if aVar == xVar {
		t.Fatalf("var_user_acc identical across profiles — profile not threading into generator")
	}
	if !(aVar > xVar) {
		t.Errorf("expected noisier A10 var > quieter A17 Pro var; got A10=%g A17Pro=%g", aVar, xVar)
	}
}

// TestSimulateZeroProfileFallsBackToDefault — a zero-value Profile in opts
// must not produce NaN / zeroed-out samples; the simulator must fall back
// to DefaultProfile.
func TestSimulateZeroProfileFallsBackToDefault(t *testing.T) {
	r := Simulate(SimulateOptions{
		Duration:  3 * time.Second,
		Seed:      99,
		Activity:  ActivityIdle,
		StartUnix: 1700000000,
		// Profile left as zero-value DeviceProfile{}
	})
	v := r["var_user_acc"].(float64)
	if v <= 0 || v > 1 {
		t.Errorf("var_user_acc with zero profile: got %f, expected positive and < 1", v)
	}
}
