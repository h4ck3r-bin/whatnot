package payload

import (
	"fmt"
	"math/rand"
)

// PerformanceProbes synthesises the top-level `performance_probes` dict that
// `-[SEONFingerprint setProbePerformances:]` (IDA 0x1c8d0) emits.
//
// Shape:
//
//	{
//	  "0" : {"0" : "<init_ms>", "1" : "<scan_ms>",   "2" : "<experimental_ms>"},   // device
//	  "1" : {... hardware ...},
//	  "2" : {... locale    ...},
//	  "3" : {... media     ...},
//	  "4" : {... network   ...},
//	  "5" : {... os        ...},
//	  "6" : {... telephony ...},
//	  "7" : {... accessibility ...},
//	  "8" : {... authentication ...},
//	  "9" : {... geolocation  ...}
//	}
//
// Each value is `+[SEONUtil formatMilliseconds:]` output ("%.3f") or `null`
// when the corresponding phase never ran. Per IDA 0xb270 / 0xb2cc the null
// sentinel fires when the stored timestamp is negative (uninitialised).
//
// Calibration source: 20-fingerprint iPhone 15 / iOS 26.5 corpus
// (`legitfp/decoded.json`). Per-probe min/max/mean (ms) for each slot:
//
//	  probe        | slot 0 (init)              | slot 1 (scan)              | slot 2 (exp)
//	  0 device     | 0.012–0.270  μ 0.062       | 6.080–320.564 μ 23.545     | 0.009–0.094 μ 0.043
//	  1 hardware   | 0.000        μ 0.000       | 0.020–0.095   μ 0.040      | 0.007–0.011 μ 0.008
//	  2 locale     | 0.000–0.001  μ 0.000       | 0.139–0.747   μ 0.272      | 0.053–0.086 μ 0.062
//	  3 media      | 0.000        μ 0.000       | 0.309–15.916  μ 3.359      | 0.260–0.825 μ 0.466
//	  4 network    | 0.001–0.004  μ 0.002       | 0.966–4.173   μ 1.611      | 0.431–2.031 μ 0.585
//	  5 os         | 0.008–0.025  μ 0.010       | 7.861–15.243  μ 10.107     | 0.036–0.080 μ 0.043
//	  6 telephony  | 0.008–0.119  μ 0.032       | 0.216–0.685   μ 0.321      | 0.802–2.549 μ 1.115
//	  7 accessib.  | 0.000        μ 0.000       | 0.064–25.072  μ 8.463      | 0.000       μ 0.000
//	  8 auth       | 1.057–7.052  μ 1.964       | 3.732–38.396  μ 8.761      | 0.002–0.242 μ 0.017
//	  9 geolocate  | 0.001        μ 0.001       | null (geo disabled)        | null (geo disabled)
//
// Earlier ranges were tuned against ONE iPhone 14 Pro / iOS 18.5 capture —
// those values are off by 2–4× for several probes vs the 20-sample iPhone 15
// dataset. Notable corrections:
//
//   - Probe 8 slot 2 is NOT null on iOS 26.5 — every capture has 0.002–0.242
//     ms. Previous code emitted null and (correctly per its own model)
//     dropped supported_biometry_type, but the corpus shows BOTH the timing
//     and the biometry type are populated.
//   - Probe 3 (media) scan tails to ~16 ms, well above the previous 1.8 ms cap.
//   - Probe 7 (accessibility) scan tails to ~25 ms, far above the 6 ms cap.
//   - Probe 8 auth scan tails to ~38 ms (cold LAContext), above the 6 ms cap.
//   - Probes 1/2/3/7 init are observably exactly 0 (formatted "0.000"), not
//     a small non-zero — the SDK's `nearZero` band put a non-zero floor on
//     these and produced "0.001"–"0.008" where reality is "0.000".
type probePhases struct {
	initMs         float64 // -1 = not run
	scanMs         float64 // -1 = not run
	experimentalMs float64 // -1 = not run
}

const probeNotRun = -1.0

