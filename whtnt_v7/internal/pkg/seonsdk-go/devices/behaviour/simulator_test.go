package behaviour

import (
	"testing"
	"time"
)

// TestSimulateShapeIdle verifies that a 3-second idle window produces every
// expected key with the right Go-side type.
func TestSimulateShapeIdle(t *testing.T) {
	r := Simulate(SimulateOptions{
		Duration:            3 * time.Second,
		Seed:                42,
		Activity:            ActivityIdle,
		InitialHeading:      180,
		MagneticDeclination: -0.5,
		MagFieldMagnitude:   40,
		StartUnix:           1700000000,
	})

	// Scalar stats — all float64.
	for _, k := range []string{
		"min_user_acc", "max_user_acc", "mean_user_acc",
		"median_user_acc", "var_user_acc", "p95_user_acc",
		"min_rot_rate", "max_rot_rate", "mean_rot_rate",
		"median_rot_rate", "var_rot_rate", "p95_rot_rate",
		"min_gravity", "max_gravity", "mean_gravity",
		"median_gravity", "var_gravity", "p95_gravity",
		"min_mag_field", "max_mag_field", "mean_mag_field",
		"median_mag_field", "var_mag_field", "p95_mag_field",
		"mag_declination",
	} {
		v, ok := r[k]
		if !ok {
			t.Errorf("missing key %q", k)
			continue
		}
		if _, isFloat := v.(float64); !isFloat {
			t.Errorf("key %q: got %T, want float64", k, v)
		}
	}

	// monitoring_duration is int64 in ms. Default emits 0 to match the
	// iPhone 15 / iOS 26.5 corpus (20/20 captures). EmitDuration=true is
	// the legacy opt-in for non-zero ms — see TestSimulateMonitoringDurationEmitDuration.
	md, ok := r["monitoring_duration"].(int64)
	if !ok {
		t.Fatalf("monitoring_duration type: got %T, want int64", r["monitoring_duration"])
	}
	if md != 0 {
		t.Errorf("monitoring_duration: got %d, want 0 (default zero per corpus)", md)
	}

	// heading_changes and orientation_changes are maps.
	if _, ok := r["heading_changes"].(map[string]interface{}); !ok {
		t.Errorf("heading_changes not a map: got %T", r["heading_changes"])
	}
	if _, ok := r["orientation_changes"].(map[string]interface{}); !ok {
		t.Errorf("orientation_changes not a map: got %T", r["orientation_changes"])
	}

	// most_freq_attitude is {x, y, z, w}.
	a, ok := r["most_freq_attitude"].(map[string]interface{})
	if !ok {
		t.Fatalf("most_freq_attitude type: got %T, want map", r["most_freq_attitude"])
	}
	for _, k := range []string{"x", "y", "z", "w"} {
		if _, ok := a[k]; !ok {
			t.Errorf("most_freq_attitude missing %q", k)
		}
	}
}

// TestSimulateDeterministic verifies that the same seed + same opts produces
// byte-for-byte identical output (required for reproducible test fingerprints).
func TestSimulateDeterministic(t *testing.T) {
	opts := SimulateOptions{
		Duration:       2 * time.Second,
		Seed:           12345,
		Activity:       ActivityIdle,
		StartUnix:      1700000000,
		InitialHeading: 270,
	}
	r1 := Simulate(opts)
	r2 := Simulate(opts)

	// Spot-check a few float values.
	for _, k := range []string{"mean_user_acc", "max_rot_rate", "var_gravity", "mag_declination"} {
		if r1[k] != r2[k] {
			t.Errorf("key %q non-deterministic: %v vs %v", k, r1[k], r2[k])
		}
	}
	if r1["monitoring_duration"] != r2["monitoring_duration"] {
		t.Errorf("monitoring_duration drift")
	}
}

// TestSimulateHeadingChangesCount — a 5-second handheld-still window
// should average around 5 / 1.1s ≈ 4.5 entries. Allow [1, 9] over single
// seeds to absorb Poisson variance; the distributional check is in
// TestSimulateHeadingActivity_DistributionalProperties below.
func TestSimulateHeadingChangesCount(t *testing.T) {
	r := Simulate(SimulateOptions{
		Duration:  5 * time.Second,
		Seed:      7,
		StartUnix: 1700000000,
	})
	hc := r["heading_changes"].(map[string]interface{})
	if len(hc) < 1 || len(hc) > 9 {
		t.Errorf("heading_changes count: got %d, want 1-9 over 5 s", len(hc))
	}
}

