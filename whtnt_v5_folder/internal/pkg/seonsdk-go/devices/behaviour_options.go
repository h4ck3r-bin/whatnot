package devices

import (
	"time"

	seonsdk "status403.com/rsavv/seonsdk-go"
	"status403.com/rsavv/seonsdk-go/devices/behaviour"
)

// BehaviourOptions wraps the tunables exposed at the Device level for
// synthesising a `behaviour_results` block. When attached to DeviceOptions
// and Enabled is true, NewFingerprint() runs the simulator and populates
// FingerprintConfig.BehaviourResults.RawResults with the output.
//
// Reasonable defaults for a "phone-signup held-in-hand" window (the
// scenario Whatnot's login / phone-signup SEON calls target) are filled in
// when fields are left at their zero values.
type BehaviourOptions struct {
	// Enabled must be true to activate simulation.
	Enabled bool

	// Duration of the monitoring window. When 0, defaults to 3 s which is
	// the low end of realistic phone-signup latency (user sees the screen,
	// enters a phone number, taps submit).
	Duration time.Duration

	// Activity level while the window is active.
	Activity behaviour.ActivityLevel

	// Seed for deterministic output. 0 = time-seeded.
	Seed int64

	// InitialHeading in degrees true (0–360). When 0, the simulator picks a
	// random facing direction.
	InitialHeading float64

	// MagneticDeclination in degrees; difference between true and magnetic
	// heading at the simulated user's location.
	MagneticDeclination float64

	// MagFieldMagnitude — total magnetic field in µT. 0 falls back to an
	// indoor default (~40 µT).
	MagFieldMagnitude float64

	// EmitAtEar, when true, allows the orientation-change FSM to fire an
	// at-ear transition (rare for idle windows). Leave false for the common
	// phone-in-hand case.
	EmitAtEar bool

	// HeadingActivity selects the CLLocationManager heading-delivery
	// scenario. Default (zero value = HeadingActivityHandheldStill)
	// matches the typical Whatnot signup user. Use
	// behaviour.RandomizeHeadingActivity() to draw from
	// behaviour.DefaultHeadingActivityWeights (70% Still / 25% Active /
	// 5% OnTable) per fingerprint and break up the per-batch uniformity
	// SEON could otherwise key on.
	HeadingActivity behaviour.HeadingActivity

	// EmitDuration, when true, emits Duration.Milliseconds() in
	// `monitoring_duration` instead of the default 0.
	//
	// The iPhone 15 / iOS 26.5 corpus has 20/20 captures with
	// `monitoring_duration: 0` — that's the IDA-canonical "first stop after
	// start" path (read-before-write at 0x12438 + 0x13278). Older Whatnot
	// captures emitted non-zero ms here; flip this on only if you're
	// reproducing that older pattern.
	EmitDuration bool
}

// simulateOptionsForMachine converts the device-level options to the
// simulator's input and picks a chip-specific sensor noise profile when the
// chip has been calibrated against real-device data, otherwise falls back
// to behaviour.DefaultProfile.
//
// Currently only `T8150` (A19 / A19 Pro — iPhone 17 family) has verified
// σ values. Other chip profiles in devices/behaviour/profiles.go are
// unverified extrapolations, so we route everything else through
// DefaultProfile to stay in the known-safe band. When a real-device
// capture lands for another chip family, add it to `calibratedChips`.
func (o *BehaviourOptions) simulateOptionsForMachine(machine string) behaviour.SimulateOptions {
	profile := behaviour.DefaultProfile
	if chip := ChipForDevice(machine); chipIsCalibrated(chip) {
		profile = behaviour.ProfileForChip(chip)
	}
	return behaviour.SimulateOptions{
		Duration:            o.Duration,
		Seed:                o.Seed,
		Activity:            o.Activity,
		InitialHeading:      o.InitialHeading,
		MagneticDeclination: o.MagneticDeclination,
		MagFieldMagnitude:   o.MagFieldMagnitude,
		EmitAtEar:           o.EmitAtEar,
		Profile:             profile,
		HeadingActivity:     o.HeadingActivity,
		EmitDuration:        o.EmitDuration,
	}
}

// calibratedChips is the allow-list of chip identifiers whose
// devices/behaviour/profiles.go entry has been verified against a
// real-device behaviour_results capture. Unlisted chips fall back to
// DefaultProfile (A15/A16-class).
var calibratedChips = map[string]bool{
	"T8150": true, // A19 / A19 Pro — iPhone 17 family (iOS 26.1 capture)
}

func chipIsCalibrated(chip string) bool {
	return calibratedChips[chip]
}

// buildBehaviourData runs the simulator and wraps the output in a
// *seonsdk.BehaviourData. A nil or disabled BehaviourOptions yields nil
// (no behaviour_results in the payload).
func buildBehaviourData(opts *BehaviourOptions, machine string) *seonsdk.BehaviourData {
	if opts == nil || !opts.Enabled {
		return nil
	}
	out := behaviour.Simulate(opts.simulateOptionsForMachine(machine))
	return &seonsdk.BehaviourData{RawResults: out}
}
