package behaviour

import (
	"math"
	"math/rand"
	"time"
)

// HeadingActivity controls how often CLLocationManager would deliver
// `didUpdateHeading:` events for the simulated scenario. Apple's CL is
// event-driven: it fires when |currentHeading - lastDelivered| >=
// headingFilter (default 1°), so the empirical delivery rate depends on
// physical state. Verified via IDA — SeonSDK never sets headingFilter
// (no `setHeadingFilter:` call in `startBehaviourMonitoring`), so the
// default 1° threshold is in effect.
//
// SeonSDK's per-event throttle is 200ms (`minimumTimeInterval = 0.2` at
// IDA 0x12784), and the dictionary key truncates to unix-seconds — so
// many same-second events collapse to one entry per second. The mean
// rate below is the EFFECTIVE rate of distinct second-keys, not raw CL
// callbacks.
type HeadingActivity int

const (
	// HeadingActivityHandheldStill — phone held in hand with low motion
	// (the typical Whatnot signup pose: user reading the screen). CL fires
	// 0.5-3 Hz; with second-key collapse we observe ~1 entry per second.
	// This is the default and matches the majority of historical valid
	// fingerprints in the testdata.
	HeadingActivityHandheldStill HeadingActivity = iota

	// HeadingActivityOnTable — phone face-up or face-down on a table.
	// Mag noise σ ≈ 0.05° rarely crosses the 1° filter, so CL fires
	// every 5-60 seconds. The real iPhone 17 Pro reference capture sits
	// in this regime: 2 entries 31 seconds apart.
	HeadingActivityOnTable

	// HeadingActivityHandheldActive — phone in hand with active rotation
	// (user moving the phone, walking). CL fires 5-25 Hz raw; second-key
	// collapse gives 1 entry per second every second the phone moves.
	HeadingActivityHandheldActive
)

// meanInterval returns the expected gap between distinct dictionary-key
// (unix-second) heading entries for this activity. Used as 1/λ for the
// Exponential inter-arrival distribution.
func (h HeadingActivity) meanInterval() time.Duration {
	switch h {
	case HeadingActivityHandheldActive:
		return 700 * time.Millisecond // ~1.4 Hz of distinct seconds
	case HeadingActivityOnTable:
		return 18 * time.Second // ~0.055 Hz, gaps 5-60s
	default: // HeadingActivityHandheldStill
		return 1100 * time.Millisecond // ~0.9 Hz, mostly consecutive seconds
	}
}

// HeadingActivityWeights defines a population mix of CL heading-delivery
// scenarios. Pass to .Pick() to draw an activity at random with these
// weights. Use this to break up the per-batch uniformity that comes from
// every fingerprint sharing the same scenario — real Whatnot user batches
// span hand-held typing, hand-held with active rotation, and the
// occasional phone-on-desk.
type HeadingActivityWeights struct {
	HandheldStill  float64
	HandheldActive float64
	OnTable        float64
}

// DefaultHeadingActivityWeights model a realistic Whatnot signup
// population:
//
//   - 70% HandheldStill — typical thumb-typing pose, phone held mostly steady
//   - 25% HandheldActive — both-thumbs typing with noticeable rotation, or
//     the user shifting the phone while typing
//   - 5%  OnTable — phone propped up or flat on a desk while the user types
//
// These shares keep most fingerprints in the "phone in hand" regime
// (matching the dominant historical valid population) while seeding
// enough OnTable variance to avoid the uniform-1Hz signature SEON could
// otherwise key on. Override via HeadingActivityWeights{}.Pick if your
// app's signup flow has a different physical mix.
var DefaultHeadingActivityWeights = HeadingActivityWeights{
	HandheldStill:  0.70,
	HandheldActive: 0.25,
	OnTable:        0.05,
}

// RandomizeHeadingActivity draws a HeadingActivity from
// DefaultHeadingActivityWeights. Equivalent to
// DefaultHeadingActivityWeights.Pick().
func RandomizeHeadingActivity() HeadingActivity {
	return DefaultHeadingActivityWeights.Pick()
}