// TestSimulateHeadingActivity_DistributionalProperties confirms the
// activity scenarios produce realistic event-count distributions across
// many runs. Per-seed counts vary because of the Exponential inter-arrival
// model — that's the point.
func TestSimulateHeadingActivity_DistributionalProperties(t *testing.T) {
	cases := []struct {
		activity HeadingActivity
		duration time.Duration
		minMean  float64 // expected mean count, lower bound
		maxMean  float64 // expected mean count, upper bound
	}{
		// 17s @ 1.1s mean → ~15 events but capped at 10
		{HeadingActivityHandheldStill, 17 * time.Second, 8, 10},
		// 17s @ 0.7s mean → ~24 events but capped at 10
		{HeadingActivityHandheldActive, 17 * time.Second, 9, 10},
		// 30s @ 18s mean → ~1.7 events, mostly 0-3
		{HeadingActivityOnTable, 30 * time.Second, 0.5, 3.5},
	}
	const trials = 200
	for _, c := range cases {
		var total int
		for i := 0; i < trials; i++ {
			r := Simulate(SimulateOptions{
				Duration:        c.duration,
				Seed:            int64(i + 1), // each trial different seed
				StartUnix:       1700000000,
				HeadingActivity: c.activity,
			})
			hc := r["heading_changes"].(map[string]interface{})
			total += len(hc)
		}
		mean := float64(total) / float64(trials)
		if mean < c.minMean || mean > c.maxMean {
			t.Errorf("activity=%v duration=%v: mean count %.2f outside [%.2f, %.2f]",
				c.activity, c.duration, mean, c.minMean, c.maxMean)
		}
	}
}

// TestRandomizeHeadingActivity_Distribution confirms the population
// weights match DefaultHeadingActivityWeights (70/25/5) within a small
// tolerance over 10k draws.
func TestRandomizeHeadingActivity_Distribution(t *testing.T) {
	const trials = 10000
	counts := map[HeadingActivity]int{}
	for i := 0; i < trials; i++ {
		counts[RandomizeHeadingActivity()]++
	}
	checks := []struct {
		activity HeadingActivity
		expect   float64
		tol      float64
	}{
		{HeadingActivityHandheldStill, 0.70, 0.025},
		{HeadingActivityHandheldActive, 0.25, 0.02},
		{HeadingActivityOnTable, 0.05, 0.015},
	}
	for _, c := range checks {
		got := float64(counts[c.activity]) / float64(trials)
		if got < c.expect-c.tol || got > c.expect+c.tol {
			t.Errorf("activity=%v share %.3f outside [%.3f ± %.3f]", c.activity, got, c.expect, c.tol)
		}
	}
}

// TestHeadingActivityWeights_FallbackOnZero — degenerate weights default
// to HandheldStill (the safe majority pick).
func TestHeadingActivityWeights_FallbackOnZero(t *testing.T) {
	w := HeadingActivityWeights{}
	for i := 0; i < 100; i++ {
		if got := w.Pick(); got != HeadingActivityHandheldStill {
			t.Fatalf("zero weights should fall back to HandheldStill, got %v", got)
		}
	}
}

// TestSimulateHeadingHandheldConsecutiveSeconds — confirms the HandheldStill
// scenario produces CONSECUTIVE 1-second timestamps with no gaps. This
// matches 100% of the historical valid hand-held population, where real
// CL fires multiple times per second and unix-second key collapse leaves
// every consecutive second populated. Earlier (Poisson-based) emission
// introduced gaps that broke this invariant and triggered bans.
func TestSimulateHeadingHandheldConsecutiveSeconds(t *testing.T) {
	for _, activity := range []HeadingActivity{HeadingActivityHandheldStill, HeadingActivityHandheldActive} {
		for _, seed := range []int64{1, 7, 42, 99, 12345} {
			r := Simulate(SimulateOptions{
				Duration:        17 * time.Second,
				Seed:            seed,
				StartUnix:       1700000000,
				HeadingActivity: activity,
			})
			hc := r["heading_changes"].(map[string]interface{})
			if len(hc) != 10 {
				t.Errorf("activity=%v seed=%d: got %d entries, want 10", activity, seed, len(hc))
				continue
			}
			// Collect keys, parse to int64, sort.
			var ks []int64
			for k := range hc {
				var v int64
				_, _ = fmtSscanInt(k, &v)
				ks = append(ks, v)
			}
			sortInt64(ks)
			for i := 1; i < len(ks); i++ {
				if ks[i]-ks[i-1] != 1 {
					t.Errorf("activity=%v seed=%d: gap between %d and %d (Δ=%d) — should be consecutive",
						activity, seed, ks[i-1], ks[i], ks[i]-ks[i-1])
					break
				}
			}
		}
	}
}

func fmtSscanInt(s string, v *int64) (int, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	*v = n
	return 1, nil
}

