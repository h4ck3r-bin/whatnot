package devices

import "strings"

// ChipIdentifiers maps machine identifiers to their ARM64 chip suffix
// as it appears in the Darwin kernel version string (RELEASE_ARM64_T<chip>).
var ChipIdentifiers = map[string]string{
	// A10 Fusion
	"iPhone9,1": "T8010", "iPhone9,2": "T8010", "iPhone9,3": "T8010", "iPhone9,4": "T8010",
	// A11 Bionic
	"iPhone10,1": "T8015", "iPhone10,2": "T8015", "iPhone10,3": "T8015",
	"iPhone10,4": "T8015", "iPhone10,5": "T8015", "iPhone10,6": "T8015",
	// A12 Bionic
	"iPhone11,2": "T8020", "iPhone11,4": "T8020", "iPhone11,6": "T8020", "iPhone11,8": "T8020",
	// A13 Bionic
	"iPhone12,1": "T8030", "iPhone12,3": "T8030", "iPhone12,5": "T8030", "iPhone12,8": "T8030",
	// A14 Bionic
	"iPhone13,1": "T8101", "iPhone13,2": "T8101", "iPhone13,3": "T8101", "iPhone13,4": "T8101",
	// A15 Bionic
	"iPhone14,2": "T8110", "iPhone14,3": "T8110", "iPhone14,4": "T8110", "iPhone14,5": "T8110",
	"iPhone14,6": "T8110", "iPhone14,7": "T8110", "iPhone14,8": "T8110",
	// A16 Bionic
	"iPhone15,2": "T8120", "iPhone15,3": "T8120",
	"iPhone15,4": "T8120", "iPhone15,5": "T8120",
	// A17 Pro
	"iPhone16,1": "T8122", "iPhone16,2": "T8122",
	// A18
	"iPhone17,3": "T8130", "iPhone17,4": "T8130", "iPhone17,5": "T8130",
	// A18 Pro
	"iPhone17,1": "T8140", "iPhone17,2": "T8140",
	// A19 Pro
	"iPhone18,1": "T8150", "iPhone18,2": "T8150",
	// A19
	"iPhone18,3": "T8150", "iPhone18,4": "T8150",
}

// ChipForDevice returns the chip identifier suffix for a given machine identifier.
// Returns empty string if unknown.
func ChipForDevice(machine string) string {
	if chip, ok := ChipIdentifiers[machine]; ok {
		return chip
	}
	return ""
}

// ReplaceChipSuffix takes a kernel version string and replaces the RELEASE_ARM64_T<chip>
// suffix with the correct chip for the target device.
func ReplaceChipSuffix(kernelVersion string, targetChip string) string {
	if targetChip == "" || kernelVersion == "" {
		return kernelVersion
	}

	idx := strings.LastIndex(kernelVersion, "RELEASE_ARM64_T")
	if idx == -1 {
		return kernelVersion
	}

	return kernelVersion[:idx] + "RELEASE_ARM64_" + targetChip
}

// KernelVersionForDevice picks a kernel version string from the array for the given
// iOS version, and replaces the chip suffix to match the target device.
// Returns the last entry (release/RC) by default.
func KernelVersionForDevice(iosVersion string, machine string) string {
	versions, ok := KernelVersions[iosVersion]
	if !ok || len(versions) == 0 {
		return ""
	}

	base := versions[len(versions)-1]

	chip := ChipForDevice(machine)
	if chip == "" {
		return base
	}

	return ReplaceChipSuffix(base, chip)
}