// Pick returns a HeadingActivity sampled from w. If all weights are zero
// or any are negative, falls back to HeadingActivityHandheldStill (the
// safest majority-population choice).
func (w HeadingActivityWeights) Pick() HeadingActivity {
	total := w.HandheldStill + w.HandheldActive + w.OnTable
	if total <= 0 {
		return HeadingActivityHandheldStill
	}
	r := rand.Float64() * total
	switch {
	case r < w.HandheldStill:
		return HeadingActivityHandheldStill
	case r < w.HandheldStill+w.HandheldActive:
		return HeadingActivityHandheldActive
	default:
		return HeadingActivityOnTable
	}
}

// SimulateOptions tunes the simulator's input to reproduce a realistic
// monitoring window.
type SimulateOptions struct {
	// Duration of the monitoring window. The simulator runs CMDeviceMotion +
	// CMMagnetometer for this long when generating samples. By default the
	// emitted `monitoring_duration` is 0 (IDA 0x12438 + 0x13278's
	// read-before-write ordering on the first stop after start) — this matches
	// 20/20 captures in the iPhone 15 / iOS 26.5 corpus. Use EmitDuration if
	// you need to mimic an older Whatnot pattern that reported the buffer ms.
	Duration time.Duration

	// EmitDuration, when true, emits the actual buffer length in milliseconds
	// instead of 0. Older Whatnot captures (pre-iOS 26) reported non-zero ms
	// here, but the iPhone 15 / iOS 26.5 corpus is uniformly 0, so callers
	// should leave this at the default unless they're explicitly reproducing
	// the older pattern.
	EmitDuration bool

	// Seed — deterministic output when non-zero. Pass 0 for time-varying.
	Seed int64

	// Activity level during the window.
	Activity ActivityLevel

	// InitialHeading in degrees true (0 = north). If 0, simulator picks a
	// random facing direction on construction.
	InitialHeading float64

	// MagneticDeclination in degrees; mag_heading = true_heading - declination.
	// Sensible defaults per region:
	//   US East Coast       ≈ -14 (mag north is west of true north)
	//   US West Coast       ≈ +13
	//   UK / Western Europe ≈ -0.5 to -2
	//   Poland              ≈ +6
	//   Japan               ≈ -7
	MagneticDeclination float64

	// MagFieldMagnitude — total magnetic field in µT. Indoor 25–45, outdoor
	// 40–65. When 0, the simulator picks a sensible indoor value.
	MagFieldMagnitude float64

	// StartUnix — the UNIX second stamp that goes into heading_changes /
	// orientation_changes keys. Defaults to `time.Now().Unix()`.
	StartUnix int64

	// EmitAtEar — if true, include the at-ear transition states at 0x133f4
	// (rare for Idle monitoring windows; usually never triggers). If you
	// want a "phone was raised to ear then lowered" pattern, set this and
	// the simulator will emit one round-trip.
	EmitAtEar bool

	// Profile is the per-chip sensor noise profile. When zero (all fields 0),
	// Simulate() falls back to DefaultProfile (A15/A16 baseline). Typical
	// callers set this via ProfileForChip(ChipForDevice(machine)).
	Profile DeviceProfile

	// HeadingActivity selects the CLLocationManager heading-delivery
	// scenario to simulate. Default (zero value = HeadingActivityHandheldStill)
	// matches the typical Whatnot signup user: phone in hand with low motion,
	// CL fires roughly every 1.1s with Exponential jitter. Switch to
	// HeadingActivityOnTable for the rare-update case (matches the real
	// iPhone 17 Pro reference) or HeadingActivityHandheldActive for users
	// actively moving the phone.
	HeadingActivity HeadingActivity
}