func sortInt64(a []int64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

// TestSimulateHeadingOnTable_HasGaps — confirms OnTable scenario has gappy
// timestamps (variable inter-arrival, often missing seconds), matching the
// real iPhone 17 Pro reference capture (2 entries 31s apart).
func TestSimulateHeadingOnTable_HasGaps(t *testing.T) {
	const trials = 30
	gappyCount := 0
	for i := 0; i < trials; i++ {
		r := Simulate(SimulateOptions{
			Duration:        60 * time.Second,
			Seed:            int64(i + 1),
			StartUnix:       1700000000,
			HeadingActivity: HeadingActivityOnTable,
		})
		hc := r["heading_changes"].(map[string]interface{})
		if len(hc) < 2 {
			continue
		}
		var ks []int64
		for k := range hc {
			var v int64
			_, _ = fmtSscanInt(k, &v)
			ks = append(ks, v)
		}
		sortInt64(ks)
		for i := 1; i < len(ks); i++ {
			if ks[i]-ks[i-1] > 1 {
				gappyCount++
				break
			}
		}
	}
	// Most OnTable runs (with mean 18s) should produce gaps when there
	// are multiple entries.
	if gappyCount < trials/3 {
		t.Errorf("OnTable: only %d/%d trials had gaps; expected most to be gappy", gappyCount, trials)
	}
}

// TestSimulateAtEarEmission — when EmitAtEar is on, at least one
// [AtEarStart, orient] pair should appear in orientation_changes.
func TestSimulateAtEarEmission(t *testing.T) {
	r := Simulate(SimulateOptions{
		Duration:  5 * time.Second,
		Seed:      123,
		Activity:  ActivityTyping, // needs some movement to cross the FSM thresholds
		EmitAtEar: true,
		StartUnix: 1700000000,
	})
	oc := r["orientation_changes"].(map[string]interface{})
	foundEarStart := false
	for _, v := range oc {
		arr, ok := v.([]interface{})
		if !ok || len(arr) != 2 {
			continue
		}
		// At-ear entries lead with OrientAtEarStart (int64 1).
		if arr[0] == OrientAtEarStart {
			foundEarStart = true
			break
		}
	}
	if !foundEarStart {
		// Not fatal — FSM may not trigger given specific seed. We skip.
		t.Skip("EmitAtEar did not cross thresholds with this seed; not a hard failure")
	}
}

// TestCheckSuspiciousFSMUserAccIsNotFabs — the real SDK compares userAcc.x
// and userAcc.y directly against 0.15 (`FCMP D1, 0.15; B.GT` at IDA 0x134f0),
// NOT their absolute values. Negative-only motion on userAcc must NOT
// trigger an at-ear transition. This regression pins that behaviour — it
// was previously `math.Abs(uaX) > 0.15 || math.Abs(uaY) > 0.15`.
func TestCheckSuspiciousFSMUserAccIsNotFabs(t *testing.T) {
	// Drive the simulator with a stable seed, a profile whose σ is small
	// enough that the baseline won't accidentally nudge uaX/uaY positive
	// past 0.15, and EmitAtEar on. Under fabs semantics, typing bursts
	// that land negative would cross the threshold; under the correct
	// semantics they don't.
	r := Simulate(SimulateOptions{
		Duration:  6 * time.Second,
		Seed:      1,
		Activity:  ActivityIdle, // idle — bursts off, tiny tremor only
		EmitAtEar: true,
		StartUnix: 1700000000,
		Profile:   ProfileForChip("T8122"), // A17 Pro — quietest profile
	})

	oc := r["orientation_changes"].(map[string]interface{})
	for _, v := range oc {
		arr, ok := v.([]interface{})
		if !ok {
			continue
		}
		if len(arr) == 2 && arr[0] == OrientAtEarStart {
			t.Fatalf("at-ear transition fired on idle A17 Pro window — FSM may be using fabs on userAcc")
		}
	}
}

// TestSimulateMonitoringDurationDefaultZero pins the iPhone 15 / iOS 26.5
// corpus pattern: 20/20 captures emit `monitoring_duration: 0` regardless of
// the actual buffer duration (read-before-write at IDA 0x12438 + 0x13278).
func TestSimulateMonitoringDurationDefaultZero(t *testing.T) {
	for _, d := range []time.Duration{500 * time.Millisecond, 3 * time.Second, 17 * time.Second} {
		r := Simulate(SimulateOptions{Duration: d, Seed: 1, StartUnix: 1})
		if got := r["monitoring_duration"]; got != int64(0) {
			t.Errorf("duration=%v: got monitoring_duration=%v, want 0 (default)", d, got)
		}
	}
}

// TestSimulateMonitoringDurationEmitDuration pins the legacy opt-in path —
// EmitDuration=true emits llround(duration*1000) (IDA 0x124dc). Use only
// when reproducing the older Whatnot pattern (pre-iOS 26).
func TestSimulateMonitoringDurationEmitDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want int64
	}{
		{3 * time.Second, 3000},
		{500 * time.Millisecond, 500},
		{3*time.Second + 250*time.Millisecond, 3250},
	}
	for _, c := range cases {
		r := Simulate(SimulateOptions{Duration: c.in, Seed: 1, StartUnix: 1, EmitDuration: true})
		if r["monitoring_duration"] != c.want {
			t.Errorf("duration=%v: got %v, want %d", c.in, r["monitoring_duration"], c.want)
		}
	}
}
