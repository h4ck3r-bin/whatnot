# Behaviour Probe Simulation

The `devices/behaviour` package produces a synthetic `behaviour_results` block byte-shape-identical to what `SEONBehaviourProbe` emits on a real iPhone held in a user's hand for a given duration. This lets `Device.NewFingerprint(...)` emit payloads that look like they came from a device that had `startBehaviourMonitoring` running during a login or phone-signup flow.

**Scope.** The simulator only fires on the three Whatnot call sites that do behaviour monitoring: login (`0x1001df9a4`), phone-signup entry (`0x10026f1b0`), and phone-onboarding teardown (`0x10026cffc`). The other 10 Whatnot SEON call sites — address mutations, misc. sign-up attribution, etc. — do NOT run behaviour monitoring, so their fingerprints legitimately have no `behaviour_results` field. See [`device-api.md`](device-api.md#fingerprint-overrides) for how to toggle behaviour per call.

## What gets emitted

The top-level `behaviour_results` dict holds exactly the 30 keys `-[SEONBehaviourProbe stopBehaviourMonitoring]` (IDA `0x1300c`) produces:

| Category | Keys | Source |
|---|---|---|
| User acceleration magnitude (g) | `min_user_acc`, `max_user_acc`, `mean_user_acc`, `median_user_acc`, `var_user_acc`, `p95_user_acc` | CMDeviceMotion `userAcceleration` magnitude |
| Rotation rate magnitude (rad/s) | `min_rot_rate`, `max_rot_rate`, `mean_rot_rate`, `median_rot_rate`, `var_rot_rate`, `p95_rot_rate` | CMDeviceMotion `rotationRate` magnitude |
| Gravity Z (g) | `min_gravity`, `max_gravity`, `mean_gravity`, `median_gravity`, `var_gravity`, `p95_gravity` | CMDeviceMotion `gravity.z` — **double-emitted per motion update** (0x1428c + 0x12d60) |
| Magnetic field magnitude (µT) | `min_mag_field`, `max_mag_field`, `mean_mag_field`, `median_mag_field`, `var_mag_field`, `p95_mag_field` | CMMagnetometer magnitude |
| Heading deltas | `mag_declination` | `trueHeading - magneticHeading`, wrapped to `[-180, +180]` |
| Heading trail | `heading_changes` | `map[unix-sec-string] → trueHeading` (max 10 entries) |
| Orientation trail | `orientation_changes` | `map[unix-sec-string] → [orient_idx]` or `[sentinel, orient_idx]` (max 10 entries) |
| Window length | `monitoring_duration` | `int64` ms — `llround((stop - start) * 1000)` |
| Dominant attitude | `most_freq_attitude` | `{x, y, z, w}` — bucket-center of the attitudeBucket winner |

Every statistic formula in the first four rows reproduces Apple's quirks exactly:

- **Empty buffer → 0.0** for every stat (matches 0xbed0 / 0xbfb0 / 0xbd50 / 0xc0bc / 0xc124).
- **Population variance** (`sumSqr/n - mean²`), clamped ≥ 0, returns 0 when n < 2.
- **`p95 = sorted[floor((n-1)*0.95)]`** — integer truncation, not linear interpolation.
- **Median** is the mean of the two middle values for even n (0xbd50).
- **Ring buffers** cap at 1000 samples and overwrite from `currentIndex=0` when full.

## Orientation table

The 14-entry gravity-vector → orient_idx table extracted from `unk_2D3F8`:

| Idx | Gravity (x, y, z) | Meaning |
|---:|---|---|
| 2 | (0, 0, -1) | FaceUp |
| 3 | (0, 0, +1) | FaceDown |
| 4 | (+1, 0, 0) | LandscapeRight |
| 5 | (-1, 0, 0) | LandscapeLeft |
| 6 | (0, +1, 0) | PortraitUpsideDown |
| **7** | **(0, -1, 0)** | **Portrait — held-upright default** |
| 8 / 9 | (0, -0.87, ±0.5) | Portrait ±30° tilted |
| 10 / 11 | (0, +0.87, ±0.5) | PortraitUD ±30° tilted |
| 12 / 13 | (-0.87, 0, ±0.5) | LL ±30° tilted |
| 14 / 15 | (+0.87, 0, ±0.5) | LR ±30° tilted |

At-ear sentinels:
- `1` (from `off_43D60`): phone transitioned to ear.
- `0` (from `off_43D78`): phone released from ear.
- `-1` (from `off_43D90`): initial `previousOrientation` value before the first motion sample.

## At-ear FSM semantics (`-[checkSuspiciousDeviceOrientation:]` — IDA 0x133f4)

Trigger conditions to emit `[AtEarStart, orient]`:

1. `|gravity.z| < 0.2`
2. `|gravity.y| > 0.7`
3. `|userAcc.x| > 0.15` **or** `|userAcc.y| > 0.15`
4. `|rotationRate.z| > 1.5`
5. Rounded current attitude is NOT similar to `mostFrequentAttitude` (cosine similarity ≤ 0.96)
6. `!isAtEar` (not already in at-ear state)

To emit `[AtEarEnd, orient]`:

- `isAtEar` is currently set
- Rounded current attitude IS similar to `mostFrequentAttitude`

Otherwise, if `orientIdx != previousOrientation` for the current gravity vector, the buffer records `[orientIdx]` keyed by the current unix-second.

## Sampling rates

- **CMDeviceMotion**: `setDeviceMotionUpdateInterval: 0.1` → **10 Hz** (0x12a20).
- **CMMagnetometer**: `setMagnetometerUpdateInterval: 0.1` → **10 Hz** (0x12df0).
- **CLHeading**: depends on CoreLocation delivery — simulator emits at **1 Hz** (conservative; idle devices throttle below 10 Hz).

Gravity-Z gets added to `gravityZ` buffer **twice per motion update** (once in `calculatePhysicalDeviceOrientationWithGravity` at 0x1428c, once in the motion block tail at 0x12d60). The simulator reproduces this, so `gravity` stat sample counts are 2× user-acc and rot-rate.

## Noise model defaults (per chip family)

Rather than ship a single σ for every iPhone, the simulator resolves a `DeviceProfile` from the machine's chip identifier (`ChipForDevice` in `devices/chips.go`) and threads it into `SampleGenerator`. Older chips ship noisier IMUs; newer chips (A17 Pro / A18 / A19) carry a quieter inertial stack. SEON's backend stores the machine next to the observed stats, so emitting A17-quiet values on an iPhone 9,1 is a cross-field tell — calibrating per chip avoids that.

| Chip family | Machines | userAcc σ (g) | rotRate σ (rad/s) | magField σ (µT) | Gravity drift cap (rad) | Indoor field (µT) |
|---|---|---|---|---|---|---|
| A10 (T8010) | iPhone 7 / 8 / SE1 | 0.016 | 0.045 | 1.30 | 0.40 | 38 |
| A11 (T8015) | iPhone X | 0.015 | 0.042 | 1.25 | 0.40 | 38 |
| A12 (T8020) | iPhone XS / XR | 0.014 | 0.038 | 1.15 | 0.38 | 39 |
| A13 (T8030) | iPhone 11 / SE2 | 0.012 | 0.035 | 1.00 | 0.37 | 40 |
| A14 (T8101) | iPhone 12 | 0.011 | 0.033 | 0.95 | 0.36 | 40 |
| A15 (T8110) | iPhone 13 / 14 / SE3 | 0.010 | 0.030 | 0.80 | 0.35 | 40 |
| A16 (T8120) | iPhone 14 Pro / 15 | 0.010 | 0.028 | 0.78 | 0.34 | 41 |
| A17 Pro (T8122) | iPhone 15 Pro | 0.008 | 0.025 | 0.70 | 0.32 | 42 |
| A18 (T8130) | iPhone 16 / 16e | 0.008 | 0.024 | 0.68 | 0.32 | 42 |
| A18 Pro (T8140) | iPhone 16 Pro | 0.008 | 0.023 | 0.68 | 0.31 | 42 |
| A19 / Pro (T8150) | iPhone 17 | 0.007 | 0.022 | 0.65 | 0.30 | 43 |

**Unknown chip / no machine supplied** falls back to `DefaultProfile` (A15/A16 baseline).

**Wiring** — the `Device` path automatically picks the profile from its frozen `machine`:

```go
// devices/behaviour_options.go
Profile: behaviour.ProfileForChip(ChipForDevice(machine))
```

Direct `behaviour.Simulate` callers can pass `Profile: behaviour.ProfileForChip("T8122")` explicitly, or leave it zero to inherit `DefaultProfile`.

**Bursts / gait** (Typing and Walking activities) are overlaid on top of the idle baseline σ unchanged across chip families — a user typing on an iPhone X produces the same thumb-travel shape as one on an iPhone 17. Only the sensor noise floor varies.

These σ values are interim and will be recalibrated against real `behaviour_results` captures collected via the Frida script in `frida/capture.js`.

## FSM fidelity note — `userAcc` is NOT fabs

`-[SEONBehaviourProbe checkSuspiciousDeviceOrientation:]` at IDA `0x134f0` compares `userAcc.x` and `userAcc.y` **raw** against `0.15` (verified: `FCMP D1, 0.15; B.GT`, `qword_2D3C0 = 0.15`). The `gravity.z` / `gravity.y` / `rotationRate.z` checks elsewhere in the same function DO use `fabs`, but userAcc is unique. Negative-only userAcc motion must not trigger the at-ear FSM — pinned by `TestCheckSuspiciousFSMUserAccIsNotFabs` in `devices/behaviour/simulator_test.go`.

## Usage

### Simplest path (wire it to a Device)

```go
import "status403.com/rsavv/seonsdk-go/devices"
import "status403.com/rsavv/seonsdk-go/devices/behaviour"

dev, _ := devices.NewDevice(devices.DeviceOptions{
    Machine:    "iPhone16,2",
    IOSVersion: "18.2",
    // No default behaviour — toggle per call via Overrides.
})

// Address mutation: no behaviour_results in payload.
addressFP := dev.NewFingerprint()

// Login / phone signup: behaviour_results present.
loginFP := dev.NewFingerprint(devices.FingerprintOverrides{
    Behaviour: &devices.BehaviourOptions{
        Enabled:             true,
        Duration:            6 * time.Second,
        Activity:            behaviour.ActivityTyping,
        InitialHeading:      0,    // 0 = simulator picks random
        MagneticDeclination: 0.5,  // central UK ≈ +0.5° east
        MagFieldMagnitude:   48,   // UK indoor total field µT
    },
})
```

### Or a permanent device default

```go
dev, _ := devices.NewDevice(devices.DeviceOptions{
    // ...
    BehaviourMonitoring: &devices.BehaviourOptions{
        Enabled: true, Duration: 5 * time.Second,
    },
})

// Default call: with behaviour.
withBeh := dev.NewFingerprint()

// Force-disable for this one call (e.g. address mutation on same device):
withoutBeh := dev.NewFingerprint(devices.FingerprintOverrides{
    Behaviour: &devices.BehaviourOptions{Enabled: false},
})
```

### Directly, bypassing Device

```go
import "status403.com/rsavv/seonsdk-go/devices/behaviour"

rawResults := behaviour.Simulate(behaviour.SimulateOptions{
    Duration: 5 * time.Second,
    Seed:     42,
    Activity: behaviour.ActivityIdle,
})
// rawResults is map[string]interface{} with the 30 SDK keys.
```

## Activity levels

| Level | Use case |
|---|---|
| `behaviour.ActivityIdle` | User is reading the screen, not interacting. Default for "waiting for OTP" and similar idle windows. |
| `behaviour.ActivityTyping` | User is typing on the keyboard (phone number entry, password entry, search). Short motion bursts. |
| `behaviour.ActivityWalking` | User is walking while using the phone. Periodic vertical bouncing + sustained yaw wiggle. |

## Reproducibility

Pass a non-zero `Seed` for deterministic output. The same `(Seed, Duration, Activity, InitialHeading, MagneticDeclination, MagFieldMagnitude, EmitAtEar)` tuple produces byte-for-byte identical results across runs — useful for pinning tests and for comparing against real-device captures.

## IDA cross-reference

| Function | Address | Purpose |
|---|---:|---|
| `-[SEONBehaviourProbe bootstrap]` | `0x12520` | Inits buffers (1000-cap) and dict buffers (10/10/100-cap), bucketSize = 10. |
| `-[SEONBehaviourProbe startBehaviourMonitoring]` | `0x12718` | Re-inits buffers, sets `minimumTimeInterval=0.2`, `previousOrientation=-1`, fires CLHeading + CMMotion + CMMagnetometer. |
| `-[SEONBehaviourProbe startMonitoringMotion]` | `0x129bc` | `setDeviceMotionUpdateInterval: 0.1`. |
| motion update block | `0x12b18` | Per-sample work: updateAttitude + checkSuspicious + addValues. |
| `-[SEONBehaviourProbe startMonitoringMagnetometer]` | `0x12d94` | `setMagnetometerUpdateInterval: 0.1`. |
| magnetometer update block | `0x12e6c` | `addValue` magnitude from raw x/y/z. |
| `-[SEONBehaviourProbe stopBehaviourMonitoring]` | `0x1300c` | Builds the 9-top-level-keys dict, returns `.copy`. |
| `-[SEONBehaviourNumberBuffer *]` | `0xba50-0xc6e4` | Ring buffer + stats. |
| `-[SEONBehaviourDictionaryBuffer *]` | `0x1445c-0x150e0` | Bounded dict w/ FIFO eviction. |
| `checkSuspiciousDeviceOrientation:` | `0x133f4` | At-ear FSM + orientation change recording. |
| `locationManager:didUpdateHeading:` | `0x1394c` | Updates `magneticDeclination`; calls addHeadingData. |
| `addHeadingData:forDate:` | `0x13b04` | Inserts into headingData dict. |
| `updateMostFrequentAttitude:` | `0x13ce4` | Bucket-key counter; refreshes `mostFrequentAttitude`. |
| `bucketKeyForAttitude:` | `0x13e70` | `"%f,%f,%f,%f"` of rounded × bucketSize. |
| `attitudeFromBucketKey:` | `0x13ee4` | Reverses the bucket key to a quaternion. |
| `dictionaryFromQuaternion:` | `0x14034` | `{x, y, z, w}` dict. |
| `roundQuaternion:` | `0x141b8` | Round each component to 2 decimals. |
| `isAttitude:similarToAttitude:` | `0x14204` | `|dot| > 0.96`. |
| `calculatePhysicalDeviceOrientationWithGravity:` | `0x1423c` | 14-orient table lookup + double-add of gravity.z. |
| `calculateMagnitudeFromMagneticField:` | `0x14194` | `sqrt(x² + y² + z²)`. |
| `setMonitoringIsRunning:` | `0x12438` | Writes `monitoringDuration` global in ms. |
| 14-orient table | `unk_2D3F8` | `[int64 idx, double x, double y, double z]` × 14. |
| At-ear sentinels | `off_43D60 / off_43D78 / off_43D90` | NSConstantIntegerNumber 1 / 0 / -1. |
