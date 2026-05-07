package runner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	loadgenv1 "github.com/yld/url-shortener/services/proto/gen/loadgen/v1"
)

func TestPlanFromRequest_Presets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		preset   loadgenv1.Preset
		scenario string
		rps      uint32
		dur      time.Duration
	}{
		{loadgenv1.Preset_LOW, "read", 100, 10 * time.Second},
		{loadgenv1.Preset_MEDIUM, "read", 1000, 15 * time.Second},
		{loadgenv1.Preset_HIGH, "read", 5000, 20 * time.Second},
		{loadgenv1.Preset_XHIGH, "read", 10000, 20 * time.Second},
		{loadgenv1.Preset_XXHIGH, "mixed", 25000, 20 * time.Second},
		{loadgenv1.Preset_INSANE, "mixed", 50000, 30 * time.Second},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.preset.String(), func(t *testing.T) {
			t.Parallel()
			p, err := PlanFromRequest(&loadgenv1.LoadTestRequest{Preset: tc.preset})
			require.NoError(t, err)
			assert.Equal(t, tc.scenario, p.Scenario)
			assert.Equal(t, tc.rps, p.TargetRPS)
			assert.Equal(t, tc.dur, p.Duration)
		})
	}
}

func TestPlanFromRequest_CustomFields(t *testing.T) {
	t.Parallel()
	p, err := PlanFromRequest(&loadgenv1.LoadTestRequest{
		Preset:          loadgenv1.Preset_CUSTOM,
		Scenario:        "write",
		TargetRps:       250,
		DurationSeconds: 5,
	})
	require.NoError(t, err)
	assert.Equal(t, "write", p.Scenario)
	assert.Equal(t, uint32(250), p.TargetRPS)
	assert.Equal(t, 5*time.Second, p.Duration)
}

func TestPlanFromRequest_RejectsBadScenario(t *testing.T) {
	t.Parallel()
	_, err := PlanFromRequest(&loadgenv1.LoadTestRequest{
		Preset:          loadgenv1.Preset_CUSTOM,
		Scenario:        "garbage",
		TargetRps:       100,
		DurationSeconds: 5,
	})
	require.Error(t, err)
}

func TestPlanFromRequest_RequiresFieldsWhenNoPreset(t *testing.T) {
	t.Parallel()
	_, err := PlanFromRequest(&loadgenv1.LoadTestRequest{Preset: loadgenv1.Preset_PRESET_UNSPECIFIED})
	require.Error(t, err)

	_, err = PlanFromRequest(&loadgenv1.LoadTestRequest{
		Preset:          loadgenv1.Preset_CUSTOM,
		Scenario:        "read",
		TargetRps:       0,
		DurationSeconds: 5,
	})
	require.Error(t, err)
}
