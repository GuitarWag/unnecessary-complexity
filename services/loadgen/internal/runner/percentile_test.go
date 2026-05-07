package runner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPercentiles_Empty(t *testing.T) {
	t.Parallel()
	p50, p95, p99 := Percentiles(nil)
	assert.Equal(t, 0.0, p50)
	assert.Equal(t, 0.0, p95)
	assert.Equal(t, 0.0, p99)
}

func TestPercentiles_OrderedSpread(t *testing.T) {
	t.Parallel()
	samples := make([]float64, 100)
	for i := range samples {
		samples[i] = float64(i + 1) // 1..100
	}
	p50, p95, p99 := Percentiles(samples)
	assert.InDelta(t, 50.0, p50, 1.0)
	assert.InDelta(t, 95.0, p95, 1.0)
	assert.InDelta(t, 99.0, p99, 1.0)
}

func TestPercentiles_Single(t *testing.T) {
	t.Parallel()
	p50, p95, p99 := Percentiles([]float64{42})
	assert.Equal(t, 42.0, p50)
	assert.Equal(t, 42.0, p95)
	assert.Equal(t, 42.0, p99)
}
