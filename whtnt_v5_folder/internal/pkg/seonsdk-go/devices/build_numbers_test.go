package devices

import (
	"regexp"
	"testing"
)

// buildNumberPattern matches an Apple iOS build number: major darwin number,
// a release-train letter, then a build counter — e.g. "23F84", "22H333",
// "23D8133".
var buildNumberPattern = regexp.MustCompile(`^[0-9]{2}[A-Z][0-9]+$`)

// TestCorpusDeviceBuildsDefaults is the regression guard for the gap that
// made the freshwhatnot capture ungeneratable: iPhone18,2 on iOS 26.5.2
// returned nil from BuildDefaults because IOSBuildNumbers stopped at 26.4.1.
func TestCorpusDeviceBuildsDefaults(t *testing.T) {
	const (
		machine     = "iPhone18,2"
		iosVersion  = "26.5.2"
		wantKernel  = "23F84"
		wantMemsize = uint64(12262113280)
	)

	defaults := BuildDefaults(machine, iosVersion)
	if defaults == nil {
		t.Fatalf("BuildDefaults(%q, %q) = nil, want non-nil "+
			"(is %s missing from IOSBuildNumbers?)", machine, iosVersion, iosVersion)
	}
	if defaults.KernelVersion != wantKernel {
		t.Errorf("kernel_version = %q, want %q (testdata/freshwhatnot)",
			defaults.KernelVersion, wantKernel)
	}
	if defaults.PhysicalMemory != wantMemsize {
		t.Errorf("physical_memory = %d, want %d (iPhone 17 Pro Max, not the Pro's value)",
			defaults.PhysicalMemory, wantMemsize)
	}
}

// TestProModelsHaveDistinctMemsize pins the split that keeps iPhone18,1 and
// iPhone18,2 from sharing one constant. Both values are real; collapsing them
// emits the wrong hw.memsize for one of the two models.
func TestProModelsHaveDistinctMemsize(t *testing.T) {
	pro := LookupSpec("iPhone18,1")
	proMax := LookupSpec("iPhone18,2")
	if pro == nil || proMax == nil {
		t.Fatal("iPhone18,1 / iPhone18,2 missing from DeviceSpecs")
	}
	if pro.PhysicalMemory == proMax.PhysicalMemory {
		t.Errorf("iPhone18,1 and iPhone18,2 share physical_memory %d; "+
			"they report different hw.memsize on real hardware", pro.PhysicalMemory)
	}
	// Both are exact multiples of the 16 KiB arm64 page size — the property
	// that distinguishes a captured hw.memsize from an invented number.
	const pageSize = 16384
	for _, spec := range []*DeviceSpec{pro, proMax} {
		if spec.PhysicalMemory%pageSize != 0 {
			t.Errorf("%s: physical_memory %d is not a multiple of the %d-byte page size",
				spec.Machine, spec.PhysicalMemory, pageSize)
		}
	}
}

// TestEveryCatalogVersionIsBuildable walks the whole selection catalog.
//
// CompatibleIOSVersions iterates IOSBuildNumbers, so any version present
// there can be handed to BuildDefaults by PickRealisticIOSVersion. A version
// whose build number is missing or malformed becomes a latent hard failure
// that only shows up for whichever device happens to draw it.
func TestEveryCatalogVersionIsBuildable(t *testing.T) {
	if len(IOSBuildNumbers) == 0 {
		t.Fatal("IOSBuildNumbers is empty")
	}

	for version, builds := range IOSBuildNumbers {
		if len(builds) == 0 {
			t.Errorf("%s: no build numbers — BuildNumberFor returns \"\" and BuildDefaults fails", version)
			continue
		}
		for _, build := range builds {
			if !buildNumberPattern.MatchString(build) {
				t.Errorf("%s: build %q does not look like an Apple build number", version, build)
			}
		}
		if got := BuildNumberFor(version); got == "" {
			t.Errorf("%s: BuildNumberFor returned empty", version)
		}
	}
}

// TestCatalogVersionsBuildOnCompatibleMachines confirms the catalog and the
// device database agree: for every machine, every iOS version the machine is
// allowed to pick must actually produce defaults.
func TestCatalogVersionsBuildOnCompatibleMachines(t *testing.T) {
	for machine := range DeviceSpecs {
		for _, version := range CompatibleIOSVersions(machine, "") {
			if BuildDefaults(machine, version) == nil {
				t.Errorf("BuildDefaults(%q, %q) = nil, but CompatibleIOSVersions offers it",
					machine, version)
			}
		}
	}
}
