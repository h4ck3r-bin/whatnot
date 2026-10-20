package behaviour

import (
	"math"
	"math/rand"
)

// ActivityLevel classifies the user–device interaction pattern during the
// monitoring window. Values chosen to be realistic for the iPhone's accel/gyro
// output (in g and rad/s respectively). All three activities remain in
// Portrait orientation — no flips to landscape/face-down.
type ActivityLevel int

const (
	// ActivityIdle: user is holding the phone roughly still (reading a
	// screen, waiting). Only natural hand tremor contributes to userAcc /
	// rotRate, with very small σ.
	ActivityIdle ActivityLevel = iota

	// ActivityTyping: user is typing on the keyboard. Short motion bursts
	// (tap reaction + thumb travel) overlaid on the Idle baseline.
	ActivityTyping

	// ActivityWalking: user is walking while using the phone. Periodic
	// vertical bouncing, sustained hand rotation from gait.
	ActivityWalking
)

// PopulationActivityWeights is the activity mix that produces across-capture
// variance in user_acc / rot_rate consistent with the iPhone 15 / iOS 26.5
// corpus. Most users are typing (Whatnot signup is keyboard-heavy), some are
// reading-still, and a few are walking around with the device.
var PopulationActivityWeights = struct {
	Idle    float64
	Typing  float64
	Walking float64
}{
	Idle:    0.20,
	Typing:  0.60,
	Walking: 0.20,
}

// RandomizeActivity samples an ActivityLevel from PopulationActivityWeights.
// Use this in fingerprint generation paths instead of a fixed Activity to
// keep across-capture sensor-stat distributions matching the corpus.
func RandomizeActivity() ActivityLevel {
	r := rand.Float64()
	w := PopulationActivityWeights
	total := w.Idle + w.Typing + w.Walking
	if total <= 0 {
		return ActivityTyping
	}
	r *= total
	switch {
	case r < w.Idle:
		return ActivityIdle
	case r < w.Idle+w.Typing:
		return ActivityTyping
	default:
		return ActivityWalking
	}
}

// Sample is one instantaneous reading the SDK's motion-handler block would
// receive from CMMotionManager at a 0.1 s cadence.
type Sample struct {
	UserAcc      [3]float64 // g
	RotationRate [3]float64 // rad/s
	Gravity      [3]float64 // g, unit vector
	Attitude     Quaternion
	MagField     [3]float64 // µT
}

// HeadingSample is one CLLocationManager heading update.
type HeadingSample struct {
	TrueHeading     float64 // degrees
	MagneticHeading float64 // degrees
}

// SampleGenerator emits Sample and HeadingSample values for a given
// monitoring duration. It's stateful so successive calls produce correlated
// drifts (the hand rotates smoothly, the heading wobbles by a few degrees,
// etc.) rather than independent noise.
type SampleGenerator struct {
	Rng                *rand.Rand
	Activity           ActivityLevel
	InitialHeading     float64 // degrees true
	MagneticDeclination float64 // degrees; mag_heading = true_heading - declination
	MagFieldMagnitude  float64 // µT; typical indoor 25-45, outdoor 40-65
	Profile            DeviceProfile // per-chip noise characteristics

	// Internal state.
	heading             float64
	baseAttitude        Quaternion // Portrait-up reference
	gravityDriftTheta   float64    // accumulated rotation for slow drift
	gravityDriftPhi     float64
	// Initial tilt seeded at construction. Clamps below restrict drift
	// RELATIVE to this pose so the user's hand-held angle is preserved
	// across the monitoring window instead of being ratcheted back to
	// upright on the first sample.
	initialDriftTheta float64
	initialDriftPhi   float64
	typingBurstCooldown int // samples remaining in a typing burst
}

// NewSampleGenerator builds a generator seeded with defaults tuned for a
// stationary, upright iPhone in a user's hand. Uses DefaultProfile (A15/A16
// class noise floor) — use NewSampleGeneratorForProfile when you have a
// chip-specific DeviceProfile.
func NewSampleGenerator(seed int64, activity ActivityLevel) *SampleGenerator {
	return NewSampleGeneratorForProfile(seed, activity, DefaultProfile)
}

