package behaviour

import (
	"math"
	"testing"
)

func almostEqual(a, b float64) bool {
	if math.IsNaN(a) && math.IsNaN(b) {
		return true
	}
	return math.Abs(a-b) < 1e-9
}

// TestNumberBufferEmpty verifies the "empty buffer → 0.0 everywhere" quirk at
// IDA 0xbed0, 0xbfb0, 0xbd50, 0xc0bc, 0xc124.
func TestNumberBufferEmpty(t *testing.T) {
	b := NewNumberBuffer(1000)
	for _, got := range []float64{b.Min(), b.Max(), b.Mean(), b.Median(), b.Variance(), b.P95()} {
		if got != 0 {
			t.Fatalf("empty buffer got %v, want 0", got)
		}
	}
}

// TestNumberBufferSmallSet uses [1, 2, 3, 4, 5] — a canonical set where
// every stat has an easy hand-calculation.
func TestNumberBufferSmallSet(t *testing.T) {
	b := NewNumberBuffer(1000)
	for _, v := range []float64{1, 2, 3, 4, 5} {
		b.AddValue(v)
	}
	if !almostEqual(b.Min(), 1) {
		t.Errorf("min: got %v, want 1", b.Min())
	}
	if !almostEqual(b.Max(), 5) {
		t.Errorf("max: got %v, want 5", b.Max())
	}
	if !almostEqual(b.Mean(), 3) {
		t.Errorf("mean: got %v, want 3", b.Mean())
	}
	if !almostEqual(b.Median(), 3) {
		t.Errorf("median: got %v, want 3 (odd n)", b.Median())
	}
	// Population variance = E[X²] - (E[X])² = (1+4+9+16+25)/5 - 9 = 11 - 9 = 2
	if !almostEqual(b.Variance(), 2) {
		t.Errorf("variance: got %v, want 2", b.Variance())
	}
	// floor((5-1)*0.95) = floor(3.8) = 3 → sorted[3] = 4
	if !almostEqual(b.P95(), 4) {
		t.Errorf("p95: got %v, want 4", b.P95())
	}
}

// TestNumberBufferEvenMedian verifies (sorted[(n-1)/2] + sorted[n/2])/2 path.
func TestNumberBufferEvenMedian(t *testing.T) {
	b := NewNumberBuffer(1000)
	for _, v := range []float64{1, 2, 3, 4} {
		b.AddValue(v)
	}
	// (sorted[1] + sorted[2]) / 2 = (2 + 3) / 2 = 2.5
	if !almostEqual(b.Median(), 2.5) {
		t.Errorf("even median: got %v, want 2.5", b.Median())
	}
}

// TestNumberBufferVarianceSingleSample verifies the n<2 short-circuit.
func TestNumberBufferVarianceSingleSample(t *testing.T) {
	b := NewNumberBuffer(1000)
	b.AddValue(42)
	if b.Variance() != 0 {
		t.Fatalf("single-sample variance: got %v, want 0", b.Variance())
	}
}

// TestNumberBufferVarianceClamped verifies that floating-point rounding
// that would produce a tiny negative doesn't leak — the SDK clamps to 0
// (the `if (v4 <= 0.0)` check at 0xc104).
func TestNumberBufferVarianceClamped(t *testing.T) {
	b := NewNumberBuffer(1000)
	// Two identical values → true variance = 0, but running-sum version can
	// produce very small floats (±ε) depending on arithmetic order.
	b.AddValue(1e6)
	b.AddValue(1e6)
	if b.Variance() != 0 {
		t.Fatalf("identical-values variance: got %v, want 0 (clamped)", b.Variance())
	}
}

// TestNumberBufferP95Edge pins the exact index formula at 0xc124:
// `floor((n-1)*0.95)`. For n=20 → idx 18 (i.e. 2nd-largest), NOT 19.
func TestNumberBufferP95Edge(t *testing.T) {
	cases := []struct {
		n    int
		want int // expected 1-based "rank"
	}{
		{n: 1, want: 1},   // floor(0) = 0 → sorted[0]
		{n: 2, want: 1},   // floor(0.95) = 0 → sorted[0]
		{n: 19, want: 18}, // floor(18*0.95)=floor(17.1)=17 → sorted[17] = rank 18
		{n: 20, want: 19}, // floor(19*0.95)=floor(18.05)=18 → sorted[18] = rank 19
		{n: 21, want: 20}, // floor(20*0.95)=floor(19)=19 → sorted[19] = rank 20
	}
	for _, c := range cases {
		b := NewNumberBuffer(1000)
		for i := 1; i <= c.n; i++ {
			b.AddValue(float64(i))
		}
		if !almostEqual(b.P95(), float64(c.want)) {
			t.Errorf("p95(n=%d): got %v, want %v", c.n, b.P95(), c.want)
		}
	}
}

// TestNumberBufferRingOverwrite verifies the "once full, overwrite from
// currentIndex=0" path at 0xbc40-0xbc6c.
func TestNumberBufferRingOverwrite(t *testing.T) {
	b := NewNumberBuffer(3)
	for _, v := range []float64{10, 20, 30} {
		b.AddValue(v)
	}
	if !almostEqual(b.Mean(), 20) {
		t.Fatalf("pre-overwrite mean: got %v, want 20", b.Mean())
	}
	// This should overwrite index 0 (the "10").
	b.AddValue(40)
	// Buffer is now {40, 20, 30}; mean = 30.
	if !almostEqual(b.Mean(), 30) {
		t.Errorf("post-overwrite mean: got %v, want 30", b.Mean())
	}
	if !almostEqual(b.Min(), 20) {
		t.Errorf("post-overwrite min: got %v, want 20 (10 was evicted)", b.Min())
	}
	if !almostEqual(b.Max(), 40) {
		t.Errorf("post-overwrite max: got %v, want 40", b.Max())
	}
	if b.Count() != 3 {
		t.Errorf("count stays at capacity: got %d, want 3", b.Count())
	}
}

// TestNumberBufferResults verifies the key format from
// -[getResultsWithName:] at 0xc400: "min_X", "max_X", "mean_X", "median_X",
// "var_X", "p95_X".
func TestNumberBufferResults(t *testing.T) {
	b := NewNumberBuffer(1000)
	for _, v := range []float64{1, 2, 3, 4, 5} {
		b.AddValue(v)
	}
	r := b.Results("user_acc")
	wantKeys := []string{
		"min_user_acc", "max_user_acc", "mean_user_acc",
		"median_user_acc", "var_user_acc", "p95_user_acc",
	}
	if len(r) != len(wantKeys) {
		t.Fatalf("result key count: got %d, want %d", len(r), len(wantKeys))
	}
	for _, k := range wantKeys {
		if _, ok := r[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
}

// TestNumberBufferZeroCapacity verifies the "capacity==0" early-exit branch
// (arrayWithCapacity:0 path at 0xbae4).
func TestNumberBufferZeroCapacity(t *testing.T) {
	b := NewNumberBuffer(0)
	b.AddValue(42)
	b.AddValue(17)
	if b.Count() != 0 {
		t.Errorf("capacity-0 should swallow writes, got count=%d", b.Count())
	}
}
