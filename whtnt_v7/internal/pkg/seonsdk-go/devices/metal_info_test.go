package devices

import (
	"math"
	"testing"
)

// ulp returns the distance from x to the next representable float64. Go has
// no math.Ulp, so derive it from Nextafter.
func ulp(x float64) float64 {
	return math.Nextafter(x, math.Inf(1)) - x
}

// onClockGrid reports whether durationMs could have come from subtracting two
// CACurrentMediaTime() doubles taken at the given uptime — i.e. whether it is
// an exact integer multiple of ulp(uptime).
func onClockGrid(durationMs float64, uptimeSec int64) (ticks float64, ok bool) {
	ticks = (durationMs / 1000) / ulp(float64(uptimeSec))
	return ticks, math.Abs(ticks-math.Round(ticks)) < 1e-6
}

// TestCorpusMetalDurationIsOnClockGrid verifies the property against the real
// capture before asserting our generator reproduces it. uptime 303620 s and
// duration 0.065922737121582031 ms come from testdata/freshwhatnot.
func TestCorpusMetalDurationIsOnClockGrid(t *testing.T) {
	const (
		corpusDurationMs = 0.065922737121582031
		corpusUptimeSec  = int64(303620)
		wantTicks        = 1132544.0
	)

	ticks, ok := onClockGrid(corpusDurationMs, corpusUptimeSec)
	if !ok {
		t.Fatalf("corpus duration %v is off-grid at uptime %d (ticks=%v) — "+
			"the model behind quantizeProbeDurationMs is wrong",
			corpusDurationMs, corpusUptimeSec, ticks)
	}
	if ticks != wantTicks {
		t.Errorf("corpus ticks = %v, want %v", ticks, wantTicks)
	}
}

// TestQuantizeProbeDurationMsLandsOnGrid checks the quantiser across a spread
// of uptimes, since ulp(uptime) changes with the exponent of the uptime.
func TestQuantizeProbeDurationMsLandsOnGrid(t *testing.T) {
	uptimes := []int64{
		3600,   // 1 hour
		83988,  // ~1 day
		303620, // the corpus device
		604800, // 1 week
	}
	rawSeconds := []float64{0.00003, 0.0000659, 0.00018, 0.0005, 0.0045}

	for _, uptime := range uptimes {
		for _, raw := range rawSeconds {
			got := quantizeProbeDurationMs(raw, uptime)
			if ticks, ok := onClockGrid(got, uptime); !ok {
				t.Errorf("uptime=%d raw=%v: quantised %v is off-grid (ticks=%v)",
					uptime, raw, got, ticks)
			}
			// Quantisation must not move the value meaningfully — the grid is
			// far finer than the durations involved.
			if rel := math.Abs(got-raw*1000) / (raw * 1000); rel > 1e-6 {
				t.Errorf("uptime=%d raw=%v: quantised %v drifted %.3g from %v",
					uptime, raw, got, rel, raw*1000)
			}
		}
	}
}

// TestQuantizeProbeDurationMsHandlesUnsetUptime pins the fallback so an unset
// uptime cannot silently produce an off-grid value.
func TestQuantizeProbeDurationMsHandlesUnsetUptime(t *testing.T) {
	for _, uptime := range []int64{0, -1} {
		got := quantizeProbeDurationMs(0.0000659, uptime)
		if _, ok := onClockGrid(got, 86400); !ok {
			t.Errorf("uptime=%d: %v is not on the fallback (86400 s) grid", uptime, got)
		}
	}
}

// TestBuildMetalInfoDurationOnGrid exercises the real entry point end to end.
func TestBuildMetalInfoDurationOnGrid(t *testing.T) {
	const uptime = int64(303620)

	for _, machine := range []string{"iPhone18,2", "iPhone16,2", "iPhone11,8"} {
		spec := LookupSpec(machine)
		if spec == nil {
			t.Fatalf("%s missing from DeviceSpecs", machine)
		}
		for i := 0; i < 50; i++ {
			info := BuildMetalInfo(machine, spec.PhysicalMemory, uptime)
			if info == nil {
				t.Fatalf("%s: BuildMetalInfo returned nil", machine)
			}
			d, isFloat := info["duration_ms"].(float64)
			if !isFloat {
				t.Fatalf("%s: duration_ms is %T, want float64", machine, info["duration_ms"])
			}
			if d <= 0 {
				t.Fatalf("%s: duration_ms = %v, want > 0", machine, d)
			}
			if ticks, ok := onClockGrid(d, uptime); !ok {
				t.Fatalf("%s iter %d: duration_ms %v off-grid (ticks=%v)", machine, i, d, ticks)
			}
		}
	}
}
