package seonsdk

import (
	"fmt"
	"math/rand"
)

// iOS-device-realistic timing generators.
//
// Calibrated against a real iPhone 14 Pro / iOS 18.5 capture:
//
//	performance_network_call       = 1638.188  // DC roundtrip (VPN+proxy = slow)
//	performance_actual_processing  = 1670.555  // probe_time + network = ~32 + 1638
//	performance_exec_time          = 1672.511  // actual + ~2 ms serialise overhead
//	sum(performance_probes phases) ≈ 30 ms     // what actually ran locally
//
// The key insight from that capture: `performance_actual_processing` is
// dominated by the DC network roundtrip; probe scanning itself is only
// 20–50 ms on modern hardware. Earlier tuning over-estimated probe time by
// 30×, making `actual_processing` drift too high vs `network_call` — an
// iPhone-unlike signature.

// simulateProbeProcessingMs returns the locally-spent time for the nine
// SEONAbstractProbe scans plus experimental-merge work, i.e., everything
// between `nowInMilliseconds()` at constructReport start (0x1b930) and the
// DC dispatch at 0x1c2a4.
//
// Real capture shows ~30 ms. Most modern iPhones sit in the 20–60 ms band;
// older devices (A10/A11) push 40–80 ms due to slower sysctl paths.
func simulateProbeProcessingMs() float64 {
	return 20.0 + rand.Float64()*40.0
}

// simulateNetworkCallMs returns realistic DC network call time.
//
// Distribution tuned against the 20-fingerprint iPhone 15 / iOS 26.5 corpus
// (`legitfp/decoded.json`), which sits much tighter than earlier
// hand-tuned bands:
//
//	performance_network_call: min 382.5, max 555.6, mean 422.8, stdev 34.7
//
// We sample from a 380–560 ms band, biased toward the 400–450 ms median
// where the bulk of the corpus mass actually lives. The earlier wide
// distribution (100–2000 ms across three bands) was producing values that
// the live corpus never shows — itself a tell.
//
// Cap at 2000 ms: NSURLSession's timeoutIntervalForRequest is 2.0 s per
// IDA 0x7918. Anything above that would have timed out on a real device.
func simulateNetworkCallMs() float64 {
	r := rand.Float64()
	switch {
	case r < 0.65:
		// Bulk: 400–450 ms (median band).
		return 400.0 + rand.Float64()*50.0
	case r < 0.90:
		// Tail toward fast: 380–400 ms or fast-450–500 ms.
		if rand.Float64() < 0.5 {
			return 380.0 + rand.Float64()*20.0
		}
		return 450.0 + rand.Float64()*50.0
	default:
		// Slow tail: 500–560 ms (corpus max was 555.6).
		return 500.0 + rand.Float64()*60.0
	}
}

// simulateDCTimeoutMs returns a value around the real SDK's hard 2-second DC timeout
// (2000 ms fixed by setTimeoutIntervalForRequest/Resource at 0x7918). Use this when
// running server-side with SkipDC=true to match the pattern produced by genuinely
// unreachable DC endpoints: 2000.0 ± <1ms.
func simulateDCTimeoutMs() float64 {
	return 2000.0 + rand.Float64()*2.0 // 2000.0-2002.0 ms, matching observed samples
}

// simulateSerializeOverheadMs returns the gap between `actual_processing`
// and `exec_time` — the wall time between constructReport's finalize block
// and getFingerprintBase64's serialize block.
//
// Real capture: 1672.511 - 1670.555 = 1.956 ms. Observed typical range is
// 1-5 ms; the only work in that gap is the NSJSONSerialization options=3
// pass plus AES encryption of the resulting data.
func simulateSerializeOverheadMs() float64 {
	return 1.0 + rand.Float64()*4.0
}

// formatPerfMs renders a millisecond count using the exact format that
// `+[SEONUtil formatMilliseconds:]` at IDA 0x5b6c produces — `"%.3f"` with
// NO trailing " ms" unit.
func formatPerfMs(ms float64) string {
	return fmt.Sprintf("%.3f", ms)
}

// maxRealisticNetworkMs is the upper bound the real SDK can ever emit for
// `performance_network_call`. IDA 0x7918 sets
// `setTimeoutIntervalForRequest/Resource = 2.0`, so NSURLSession hard-stops
// the DC call at 2000ms. Observed real-device timeouts are 2000–2002ms.
// Anything we report above this floor is iOS-impossible — SEON's backend
// will flag. We clamp measured network time down to the timeout value if
// it exceeds this bound (which can happen when SEON slow-plays banned
// requests and our HTTP stack is more forgiving than NSURLSession).
const maxRealisticNetworkMs = 2002.0

// resolvePerformanceTiming computes the three performance_* fields.
// If the user provided explicit values, those are honored; otherwise realistic
// iOS-device-like timings are simulated.
//
// actualNetworkMs: real DC call time if measured (0 if SkipDC or failure).
// Values > maxRealisticNetworkMs are clamped to a timeout-range value, so
// we never emit a wall-clock that a real iPhone couldn't produce.
//
// Returns (actual, exec, network) as formatted strings.
func resolvePerformanceTiming(cfg *FingerprintConfig, actualNetworkMs float64) (string, string, string) {
	networkMs := actualNetworkMs
	if networkMs < 1.0 {
		if cfg.SkipDC && cfg.SimulateDCTimeout {
			networkMs = simulateDCTimeoutMs()
		} else {
			networkMs = simulateNetworkCallMs()
		}
	} else if networkMs > maxRealisticNetworkMs {
		// DC took longer than a real iPhone would ever wait — the
		// NSURLSession 2s timeout would have fired. Report a timeout-range
		// value so the payload matches what iOS would have emitted.
		networkMs = simulateDCTimeoutMs()
	}

	probeMs := simulateProbeProcessingMs()
	actualMs := probeMs + networkMs
	execMs := actualMs + simulateSerializeOverheadMs()

	perfActual := cfg.PerformanceActualProcessing
	if perfActual == "" {
		perfActual = formatPerfMs(actualMs)
	}

	perfNetwork := cfg.PerformanceNetworkCall
	if perfNetwork == "" {
		perfNetwork = formatPerfMs(networkMs)
	}

	perfExec := cfg.PerformanceExecTime
	if perfExec == "" {
		perfExec = formatPerfMs(execMs)
	}

	return perfActual, perfExec, perfNetwork
}
