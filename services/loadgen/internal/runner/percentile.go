package runner

import "sort"

// Percentiles computes p50, p95 and p99 over a slice of latencies (in
// milliseconds) using a sort+index approach. Returns zeros for an empty slice.
// The input slice is sorted in place.
func Percentiles(samples []float64) (p50, p95, p99 float64) {
	if len(samples) == 0 {
		return 0, 0, 0
	}
	sort.Float64s(samples)
	return pick(samples, 0.50), pick(samples, 0.95), pick(samples, 0.99)
}

func pick(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * q)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
