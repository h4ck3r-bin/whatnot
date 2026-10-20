package devices_test

import (
	"math/rand"
	"strings"
	"testing"

	"status403.com/rsavv/seonsdk-go/devices"
)

// TestPickRealisticIOSVersion_FavoursLatestPatch confirms the population-
// weighted picker concentrates ~60% of draws on the single latest patch of
// the highest-major branch the machine can run. Real iPhone population looks
// the same (Apple's auto-update sees to it); SEON's risk model in production
// flagged uniform-random pickers as outliers in our 2026-04-30 capture.
func TestPickRealisticIOSVersion_FavoursLatestPatch(t *testing.T) {
	rand.Seed(1)
	const machine = "iPhone15,2" // iPhone 14 Pro — supports iOS 26
	const trials = 5000
	counts := map[string]int{}
	for i := 0; i < trials; i++ {
		v := devices.PickRealisticIOSVersion(machine, "")
		if v == "" {
			t.Fatalf("empty pick on attempt %d", i)
		}
		counts[v]++
	}

	// Find the actual highest version in the catalog the device supports.
	versions := devices.CompatibleIOSVersions(machine, "")
	if len(versions) == 0 {
		t.Fatal("no compatible versions for iPhone15,2")
	}
	latest := versions[0]
	for _, v := range versions {
		if devices.CompareIOSVersions(v, latest) > 0 {
			latest = v
		}
	}
	frac := float64(counts[latest]) / float64(trials)
	if frac < 0.50 || frac > 0.70 {
		t.Errorf("latest-patch share %.2f outside [0.50, 0.70] window — got distribution %v", frac, counts)
	}
}

// TestPickRealisticIOSVersion_NoIntermediatePatchOverweight confirms that
// older intermediate patches (e.g. "18.6", "18.7.1") receive a small share —
// not the ~2% each that uniform-random would emit when the catalog has
// ~50 versions. These versions were the ones SEON flagged on 2026-04-30.
func TestPickRealisticIOSVersion_NoIntermediatePatchOverweight(t *testing.T) {
	rand.Seed(2)
	const machine = "iPhone15,2"
	const trials = 5000
	counts := map[string]int{}
	for i := 0; i < trials; i++ {
		counts[devices.PickRealisticIOSVersion(machine, "")]++
	}
	// Sum the share of every version under "18.7" — these are the ones
	// real users have been auto-updated past.
	stale := 0
	for v, c := range counts {
		if strings.HasPrefix(v, "18.6") || strings.HasPrefix(v, "18.5") ||
			strings.HasPrefix(v, "18.4") || strings.HasPrefix(v, "18.3") ||
			strings.HasPrefix(v, "17.") {
			stale += c
		}
	}
	share := float64(stale) / float64(trials)
	if share > 0.10 {
		t.Errorf("stale-version share %.3f exceeds 10%%; uniform-random would put ~30%% here. counts: %v", share, counts)
	}
}

// TestPickRealisticIOSVersion_RespectsMinBound confirms the caller-supplied
// minimum is honoured — the picker never drops below it.
func TestPickRealisticIOSVersion_RespectsMinBound(t *testing.T) {
	rand.Seed(3)
	const machine = "iPhone15,2"
	const minVer = "18.5"
	for i := 0; i < 1000; i++ {
		v := devices.PickRealisticIOSVersion(machine, minVer)
		if devices.CompareIOSVersions(v, minVer) < 0 {
			t.Fatalf("got %q below min %q", v, minVer)
		}
	}
}

// TestPickRealisticIOSVersion_NeverEmpty for any catalog machine.
func TestPickRealisticIOSVersion_NeverEmpty(t *testing.T) {
	rand.Seed(4)
	for machine := range devices.DeviceSpecs {
		v := devices.PickRealisticIOSVersion(machine, "")
		if v == "" {
			t.Errorf("empty pick for machine %s", machine)
		}
	}
}

// TestRandomMachineAndRealisticIOS_Compatible verifies the convenience helper
// always returns a (machine, version) pair where the version is compatible
// with that machine.
func TestRandomMachineAndRealisticIOS_Compatible(t *testing.T) {
	rand.Seed(5)
	for i := 0; i < 200; i++ {
		m, v := devices.RandomMachineAndRealisticIOS("")
		if m == "" || v == "" {
			t.Fatalf("empty pair on iter %d", i)
		}
		if !devices.IsCompatibleIOSVersion(m, v) {
			t.Errorf("incompatible pair: %s + %s", m, v)
		}
	}
}
