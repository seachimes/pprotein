// Package score records benchmark results alongside the profiling data that
// was collected during the same run.
//
// A benchmark score is the only measure that actually decides the contest, but
// it exists solely in the benchmarker's output. Without it, pprotein can show
// that a change made the application faster, yet cannot show whether the change
// won: a run can get faster and still score lower, typically because the
// benchmarker started failing validation.
package score

import (
	"fmt"
	"time"
)

type (
	// Value is the recorded benchmark result. It is stored as the snapshot body.
	Value struct {
		// Score is the benchmarker's headline number, where larger is better.
		Score int64
		// Passed reports whether the run itself succeeded. Failed runs are worth
		// recording: knowing which change broke the benchmark is as valuable as
		// knowing which change sped it up.
		Passed bool
		// ErrorCount is the number of errors the benchmarker reported.
		ErrorCount int64
		// Target identifies which host was driven, for multi-server setups.
		Target string
		// StartedAt and FinishedAt bound the run so it can be lined up against
		// the profiling window.
		StartedAt  time.Time
		FinishedAt time.Time
		// Raw keeps the benchmarker's own output verbatim. Every contest reports
		// a different breakdown, and a single integer cannot carry it, so the
		// original text is preserved for later reading.
		Raw string
	}
)

// Summary renders a one-line description for list views.
func (v *Value) Summary() string {
	result := "pass"
	if !v.Passed {
		result = "FAIL"
	}

	s := fmt.Sprintf("score=%d %s", v.Score, result)
	if v.ErrorCount > 0 {
		s += fmt.Sprintf(" errors=%d", v.ErrorCount)
	}
	if v.Target != "" {
		s += " " + v.Target
	}
	if d := v.Duration(); d > 0 {
		s += fmt.Sprintf(" %ds", int(d.Seconds()))
	}
	return s
}

// Duration reports how long the run took, or zero when it cannot be determined.
func (v *Value) Duration() time.Duration {
	if v.StartedAt.IsZero() || v.FinishedAt.IsZero() {
		return 0
	}
	if v.FinishedAt.Before(v.StartedAt) {
		return 0
	}
	return v.FinishedAt.Sub(v.StartedAt)
}