// Simulate runs the full SEONBehaviourProbe pipeline for the configured
// duration and returns the exact `behaviour_results` dictionary the SDK
// would emit via -[stopBehaviourMonitoring] (IDA 0x1300c). Keys present:
//
//   user_acc stats:   min_user_acc, max_user_acc, mean_user_acc,
//                     median_user_acc, var_user_acc, p95_user_acc
//   rot_rate stats:   min_rot_rate, …, p95_rot_rate
//   gravity stats:    min_gravity,  …, p95_gravity    (based on gravity.z)
//   mag_field stats:  min_mag_field, …, p95_mag_field
//   mag_declination:  float (degrees)
//   heading_changes:  map[string]float64  — keys are "%d" unix-sec strings
//   orientation_changes: map[string]interface{}  — values may be [int] or
//                                                  [int, int]
//   monitoring_duration: int64 (ms)
//   most_freq_attitude: {"x": f, "y": f, "z": f, "w": f}
func Simulate(opts SimulateOptions) map[string]interface{} {
	if opts.Duration <= 0 {
		opts.Duration = 3 * time.Second
	}
	if opts.StartUnix == 0 {
		opts.StartUnix = time.Now().Unix()
	}
	// MagFieldMagnitude falls back to the chip-profile indoor band below,
	// picked inside NewSampleGeneratorForProfile when opts.MagFieldMagnitude
	// is left at 0.

	seed := opts.Seed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}

	profile := opts.Profile
	if profile == (DeviceProfile{}) {
		profile = DefaultProfile
	}

	gen := NewSampleGeneratorForProfile(seed, opts.Activity, profile)
	gen.MagneticDeclination = opts.MagneticDeclination
	if opts.MagFieldMagnitude != 0 {
		gen.MagFieldMagnitude = opts.MagFieldMagnitude
	}
	if opts.InitialHeading != 0 {
		gen.InitialHeading = opts.InitialHeading
	}

	// Buffers sized to match SDK bootstrap at 0x12520 / 0x126c8.
	userAcc := NewNumberBuffer(1000)
	rotRate := NewNumberBuffer(1000)
	gravityZ := NewNumberBuffer(1000)
	magField := NewNumberBuffer(1000)
	headingData := NewDictionaryBuffer(10)
	orientationChanges := NewDictionaryBuffer(10)
	attitude := NewAttitudeTracker()

	// Per-sample state.
	previousOrientation := OrientInitial // -1, matches 0x12794 init.
	isAtEar := false
	earFired := false // one-shot guard for EmitAtEar

	// The SDK runs CMDeviceMotion and CMMagnetometer both at 0.1 s. Heading
	// updates from CLLocationManager are event-driven (fires when |Δheading|
	// >= 1° per Apple's default `headingFilter`); SeonSDK throttles
	// per-event delivery to >= 200ms (IDA 0x12784) and the dictionary key
	// truncates to unix-seconds.
	//
	// Heading scheduling model — empirically tuned against the historical
	// valid population (100% of passing hand-held fingerprints have
	// CONSECUTIVE 1-second timestamps, no gaps). Real CL fires multiple
	// times per second for hand-held users because hand sway crosses the
	// 1° threshold repeatedly; after unix-second key collapse this always
	// yields consecutive coverage. Only the OnTable scenario (rare CL
	// firing) produces gaps, so:
	//
	//   - HandheldStill / HandheldActive: deterministic 1-event-per-second
	//     starting at t=1s. Matches the dominant valid pattern.
	//   - OnTable: Poisson-process inter-arrivals with mean 18s; first
	//     arrival jittered. Produces gaps and large inter-event spacing.
	const motionInterval = 100 * time.Millisecond
	motionSamples := int(opts.Duration / motionInterval)

	// headingRand is an independent stream used only by the OnTable
	// scheduler so its randomness doesn't perturb sample-generator seeds.
	headingRand := rand.New(rand.NewSource(seed ^ 0x5e07_5eed_0001))
	useSparseHeading := opts.HeadingActivity == HeadingActivityOnTable
	const headingDeterministicInterval = 1 * time.Second
	var nextHeadingAt time.Duration
	if useSparseHeading {
		// First OnTable arrival jittered by Exponential(mean), floored at
		// 200ms (SDK throttle). Many fingerprints will simply not see any
		// heading event before the monitoring window ends — that's
		// realistic for a phone-on-table.
		gap := time.Duration(headingRand.ExpFloat64() * float64(opts.HeadingActivity.meanInterval()))
		if gap < 200*time.Millisecond {
			gap = 200 * time.Millisecond
		}
		nextHeadingAt = gap
	} else {
		nextHeadingAt = headingDeterministicInterval
	}
	var lastMagDeclination float64

	for i := 0; i < motionSamples; i++ {
		tElapsed := time.Duration(i) * motionInterval
		tSec := tElapsed.Seconds()
		sampleUnix := opts.StartUnix + int64(tElapsed.Seconds())

		// ---- CMDeviceMotion handler (0x12b18) -----------------------------
		s := gen.Motion(tSec)

		// Apple emits gravity.z twice per motion update:
		// 1) via calculatePhysicalDeviceOrientationWithGravity (0x1428c).
		// 2) directly in the motion-block tail (0x12d60).
		// Replicate both so sample counts and stats match.
		orientIdx := OrientationFromGravity(s.Gravity[0], s.Gravity[1], s.Gravity[2])
		gravityZ.AddValue(s.Gravity[2])

		// Attitude-bucket update happens before orientation-change logic in
		// the SDK; 0x13ce4 skips only when isAtEar == 1.
		if !isAtEar {
			attitude.Update(s.Attitude)
		}

		// Check-suspicious-device-orientation FSM (0x133f4).
		gy := s.Gravity[1]
		gz := s.Gravity[2]
		uaX := s.UserAcc[0]
		uaY := s.UserAcc[1]
		rrZ := s.RotationRate[2]

		gravityAtEarAngle := math.Abs(gz) < 0.2 && math.Abs(gy) > 0.7
		// IDA 0x134f0–0x13514: the userAcc comparison is raw `>0.15`, NOT
		// fabs. The gravity and rotRate checks do use fabs (IDA 0x134a4 /
		// 0x13544) but userAcc does not — verified via FCMP+B.GT disasm
		// against qword_2D3C0 (= 0.15). Negative-only motion on uaX/uaY
		// does NOT trigger the at-ear FSM.
		handMoving := uaX > 0.15 || uaY > 0.15
		handRotating := math.Abs(rrZ) > 1.5
		roundedAttitude := RoundQuaternion(s.Attitude)
		nearMostFreq := IsAttitudeSimilar(roundedAttitude, attitude.MostFrequent)
		key := secondsKey(sampleUnix)

		switch {
		case opts.EmitAtEar && !earFired && tElapsed > opts.Duration/3 && !isAtEar && gravityAtEarAngle && handMoving && handRotating && !nearMostFreq:
			// Simulated at-ear transition start.
			orientationChanges.AddEntry(key, []interface{}{OrientAtEarStart, orientIdx})
			isAtEar = true
			earFired = true
		case isAtEar && nearMostFreq:
			// At-ear release.
			orientationChanges.AddEntry(key, []interface{}{OrientAtEarEnd, orientIdx})
			isAtEar = false
		default:
			// Plain orientation change.
			if orientIdx != previousOrientation {
				// Only record if the slot isn't already claimed by an
				// at-ear branch this second (matches 0x1379c "existing key
				// check"). DictionaryBuffer.AddEntry overwrites on collision
				// which replicates the real SDK's behaviour there.
				orientationChanges.AddEntry(key, []interface{}{orientIdx})
				previousOrientation = orientIdx
			}
		}

		// ---- CMMagnetometer handler (0x12e6c) -----------------------------
		// Magnetometer fires at 0.1 s too; we emit one sample per motion tick.
		mag := math.Sqrt(
			s.MagField[0]*s.MagField[0] +
				s.MagField[1]*s.MagField[1] +
				s.MagField[2]*s.MagField[2],
		)
		magField.AddValue(mag)

		// ---- UserAcceleration + RotationRate magnitudes -------------------
		uaMag := math.Sqrt(uaX*uaX + uaY*uaY + s.UserAcc[2]*s.UserAcc[2])
		rrMag := math.Sqrt(s.RotationRate[0]*s.RotationRate[0] +
			s.RotationRate[1]*s.RotationRate[1] + rrZ*rrZ)
		userAcc.AddValue(uaMag)
		rotRate.AddValue(rrMag)

		// ---- Heading update (0x1394c + 0x13b04) ---------------------------
		// Hand-held: deterministic 1-event-per-second, matching the
		// dominant pattern in the valid population (real CL fires
		// multiple times per second for hand-held usage and the
		// unix-second key collapses them to one entry per second).
		// OnTable: Exponential inter-arrivals giving sparse, gappy
		// coverage matching the real iPhone 17 Pro reference.
		for tElapsed >= nextHeadingAt {
			h := gen.Heading()
			// CLHeading values are float32 on the wire — widen through
			// float32 to match the real iPhone byte pattern (e.g., real
			// captures show heading entries like `250.80400085449219` =
			// float32 `250.804f` widened).
			lastMagDeclination = float64(float32(wrapHeading(h.TrueHeading - h.MagneticHeading)))
			arrivalUnix := opts.StartUnix + int64(nextHeadingAt.Seconds())
			headingData.AddEntryAtTime(time.Unix(arrivalUnix, 0), float64(float32(h.TrueHeading)))
			if useSparseHeading {
				gap := time.Duration(headingRand.ExpFloat64() * float64(opts.HeadingActivity.meanInterval()))
				if gap < 200*time.Millisecond {
					gap = 200 * time.Millisecond
				}
				nextHeadingAt += gap
			} else {
				nextHeadingAt += headingDeterministicInterval
			}
		}
	}

	// Most-frequent attitude — if nothing landed (edge case: zero samples),
	// fall back to the neutral Portrait quaternion. Matches the SDK's
	// behaviour where `mostFrequentAttitude` stays at zero until the first
	// update but `dictionaryFromQuaternion` emits `(0, 0, 0, 0)` in that case.
	mostFreq := attitude.MostFrequent
	if attitude.Buffer.Count() == 0 {
		mostFreq = Quaternion{}
	}

	// monitoring_duration: 0 is the iPhone 15 / iOS 26.5 corpus pattern
	// (20/20 captures emit 0). The IDA-canonical "first stop after start"
	// path produces this via the read-before-write ordering at 0x12438 +
	// 0x13278. Older Whatnot captures emitted non-zero ms here; opt back
	// into that with EmitDuration.
	emittedDuration := int64(0)
	if opts.EmitDuration {
		emittedDuration = int64(opts.Duration / time.Millisecond)
	}
	results := map[string]interface{}{
		"mag_declination":     lastMagDeclination,
		"heading_changes":     headingData.ToDictionary(),
		"orientation_changes": orientationChanges.ToDictionary(),
		"monitoring_duration": emittedDuration,
		"most_freq_attitude":  DictionaryFromQuaternion(mostFreq),
	}
	mergeInto(results, userAcc.Results("user_acc"))
	mergeInto(results, rotRate.Results("rot_rate"))
	mergeInto(results, gravityZ.Results("gravity"))
	mergeInto(results, magField.Results("mag_field"))
	return results
}

func secondsKey(unixSec int64) string {
	// Matches stringWithFormat:"%ld" — plain integer, no padding.
	return formatInt64(unixSec)
}

func formatInt64(v int64) string {
	// Avoid fmt.Sprintf in a hot loop for 1000s of samples/window.
	if v == 0 {
		return "0"
	}
	neg := false
	if v < 0 {
		neg = true
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// wrapHeading mirrors the ±180° clamping at 0x13a2c / 0x13a60 — heading delta
// wraps into the symmetric range.
func wrapHeading(d float64) float64 {
	for d > 180 {
		d -= 360
	}
	for d < -180 {
		d += 360
	}
	return d
}

func mergeInto(dst, src map[string]interface{}) {
	for k, v := range src {
		dst[k] = v
	}
}
