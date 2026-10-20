package behaviour

// DeviceProfile captures the per-chip-family sensor characteristics that shape
// a `behaviour_results` block. Different iPhone generations ship different
// IMU / magnetometer packages, and SEON's backend stores `machine` next to
// the resulting stats — an iPhone 9,1 emitting iPhone-17-quiet variance is
// an immediate cross-field tell.
//
// Source: Apple doesn't publish part selections, but the chip-family split
// correlates with the inertial hardware observed in teardowns:
//
//   A10 / A11 / A12 (iPhone 7/8/X/XS/XR) — older Bosch BMA + STMicro gyro.
//     Noisier, larger bias drift.
//   A13 / A14        (iPhone 11/12)       — Bosch BMI class, mid-noise.
//   A15 / A16        (iPhone 13/14/14Pro/15/15Plus) — current baseline.
//   A17 Pro / A18 / A19 (iPhone 15 Pro / 16 / 17)   — newer TDK InvenSense
//     / upgraded Bosch, quieter noise floor.
//
// Values are conservative first-cut σ; once real-device samples are captured
// via frida/capture.js we'll recalibrate against observed `var_*` bands.
type DeviceProfile struct {
	// UserAccSigma — per-axis σ of `userAcceleration` while idle, in g.
	UserAccSigma float64

	// RotRateSigma — per-axis σ of `rotationRate` while idle, in rad/s.
	RotRateSigma float64

	// MagFieldSigma — per-axis σ of `magnetometer` noise floor, in µT.
	MagFieldSigma float64

	// GravityDriftRadCap — the ±cap on slow gravity-vector drift (pitch/roll
	// in radians). Narrower on newer chips whose gyro bias stays stabler.
	GravityDriftRadCap float64

	// MagFieldIndoorUT — default total magnetic field magnitude for an indoor
	// environment (used only when the caller passes 0). iPhones since A16 have
	// a more isolated magnetometer (USB-C SoC away from the chip) which pulls
	// readings slightly higher; older models bottom out lower.
	MagFieldIndoorUT float64
}

// DefaultProfile is the A15/A16-class baseline used when a machine id is
// unknown. Matches the σ values sample_gen.go used before the per-chip split.
var DefaultProfile = DeviceProfile{
	UserAccSigma:       0.010,
	RotRateSigma:       0.030,
	MagFieldSigma:      0.80,
	GravityDriftRadCap: 0.35,
	MagFieldIndoorUT:   40,
}

// chipProfiles maps a chip identifier (see devices/chips.go ChipIdentifiers)
// to the sensor profile that chip's IMU family produces. Multiple iPhone
// models can share a chip; they share a profile.
var chipProfiles = map[string]DeviceProfile{
	// A10 Fusion — iPhone 7 / 7 Plus / 8 / 8 Plus / SE (1st gen). Bosch BMA280
	// class accelerometer, STMicro B329 gyro. Noisy.
	"T8010": {UserAccSigma: 0.016, RotRateSigma: 0.045, MagFieldSigma: 1.3, GravityDriftRadCap: 0.40, MagFieldIndoorUT: 38},

	// A11 Bionic — iPhone X / 8 refresh. Same package class as A10, minor
	// firmware update.
	"T8015": {UserAccSigma: 0.015, RotRateSigma: 0.042, MagFieldSigma: 1.25, GravityDriftRadCap: 0.40, MagFieldIndoorUT: 38},

	// A12 Bionic — iPhone XS / XR / XS Max. Bosch BMI160 class. Mid-noise.
	"T8020": {UserAccSigma: 0.014, RotRateSigma: 0.038, MagFieldSigma: 1.15, GravityDriftRadCap: 0.38, MagFieldIndoorUT: 39},

	// A13 Bionic — iPhone 11 / 11 Pro / SE 2nd gen. BMI270 class.
	"T8030": {UserAccSigma: 0.012, RotRateSigma: 0.035, MagFieldSigma: 1.0, GravityDriftRadCap: 0.37, MagFieldIndoorUT: 40},

	// A14 Bionic — iPhone 12 family. Refined BMI270.
	"T8101": {UserAccSigma: 0.011, RotRateSigma: 0.033, MagFieldSigma: 0.95, GravityDriftRadCap: 0.36, MagFieldIndoorUT: 40},

	// A15 Bionic — iPhone 13 family / 14 non-Pro / SE 3rd gen. Current baseline.
	"T8110": {UserAccSigma: 0.010, RotRateSigma: 0.030, MagFieldSigma: 0.80, GravityDriftRadCap: 0.35, MagFieldIndoorUT: 40},

	// A16 Bionic — iPhone 14 Pro / 15 / 15 Plus. Near-identical to A15 IMU but
	// slightly quieter gyro.
	"T8120": {UserAccSigma: 0.010, RotRateSigma: 0.028, MagFieldSigma: 0.78, GravityDriftRadCap: 0.34, MagFieldIndoorUT: 41},

	// A17 Pro — iPhone 15 Pro / Pro Max. New IMU stack, quieter floor.
	"T8122": {UserAccSigma: 0.008, RotRateSigma: 0.025, MagFieldSigma: 0.70, GravityDriftRadCap: 0.32, MagFieldIndoorUT: 42},

	// A18 — iPhone 16 / 16 Plus / 16e. Same IMU as A17 Pro.
	"T8130": {UserAccSigma: 0.008, RotRateSigma: 0.024, MagFieldSigma: 0.68, GravityDriftRadCap: 0.32, MagFieldIndoorUT: 42},

	// A18 Pro — iPhone 16 Pro / Pro Max. Slightly tighter gyro bias.
	"T8140": {UserAccSigma: 0.008, RotRateSigma: 0.023, MagFieldSigma: 0.68, GravityDriftRadCap: 0.31, MagFieldIndoorUT: 42},

	// A19 / A19 Pro — iPhone 17 family.
	//
	// Calibrated against a real-device capture (iPhone 17 Pro / iOS 26.1 /
	// Polish locale, idle state). The capture's sensor variances
	// back-solved to per-axis σ:
	//
	//   var_user_acc  = 7.14e-5 → σ ≈ 0.0125 g
	//   var_rot_rate  = 4.22e-4 → σ ≈ 0.0305 rad/s
	//   var_mag_field = 0.244   → σ ≈ 0.73 µT
	//
	// Values below sit between the previous extrapolation and the measured
	// values — the capture included environmental interference from a
	// MagSafe case so we don't want to over-anchor on a single sample.
	"T8150": {UserAccSigma: 0.010, RotRateSigma: 0.028, MagFieldSigma: 0.70, GravityDriftRadCap: 0.30, MagFieldIndoorUT: 43},
}

// ProfileForChip returns the sensor profile for a given chip identifier.
// Unknown chip → DefaultProfile (A15/A16-class baseline).
func ProfileForChip(chip string) DeviceProfile {
	if p, ok := chipProfiles[chip]; ok {
		return p
	}
	return DefaultProfile
}