// realisticProbePhases picks a set of timings representative of what a real
// iPhone 15 / iOS 26.5 emits for each probe. Values in milliseconds.
//
// Distributions are biased toward the corpus median with an occasional
// long-tail draw (15% chance) to reproduce the "first probe slow-path"
// outliers visible in the corpus (probe 0 scan up to 320 ms, probe 3 scan
// up to 16 ms, probe 8 scan up to 38 ms).
func realisticProbePhases(r *rand.Rand) [10]probePhases {
	// uniform draws a uniformly-distributed value in [lo, hi].
	uniform := func(lo, hi float64) float64 { return lo + r.Float64()*(hi-lo) }

	// rightSkewed draws from [lo, hi] with a mean biased toward lo (the
	// distribution most probes follow in the corpus: most values near the
	// median, some pulling the upper tail). Implemented via two uniform
	// draws and taking the minimum, which gives a triangular-ish density
	// peaked at lo.
	rightSkewed := func(lo, hi float64) float64 {
		a := lo + r.Float64()*(hi-lo)
		b := lo + r.Float64()*(hi-lo)
		if a < b {
			return a
		}
		return b
	}

	// tail draws from [lo, hi] most of the time but with `tailProb` chance
	// hits the [hi, tailHi] band — used for probes whose scan slot has a
	// rare cold-start spike in the corpus.
	tail := func(lo, hi, tailHi, tailProb float64) float64 {
		if r.Float64() < tailProb {
			return hi + r.Float64()*(tailHi-hi)
		}
		return lo + r.Float64()*(hi-lo)
	}

	return [10]probePhases{
		// 0: device — UIScreen + pasteboard detect; scan often slow.
		//   corpus init 0.012–0.270  μ 0.062
		//   corpus scan 6.080–320.564 μ 23.545 (one cold-start outlier at 320)
		//   corpus exp  0.009–0.094  μ 0.043
		{uniform(0.012, 0.080), tail(6.0, 30.0, 320.0, 0.07), uniform(0.009, 0.060)},
		// 1: hardware — corpus shows exact 0 init/exp.
		//   corpus init 0.000        (literal)
		//   corpus scan 0.020–0.095  μ 0.040
		//   corpus exp  0.007–0.011  μ 0.008
		{0, uniform(0.020, 0.095), uniform(0.007, 0.011)},
		// 2: locale — NSLocale property access; init effectively 0.
		//   corpus init 0.000–0.001  μ 0.000
		//   corpus scan 0.139–0.747  μ 0.272
		//   corpus exp  0.053–0.086  μ 0.062
		{0, uniform(0.139, 0.500), uniform(0.053, 0.086)},
		// 3: media — AVAudioSession; scan has long cold-start tail to ~16 ms.
		//   corpus init 0.000        (literal)
		//   corpus scan 0.309–15.916 μ 3.359
		//   corpus exp  0.260–0.825  μ 0.466
		{0, tail(0.30, 4.0, 16.0, 0.10), uniform(0.260, 0.825)},
		// 4: network — NWPathMonitor + interface list. Right-skewed scan/exp.
		//   corpus init 0.001–0.004  μ 0.002
		//   corpus scan 0.966–4.173  μ 1.611
		//   corpus exp  0.431–2.031  μ 0.585
		{uniform(0.001, 0.004), rightSkewed(0.966, 4.173), rightSkewed(0.431, 2.031)},
		// 5: os — sysctl chain; scan consistently slow ~10 ms.
		//   corpus init 0.008–0.025  μ 0.010
		//   corpus scan 7.861–15.243 μ 10.107
		//   corpus exp  0.036–0.080  μ 0.043
		{uniform(0.008, 0.025), uniform(7.861, 15.243), uniform(0.036, 0.080)},
		// 6: telephony — CTTelephonyNetworkInfo + cellular-data check.
		//   corpus init 0.008–0.119  μ 0.032   (right-skewed)
		//   corpus scan 0.216–0.685  μ 0.321   (right-skewed)
		//   corpus exp  0.802–2.549  μ 1.115   (right-skewed)
		{rightSkewed(0.008, 0.119), rightSkewed(0.216, 0.685), rightSkewed(0.802, 2.549)},
		// 7: accessibility — corpus init/exp exact 0; scan very wide,
		//                    spans 0.064–25 ms with no clear central tendency.
		//                    Use full uniform — both fast (UIAccessibility
		//                    cache hit) and slow (cold-state dump) paths
		//                    occur in the corpus.
		//   corpus init 0.000        (literal)
		//   corpus scan 0.064–25.072 μ 8.463
		//   corpus exp  0.000        (literal)
		{0, uniform(0.064, 25.072), 0},
		// 8: authentication — LAContext init slow; scan has long tail to 38 ms;
		//                     experimental IS populated (was null in older code).
		//                     init/exp are right-skewed in the corpus.
		//   corpus init 1.057–7.052  μ 1.964
		//   corpus scan 3.732–38.396 μ 8.761
		//   corpus exp  0.002–0.242  μ 0.017
		{rightSkewed(1.057, 7.052), tail(3.7, 12.0, 38.0, 0.20), rightSkewed(0.002, 0.060)},
		// 9: geolocation — init only when geo disabled (the dominant case).
		//   corpus init 0.001        (literal)
		//   corpus scan null
		//   corpus exp  null
		{0.001, probeNotRun, probeNotRun},
	}
}

// BuildPerformanceProbes returns the nested dict that becomes
// `performance_probes`. `geolocationEnabled` toggles probe 9's scan/exp;
// every other probe is always populated.
func BuildPerformanceProbes(r *rand.Rand, geolocationEnabled bool) map[string]interface{} {
	if r == nil {
		// Fresh, time-seeded RNG per call. Using `rand.NewSource(0)` here
		// (the previous behaviour) made every fingerprint emit byte-
		// identical `performance_probes` timings — a 100 % ban signature
		// once SEON's backend saw a handful of our emissions. Seed from
		// the concurrency-safe global source instead so each call samples
		// independently.
		r = rand.New(rand.NewSource(rand.Int63()))
	}
	phases := realisticProbePhases(r)

	if geolocationEnabled {
		// Geo scan + experimental populated when enabled. Typical 0.5–2 ms
		// for scan (CLLocationManager state read) and 0.3–1.2 ms for exp.
		phases[9] = probePhases{
			initMs:         phases[9].initMs,
			scanMs:         0.5 + r.Float64()*1.5,
			experimentalMs: 0.3 + r.Float64()*1.2,
		}
	}

	out := make(map[string]interface{}, 10)
	for i, p := range phases {
		entry := map[string]interface{}{
			"0": probePhaseValue(p.initMs),
			"1": probePhaseValue(p.scanMs),
			"2": probePhaseValue(p.experimentalMs),
		}
		out[fmt.Sprintf("%d", i)] = entry
	}
	return out
}

// probePhaseValue renders one phase — formatMilliseconds("%.3f") when
// populated, JSON null when the phase didn't run (sentinel = -1).
func probePhaseValue(ms float64) interface{} {
	if ms < 0 {
		return nil
	}
	return fmt.Sprintf("%.3f", ms)
}
