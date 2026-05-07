// Package runner implements a native Go load generator for the URL-shortener
// gateway and resolver. It exposes a single Runner.Run that streams per-second
// LoadTestSample values into a SampleSink until the run terminates.
package runner

import (
	"fmt"
	"strings"
	"time"

	loadgenv1 "github.com/yld/url-shortener/services/proto/gen/loadgen/v1"
)

// Plan is a fully-resolved load test plan: scenario, target rate and duration.
type Plan struct {
	Scenario  string
	TargetRPS uint32
	Duration  time.Duration
}

// PlanFromRequest resolves a LoadTestRequest into a Plan. Presets win over
// explicit fields; CUSTOM/UNSPECIFIED falls back to the explicit fields.
func PlanFromRequest(req *loadgenv1.LoadTestRequest) (Plan, error) {
	if req == nil {
		return Plan{}, fmt.Errorf("nil request")
	}
	if p, ok := planFromPreset(req.GetPreset()); ok {
		return p, nil
	}
	scenario := strings.ToLower(strings.TrimSpace(req.GetScenario()))
	if scenario == "" {
		return Plan{}, fmt.Errorf("scenario required when preset unspecified")
	}
	if !validScenario(scenario) {
		return Plan{}, fmt.Errorf("unknown scenario %q", scenario)
	}
	if req.GetTargetRps() == 0 {
		return Plan{}, fmt.Errorf("target_rps must be > 0")
	}
	if req.GetDurationSeconds() == 0 {
		return Plan{}, fmt.Errorf("duration_seconds must be > 0")
	}
	return Plan{
		Scenario:  scenario,
		TargetRPS: req.GetTargetRps(),
		Duration:  time.Duration(req.GetDurationSeconds()) * time.Second,
	}, nil
}

func planFromPreset(p loadgenv1.Preset) (Plan, bool) {
	switch p {
	case loadgenv1.Preset_LOW:
		return Plan{Scenario: "read", TargetRPS: 100, Duration: 10 * time.Second}, true
	case loadgenv1.Preset_MEDIUM:
		return Plan{Scenario: "read", TargetRPS: 1000, Duration: 15 * time.Second}, true
	case loadgenv1.Preset_HIGH:
		return Plan{Scenario: "read", TargetRPS: 5000, Duration: 20 * time.Second}, true
	case loadgenv1.Preset_XHIGH:
		return Plan{Scenario: "read", TargetRPS: 10000, Duration: 20 * time.Second}, true
	case loadgenv1.Preset_XXHIGH:
		return Plan{Scenario: "mixed", TargetRPS: 25000, Duration: 20 * time.Second}, true
	case loadgenv1.Preset_INSANE:
		return Plan{Scenario: "mixed", TargetRPS: 50000, Duration: 30 * time.Second}, true
	default:
		return Plan{}, false
	}
}

func validScenario(s string) bool {
	switch s {
	case "read", "write", "mixed":
		return true
	default:
		return false
	}
}