// NewSampleGeneratorForProfile builds a generator whose noise characteristics
// match the supplied DeviceProfile (typically ProfileForChip(machine)).
//
// Initial pose: a real user holds the phone tilted toward themselves — not
// perfectly vertical. The 20-sample iPhone 15 / iOS 26.5 corpus shows
// `mean_gravity` (== mean(gravity.z)) clustered between -0.5 and -1.0 with
// μ ≈ -0.7, which corresponds to the phone tilted ~45° from vertical (top
// of the screen leaning toward the user). We seed the gravity-drift state
// inside the matching theta band so the initial gravity vector lies in the
// corpus envelope rather than at the perfectly upright (0, -1, 0) we used
// to emit.
func NewSampleGeneratorForProfile(seed int64, activity ActivityLevel, profile DeviceProfile) *SampleGenerator {
	r := rand.New(rand.NewSource(seed))
	gen := &SampleGenerator{
		Rng:                 r,
		Activity:            activity,
		InitialHeading:      180 + r.Float64()*180, // arbitrary facing direction
		MagneticDeclination: -0.5,                  // default ≈ UK/EU mid-latitude
		MagFieldMagnitude:   profile.MagFieldIndoorUT - 5 + r.Float64()*10, // chip-tuned indoor band
		Profile:             profile,
		heading:             0,
		// Real iPhones never emit a perfect (0,0,0,1) identity attitude —
		// inherent gyro bias + sensor-fusion settling leaves a tiny
		// non-zero offset on x/y/z even when the device is stationary.
		// Real iPhone 17 Pro capture showed most_freq_attitude =
		// (0.01, 0.01, -0, 1). A perfect identity is a physics-impossible
		// tell.
		baseAttitude: initialAttitudeWithBias(r),
	}
	// Seed gravity drift to a hand-held tilt. theta directly drives gravity.z
	// via gz = sin(theta). To keep gz in [-0.99, -0.5] (corpus envelope), we
	// pick theta uniformly in [-π/2, -π/6]. Phi stays small — most users
	// don't roll the phone significantly while typing.
	gen.gravityDriftTheta = -(math.Pi/6 + r.Float64()*(math.Pi/2-math.Pi/6))
	gen.gravityDriftPhi = (r.Float64() - 0.5) * 0.10
	gen.initialDriftTheta = gen.gravityDriftTheta
	gen.initialDriftPhi = gen.gravityDriftPhi
	return gen
}

