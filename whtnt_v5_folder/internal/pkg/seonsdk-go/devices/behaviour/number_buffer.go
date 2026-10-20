// Package behaviour reproduces SEONBehaviourProbe's sensor-aggregation pipeline.
// Every type in this package is pinned byte-for-byte to a SEON SDK 5.6.3 class
// decompiled from IDA; deviations are called out inline with the IDA address.
package behaviour

import (
	"math"
	"sort"
)

// NumberBuffer mirrors -[SEONBehaviourNumberBuffer …] (IDA 0xba50–0xc6e4).
//
// It's a circular buffer of float64 samples with a running sum / sum-of-squares /
// mean maintained on every add. Stats helpers (min, max, mean, median, variance,
// p95) reproduce Apple's quirks exactly:
//
//   - Empty buffer → all stats return 0.0 (verified 0xbed0 / 0xbfb0 / …).
//   - Variance is the *population* formula, `sumSqr/n - mean²`, clamped ≥ 0,
//     and returns 0 when n < 2 (verified 0xc0bc).
//   - p95 = sorted[floor((n-1)*0.95)] — integer truncation, not interpolation
//     (verified 0xc124 `vcvtmd_u64_f64`).
//   - Median is the mean-of-middle-two for even n (verified 0xbd50).
//   - Ring overwrite starts at index 0 once count == capacity; `sum` / `sumSqr`
//     / `mean` are updated incrementally by the outgoing sample (verified 0xbb34).
//
// Thread-safety: the real SDK protects with a serial dispatch queue. For our
// single-goroutine simulator this isn't needed.
type NumberBuffer struct {
	capacity      int
	buf           []float64 // linear storage, len grows up to capacity
	currentIndex  int       // next overwrite slot once buf is full
	count         int       // <= capacity
	sum           float64
	sumSqr        float64
	mean          float64
	sortedCache   []float64
	sortedValid   bool
}

// NewNumberBuffer matches -[initWithCapacity:] at 0xba50.
// Capacity == 0 produces a buffer that silently swallows writes (the SDK does
// the same — the `arrayWithCapacity` fast path is skipped).
func NewNumberBuffer(capacity int) *NumberBuffer {
	if capacity < 0 {
		capacity = 0
	}
	return &NumberBuffer{
		capacity:    capacity,
		buf:         make([]float64, 0, capacity),
		sortedValid: true,
	}
}

// AddValue mirrors -[addValue:] at 0xbb34.
//
// Caller is responsible for passing a value whose precision matches what
// CoreMotion / CLHeading would emit at this point in the pipeline:
//
//   - Single-axis scalars (gravity.z, heading degrees) come from the
//     hardware as float32 widened to double; pre-quantize before
//     AddValue so the sample buffer holds float32-precise doubles.
//   - 3-axis magnitudes (sqrt(x²+y²+z²) for userAcc / rotRate / magField)
//     are computed in float64 and pass through without quantization —
//     real captures confirm these stats carry full float64 precision.
//
// This split matches how Apple's CoreMotion produces each value and how
// a real iPhone's SEONBehaviourNumberBuffer would ingest it.
func (b *NumberBuffer) AddValue(v float64) {
	if b.capacity == 0 {
		return
	}
	if b.count < b.capacity {
		// Append phase.
		b.buf = append(b.buf, v)
		b.sum += v
		b.sumSqr += v * v
		b.count++
	} else {
		// Overwrite phase — subtract the outgoing sample, add the new one,
		// advance currentIndex modulo capacity. Matches Apple byte-for-byte.
		outgoing := b.buf[b.currentIndex]
		b.buf[b.currentIndex] = v
		b.sum = b.sum - outgoing + v
		b.sumSqr = b.sumSqr - outgoing*outgoing + v*v
		b.currentIndex = (b.currentIndex + 1) % b.capacity
	}
	b.sortedValid = false
	b.mean = b.sum / float64(b.count)
}

// sorted returns an ascending-sorted snapshot of the current buffer contents.
// Matches -[sortedArray] at 0xbc94 — caches under `sortedValid`, rebuilds on
// first call after any mutation.
func (b *NumberBuffer) sorted() []float64 {
	if b.sortedValid {
		return b.sortedCache
	}
	if b.count == 0 {
		b.sortedCache = nil
	} else {
		// Apple uses `[buffer subarrayWithRange:0..count]` when count < capacity,
		// and the full buffer when count == capacity. Both give the same logical
		// set of live samples; a direct copy of buf[:count] is correct regardless.
		b.sortedCache = append(b.sortedCache[:0], b.buf[:b.count]...)
		sort.Float64s(b.sortedCache)
	}
	b.sortedValid = true
	return b.sortedCache
}

// Count returns the number of live samples (0xc6dc).
func (b *NumberBuffer) Count() int { return b.count }

// Capacity returns the ring size (0xc6cc).
func (b *NumberBuffer) Capacity() int { return b.capacity }

// Mean returns the running mean (0xc6d4). Zero when count == 0.
func (b *NumberBuffer) Mean() float64 {
	if b.count == 0 {
		return 0
	}
	return b.mean
}

// Min returns the smallest sample or 0 when empty (0xbed0).
func (b *NumberBuffer) Min() float64 {
	s := b.sorted()
	if len(s) == 0 {
		return 0
	}
	return s[0]
}

// Max returns the largest sample or 0 when empty (0xbfb0).
func (b *NumberBuffer) Max() float64 {
	s := b.sorted()
	if len(s) == 0 {
		return 0
	}
	return s[len(s)-1]
}

// Median matches -[median] at 0xbd50 — mean-of-middle-two for even n, single
// middle element for odd n, 0 for empty.
func (b *NumberBuffer) Median() float64 {
	s := b.sorted()
	n := len(s)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[(n-1)/2] + s[n/2]) * 0.5
}

// Variance matches -[variance] at 0xc0bc — population variance, clamped ≥ 0,
// returns 0 when count < 2.
func (b *NumberBuffer) Variance() float64 {
	if b.count < 2 {
		return 0
	}
	v := b.sumSqr/float64(b.count) - b.mean*b.mean
	if v <= 0 {
		return 0
	}
	return v
}

// P95 matches -[p95] at 0xc124 — `sorted[floor((n-1) * 0.95)]`.
// The cast to uint64 in ARM64 is `vcvtmd_u64_f64` which truncates toward zero
// for non-negative inputs, which is identical to `math.Floor` here since
// (n-1)*0.95 is ≥ 0.
func (b *NumberBuffer) P95() float64 {
	s := b.sorted()
	n := len(s)
	if n == 0 {
		return 0
	}
	idx := int(math.Floor(float64(n-1) * 0.95))
	if idx >= n {
		idx = n - 1
	}
	return s[idx]
}

// Results matches -[getResultsWithName:] at 0xc400.
// The SDK returns an NSDictionary keyed by "min_<name>", "max_<name>", etc.;
// we produce the same strings so the caller can merge directly into the outer
// `behaviour_results` map.
func (b *NumberBuffer) Results(name string) map[string]interface{} {
	return map[string]interface{}{
		"min_" + name:    b.Min(),
		"max_" + name:    b.Max(),
		"mean_" + name:   b.Mean(),
		"median_" + name: b.Median(),
		"var_" + name:    b.Variance(),
		"p95_" + name:    b.P95(),
	}
}
