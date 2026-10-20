package devices

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
)

// CompareIOSVersions compares two semantic-ish iOS version strings (e.g. "17.7.3", "18.0").
// Returns -1 if a < b, 0 if a == b, +1 if a > b. Missing components are treated as 0.
func CompareIOSVersions(a, b string) int {
	as := parseVersion(a)
	bs := parseVersion(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		av := getOrZero(as, i)
		bv := getOrZero(bs, i)
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func parseVersion(v string) []int {
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, _ := strconv.Atoi(p)
		out[i] = n
	}
	return out
}

func getOrZero(s []int, i int) int {
	if i < len(s) {
		return s[i]
	}
	return 0
}

// IsCompatibleIOSVersion returns true if iosVersion is >= the machine's MinIOSVersion.
// Also returns true if the machine has no MinIOSVersion recorded (unknown/legacy).
//
// A real iPhone cannot run an iOS version older than the one it shipped with.
// Example: iPhone 16 Plus (iPhone17,4) shipped with iOS 18.0 — reporting iOS 17.x on
// this machine is a dead giveaway that the fingerprint is synthetic.
func IsCompatibleIOSVersion(machine, iosVersion string) bool {
	spec := LookupSpec(machine)
	if spec == nil || spec.MinIOSVersion == "" {
		return true
	}
	return CompareIOSVersions(iosVersion, spec.MinIOSVersion) >= 0
}

// ValidateIOSVersion returns an error if iosVersion is lower than the machine's
// minimum supported version. This is the signal a fingerprinting service would flag.
func ValidateIOSVersion(machine, iosVersion string) error {
	spec := LookupSpec(machine)
	if spec == nil {
		return fmt.Errorf("unknown machine: %s", machine)
	}
	if spec.MinIOSVersion == "" {
		return nil
	}
	if CompareIOSVersions(iosVersion, spec.MinIOSVersion) < 0 {
		return fmt.Errorf(
			"impossible iOS version: %s cannot run iOS %s (minimum supported: %s)",
			machine, iosVersion, spec.MinIOSVersion,
		)
	}
	return nil
}

// CompatibleIOSVersions returns every iOS version the given machine can
// actually run, drawn from the IOSBuildNumbers catalog. Filtering applies
// spec.MinIOSVersion as the hard lower bound (machine-determined) plus an
// optional caller-supplied minIOSVersion (e.g., "17.0" to avoid rolling
// very old releases that haven't shipped a security update in years).
// Pass "" for minIOSVersion to skip that extra filter.
func CompatibleIOSVersions(machine, minIOSVersion string) []string {
	spec := LookupSpec(machine)
	if spec == nil {
		return nil
	}
	// Caller's minimum OR machine's minimum — whichever is higher.
	lo := spec.MinIOSVersion
	if minIOSVersion != "" && CompareIOSVersions(minIOSVersion, lo) > 0 {
		lo = minIOSVersion
	}
	out := make([]string, 0, len(IOSBuildNumbers))
	for version := range IOSBuildNumbers {
		if lo == "" || CompareIOSVersions(version, lo) >= 0 {
			out = append(out, version)
		}
	}
	return out
}

// PickCompatibleIOSVersion returns a random iOS version that:
//
//   - is at or above the machine's MinIOSVersion, AND
//   - is at or above the caller-supplied minIOSVersion (pass "" to skip)
//
// Returns "" when no version in the catalog satisfies both bounds —
// callers should either widen the min or handle the empty string.
//
// Example:
//
//	// Any version the machine supports
//	v := devices.PickCompatibleIOSVersion("iPhone15,2", "")
//
//	// Modern-only (17.0 and up) for an iPhone 14 Pro
//	v := devices.PickCompatibleIOSVersion("iPhone15,2", "17.0")
//
//	// Recent-only (18.5 and up) for an iPhone 17 Pro
//	v := devices.PickCompatibleIOSVersion("iPhone18,1", "18.5")
func PickCompatibleIOSVersion(machine, minIOSVersion string) string {
	versions := CompatibleIOSVersions(machine, minIOSVersion)
	if len(versions) == 0 {
		return ""
	}
	return versions[rand.Intn(len(versions))]
}

// RandomMachine returns a random machine identifier from the DeviceSpecs catalog.
func RandomMachine() string {
	machines := make([]string, 0, len(DeviceSpecs))
	for m := range DeviceSpecs {
		machines = append(machines, m)
	}
	return machines[rand.Intn(len(machines))]
}

// RandomMachineAndIOS returns a random (machine, iosVersion) pair where the
// iOS version is guaranteed to be one the machine can actually run AND is at
// or above minIOSVersion. Pass "" for minIOSVersion to accept any compatible
// version.
//
// The pair is safe to feed into NewDevice() / BuildPayload() without hitting
// the MinIOSVersion compatibility check.
//
// Example:
//
//	// Any iOS version any supported machine can run
//	machine, ios := devices.RandomMachineAndIOS("")
//
//	// Only modern iOS 17.0+ combos (drops iPhone XS / XR if they'd only
//	// yield legacy versions — which they wouldn't since both support 17)
//	machine, ios := devices.RandomMachineAndIOS("17.0")
//
//	// Only very-recent 18.5+ combos — excludes older iPhones whose
//	// MinIOSVersion is ≤18.4 and no newer compatible versions exist for
//	// that specific machine
//	machine, ios := devices.RandomMachineAndIOS("18.5")
func RandomMachineAndIOS(minIOSVersion string) (machine, iosVersion string) {
	// Bounded retry loop so a too-strict min doesn't spin forever if the
	// caller happened to pass a version higher than any mapped version.
	for attempt := 0; attempt < 256; attempt++ {
		machine = RandomMachine()
		iosVersion = PickCompatibleIOSVersion(machine, minIOSVersion)
		if iosVersion != "" {
			return
		}
	}
	return "", ""
}