// Motion produces one CMDeviceMotion sample at `tSec` seconds into the
// monitoring window.
//
// Noise model rationale (with no real-device data yet, conservative defaults):
//
//   Idle:
//     userAcc σ per-axis = 0.01 g   (published iPhone accelerometer noise
//                                     floor + hand tremor at 1–3 Hz)
//     rotRate σ per-axis = 0.03 rad/s  (MEMS gyro noise + micro-wobble)
//     gravity magnitude = 1.0 g stable, orientation drifts by ±0.02 rad
//                         over 10 s (slow hand rotation)
//
//   Typing: same idle baseline + 0.05–0.15 g spike bursts every 0.5–2 s.
//
//   Walking: overlay a 2 Hz vertical bounce (amplitude 0.10 g) on gravity-Y
//            and a 1 Hz rotation wiggle (±0.3 rad/s) on rotRate.Y.
//
// These σs are intentionally conservative; once real `behaviour_results`
// samples arrive via the Frida capture we calibrate them against observed
// `var_user_acc` / `var_rot_rate` / `var_gravity` / `var_mag_field`.
func (g *SampleGenerator) Motion(tSec float64) Sample {
	var userAcc, rotRate [3]float64

	// Baseline idle tremor — noise σ picked from the device profile so
	// older/noisier chips produce a wider variance band than newer quiet
	// chips. See DeviceProfile for the per-chip calibration.
	uaSigma := g.Profile.UserAccSigma
	rrSigma := g.Profile.RotRateSigma
	for i := 0; i < 3; i++ {
		userAcc[i] = g.Rng.NormFloat64() * uaSigma
		rotRate[i] = g.Rng.NormFloat64() * rrSigma
	}

	switch g.Activity {
	case ActivityTyping:
		// Fire a burst every ~0.5–2 s. Burst lasts ~3–8 samples (300–800 ms).
		if g.typingBurstCooldown > 0 {
			// Mid-burst: larger σ on user acceleration.
			userAcc[0] += g.Rng.NormFloat64() * 0.08
			userAcc[1] += g.Rng.NormFloat64() * 0.08
			userAcc[2] += g.Rng.NormFloat64() * 0.05
			rotRate[0] += g.Rng.NormFloat64() * 0.15
			rotRate[2] += g.Rng.NormFloat64() * 0.15
			g.typingBurstCooldown--
		} else if g.Rng.Float64() < 0.08 {
			// New burst: ~8% probability per sample (10 Hz → once every
			// ~1.25 s on average).
			g.typingBurstCooldown = 3 + g.Rng.Intn(6)
		}

	case ActivityWalking:
		// 2 Hz vertical gait + gentle rotation.
		gait := 0.10 * math.Sin(2*math.Pi*2*tSec)
		userAcc[1] += gait
		userAcc[0] += g.Rng.NormFloat64() * 0.03
		userAcc[2] += g.Rng.NormFloat64() * 0.03
		rotRate[1] += 0.3*math.Sin(2*math.Pi*1*tSec) + g.Rng.NormFloat64()*0.05
	}

	// Gravity: slow drift around (0, -1, 0) — Portrait upright.
	//
	// CMDeviceMotion's `gravity` is the OUTPUT of Apple's sensor-fusion
	// pipeline (gyro + accelerometer + magnetometer) — it actively
	// corrects gyro bias so a stationary device emits near-zero drift on
	// the gravity channel. An iPhone 17 Pro capture while idle showed
	// `var_gravity ≈ 1.7e-10` — essentially floating-point noise. Our
	// previous drift rate (0.002 rad/sample) was appropriate for the
	// RAW gyro signal, not the fused gravity vector.
	//
	// Per-activity calibration:
	//   Idle     → 0.00005 rad/sample (~0° drift over 30 s — matches real)
	//   Typing   → 0.0003 rad/sample  (thumb travel rotates the device mildly)
	//   Walking  → 0.002 rad/sample   (gait-driven hand rotation dominates)
	var driftPerSample float64
	switch g.Activity {
	case ActivityWalking:
		driftPerSample = 0.002
	case ActivityTyping:
		driftPerSample = 0.0003
	default:
		driftPerSample = 0.00005
	}
	g.gravityDriftTheta += g.Rng.NormFloat64() * driftPerSample
	g.gravityDriftPhi += g.Rng.NormFloat64() * driftPerSample

	// Clamp drift to the chip profile's gyro-bias envelope. The cap is
	// applied RELATIVE to the seeded initial tilt so the user's hand-held
	// pose isn't ratcheted back to upright. Cap only matters for extended
	// monitoring windows + Walking activity; idle drift stays well inside.
	cap := g.Profile.GravityDriftRadCap
	if cap == 0 {
		cap = 0.35
	}
	g.gravityDriftTheta = clamp(g.gravityDriftTheta, g.initialDriftTheta-cap, g.initialDriftTheta+cap)
	g.gravityDriftPhi = clamp(g.gravityDriftPhi, g.initialDriftPhi-cap, g.initialDriftPhi+cap)

	// Rotate the canonical Portrait gravity (0, -1, 0) by (theta, phi).
	gx := math.Sin(g.gravityDriftPhi)
	gy := -math.Cos(g.gravityDriftTheta) * math.Cos(g.gravityDriftPhi)
	gz := math.Sin(g.gravityDriftTheta)
	// Renormalise (rounding guard — not required for unit sin/cos but safe).
	mag := math.Sqrt(gx*gx + gy*gy + gz*gz)
	if mag > 0 {
		gx, gy, gz = gx/mag, gy/mag, gz/mag
	}
	// Individual gravity axes come from CoreMotion as float32 widened to
	// double. Pre-quantize so downstream float32-shaped stats (var_gravity,
	// min/max/mean/p95_gravity) match what a real iPhone would emit.
	gravity := [3]float64{
		float64(float32(gx)),
		float64(float32(gy)),
		float64(float32(gz)),
	}

	// Attitude: rotate the base Portrait quaternion by the same drift.
	attitude := rotateQuaternion(g.baseAttitude, g.gravityDriftTheta, g.gravityDriftPhi, 0)

	// Magnetic field: project the total magnitude onto three axes based on
	// the device's current heading (rotated into the phone's frame by
	// drift). Simplified to: x and y carry the horizontal field, z has the
	// dip. Noise σ = 0.8 µT (typical magnetometer noise floor).
	horiz := g.MagFieldMagnitude * math.Cos(degToRad(65)) // 65° dip typical EU
	dipZ := g.MagFieldMagnitude * math.Sin(degToRad(65))
	magSigma := g.Profile.MagFieldSigma
	if magSigma == 0 {
		magSigma = 0.8
	}
	hx := horiz*math.Sin(degToRad(g.InitialHeading+g.heading)) + g.Rng.NormFloat64()*magSigma
	hy := horiz*math.Cos(degToRad(g.InitialHeading+g.heading)) + g.Rng.NormFloat64()*magSigma
	hz := dipZ + g.Rng.NormFloat64()*magSigma
	magField := [3]float64{hx, hy, hz}

	return Sample{
		UserAcc:      userAcc,
		RotationRate: rotRate,
		Gravity:      gravity,
		Attitude:     attitude,
		MagField:     magField,
	}
}

