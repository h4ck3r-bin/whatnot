package devices

import (
	"math/rand"

	"status403.com/rsavv/seonsdk-go/devices/behaviour"
)

// BehaviourMagField returns a plausible total magnetic-field magnitude
// (uncalibrated raw magnetometer, in µT) for the given LocalePreset +
// iPhone machine identifier, suitable for passing to
// BehaviourOptions.MagFieldMagnitude.
//
// CRITICAL: The SEON probe uses the RAW CMMagnetometer reading, not the
// calibrated CMDeviceMotion.magneticField. Raw magnetometer values include
// per-device hard-iron offsets from onboard ferromagnetic components
// (haptic engine, MagSafe magnet array, speaker assemblies). The 20-sample
// iPhone 15 / iOS 26.5 corpus shows raw |B| in the 613–650 µT band — about
// 13× Earth's field. That magnitude is dominated by the device's static
// hard-iron, NOT the geomagnetic field, so the locale-derived 25–65 µT
// outdoor/indoor bands the previous implementation used were off by ~15×.
//
// We center the magnitude on the corpus mean (~640 µT) with a per-fingerprint
// jitter band (~±15 µT) and a per-chip lift derived from the calibrated
// device profile. Locale geomagnetic intensity is intentionally ignored:
// for raw magnetometer readings it's a small (<10%) contribution dwarfed by
// hard-iron, and treating it as the dominant term produced corpus-incompatible
// values in earlier runs.
//
// The caller can still override the magnitude by setting MagFieldMagnitude
// explicitly in their BehaviourOptions when targeting a different chip
// family (older A10–A12 iPhones may sit lower; A17/A18 a touch higher).
//
// Passing an unknown preset returns the device-bound centre.
// Passing an unknown machine returns the centre with no chip bias.
func BehaviourMagField(preset LocalePreset, machine string) float64 {
	_ = preset // intentionally ignored — raw magnetometer is hard-iron dominated.
	const corpusMean = 640.0
	const corpusBand = 18.0 // half-width to cover 622–658, centred near 640

	chip := ChipForDevice(machine)
	profile := behaviour.ProfileForChip(chip)
	chipBias := profile.MagFieldIndoorUT - behaviour.DefaultProfile.MagFieldIndoorUT

	// Per-fingerprint jitter so two fingerprints from the same device don't
	// emit byte-identical magnitudes — but stay well inside the corpus band.
	jitter := (rand.Float64() - 0.5) * 2 * corpusBand

	return corpusMean + chipBias + jitter
}

// BehaviourMagDeclination returns the magnetic declination (degrees east,
// WMM-2025) for the given LocalePreset.
//
// The declination is a property of the **location's geomagnetic model** and
// is fixed for a given lat/lon — it does NOT change between heading events
// or fingerprints from the same device. The 20-sample iPhone 15 / iOS 26.5
// corpus from a Polish location reports `mag_declination: 6.9727...` to 7
// decimal places of stability across all 20 captures. The previous ±0.5°
// per-call jitter contradicted that signal; we now return the locale's
// central value verbatim.
//
// Callers that need geographic variation across a population should vary
// the LocalePreset itself, not jitter declination within one preset.
func BehaviourMagDeclination(preset LocalePreset) float64 {
	bundle := LocaleFromPreset(preset)
	return bundle.MagneticDeclination
}