// Heading produces one CLLocationManager heading update. The SDK receives
// these asynchronously on the main queue; rate is typically 1 Hz when the
// device isn't moving and up to 10 Hz while walking.
func (g *SampleGenerator) Heading() HeadingSample {
	// Random walk: drift by a few degrees per call.
	g.heading += g.Rng.NormFloat64() * 1.5
	// Clamp drift to ±15° so the simulated user doesn't rotate 180° in
	// 10 s just from noise.
	g.heading = clamp(g.heading, -15, 15)
	t := g.InitialHeading + g.heading
	if t < 0 {
		t += 360
	}
	if t >= 360 {
		t -= 360
	}
	return HeadingSample{TrueHeading: t, MagneticHeading: t - g.MagneticDeclination}
}

// portraitUpQuaternion is the CMAttitude quaternion for an iPhone held
// upright in Portrait (display facing user, home-button down).
// Reference: CMDeviceMotion with referenceFrame = XTrueNorthZVertical at rest
// in Portrait produces ~(0, 0, 0, 1). We use (0, 0, 0, 1) as the neutral.
func portraitUpQuaternion() Quaternion {
	return Quaternion{X: 0, Y: 0, Z: 0, W: 1}
}

// initialAttitudeWithBias returns a Portrait-upright quaternion offset by a
// small, per-device gyro bias. Real iPhones consistently emit
// most_freq_attitude values with non-zero x/y/z components (0.01 range)
// even when sitting perfectly still — a consequence of sensor-fusion
// converging to a slightly off-axis settled state rather than mathematical
// identity. Picking a random bias per Device reproduces that characteristic
// while keeping the attitude stable across samples within one monitoring
// window (seed controls the offset).
//
// Bias magnitude: chosen such that at least one axis reliably rounds to a
// non-zero bucket at 0.01 quantisation (BucketKeyForAttitude multiplies by
// 10 and rounds). Each axis picks a sign then a magnitude in [0.008, 0.025],
// guaranteeing |rounded| ≥ 0.01 while still producing the natural-looking
// one-axis-at-zero pattern a real iPhone shows (e.g., the iPhone 17 Pro
// capture's `(0.01, 0.01, -0, 1)` has z = signed zero from a bias that
// rounded to 0).
func initialAttitudeWithBias(r *rand.Rand) Quaternion {
	biasAxis := func() float64 {
		sign := 1.0
		if r.Float64() < 0.5 {
			sign = -1.0
		}
		return sign * (0.008 + r.Float64()*0.017)
	}
	bx := biasAxis()
	by := biasAxis()
	bz := biasAxis()
	// Occasionally zero out one axis to reproduce the "one axis signed zero"
	// pattern real captures show (e.g., z = -0).
	switch r.Intn(8) {
	case 0:
		bx = 0
	case 1:
		by = 0
	case 2:
		bz = 0
	}
	// Normalize — true quaternions are unit length. For tiny biases this
	// is nearly a no-op but preserves the invariant.
	w := math.Sqrt(1 - bx*bx - by*by - bz*bz)
	return Quaternion{X: bx, Y: by, Z: bz, W: w}
}

// rotateQuaternion applies a small rotation (pitch, roll, yaw) in radians
// to an existing quaternion. For the small drifts we simulate (< 0.35 rad)
// a first-order approximation via axis-angle composition is fine.
func rotateQuaternion(q Quaternion, pitch, roll, yaw float64) Quaternion {
	// Convert Euler (pitch around X, roll around Z here for phone frame) to
	// a small rotation quaternion. Order: yaw → pitch → roll.
	cy := math.Cos(yaw * 0.5)
	sy := math.Sin(yaw * 0.5)
	cp := math.Cos(pitch * 0.5)
	sp := math.Sin(pitch * 0.5)
	cr := math.Cos(roll * 0.5)
	sr := math.Sin(roll * 0.5)

	dq := Quaternion{
		W: cr*cp*cy + sr*sp*sy,
		X: sr*cp*cy - cr*sp*sy,
		Y: cr*sp*cy + sr*cp*sy,
		Z: cr*cp*sy - sr*sp*cy,
	}
	return multiplyQuaternions(q, dq)
}

func multiplyQuaternions(a, b Quaternion) Quaternion {
	return Quaternion{
		W: a.W*b.W - a.X*b.X - a.Y*b.Y - a.Z*b.Z,
		X: a.W*b.X + a.X*b.W + a.Y*b.Z - a.Z*b.Y,
		Y: a.W*b.Y - a.X*b.Z + a.Y*b.W + a.Z*b.X,
		Z: a.W*b.Z + a.X*b.Y - a.Y*b.X + a.Z*b.W,
	}
}

func degToRad(d float64) float64 { return d * math.Pi / 180 }

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}
