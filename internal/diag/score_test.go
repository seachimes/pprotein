package diag

import "testing"

func TestCompareScore(t *testing.T) {
	run := func(score int64, passed bool) *ScoreRun {
		return &ScoreRun{Score: score, Passed: passed}
	}

	tests := []struct {
		name          string
		before, after *ScoreRun
		latency       DiffDirection
		wantDir       DiffDirection
		wantConflict  bool
	}{
		{
			name:   "higher score is an improvement",
			before: run(10000, true), after: run(12000, true),
			latency: DirImproved,
			wantDir: DirImproved,
		},
		{
			name:   "lower score is a regression",
			before: run(12000, true), after: run(10000, true),
			latency: DirWorsened,
			wantDir: DirWorsened,
		},
		{
			// The reason this feature exists: latency says the change worked,
			// the score says it lost. The score decides, and the disagreement
			// is surfaced rather than hidden.
			name:   "faster but lower score is a flagged regression",
			before: run(12000, true), after: run(9000, true),
			latency:      DirImproved,
			wantDir:      DirWorsened,
			wantConflict: true,
		},
		{
			// A failed run scores zero, which is not a measurement. It must
			// read as broken regardless of what the latency did.
			name:   "failed run is a regression even when faster",
			before: run(12000, true), after: run(0, false),
			latency:      DirImproved,
			wantDir:      DirWorsened,
			wantConflict: true,
		},
		{
			// Fixing a broken benchmark is progress even if the score is still
			// below an earlier successful run.
			name:   "recovering from a failed run is an improvement",
			before: run(0, false), after: run(8000, true),
			latency: DirWorsened,
			wantDir: DirImproved,
		},
		{
			name:   "identical scores are neutral",
			before: run(10000, true), after: run(10000, true),
			latency: DirNeutral,
			wantDir: DirNeutral,
		},
		{
			// No conflict to report when both readings agree.
			name:   "regression with slower latency is not a conflict",
			before: run(12000, true), after: run(10000, true),
			latency:      DirWorsened,
			wantDir:      DirWorsened,
			wantConflict: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareScore(tt.before, tt.after, tt.latency)
			if got == nil {
				t.Fatal("CompareScore() = nil, want a comparison")
			}
			if got.Direction != tt.wantDir {
				t.Errorf("Direction = %q, want %q", got.Direction, tt.wantDir)
			}
			if got.Conflict != tt.wantConflict {
				t.Errorf("Conflict = %v, want %v", got.Conflict, tt.wantConflict)
			}
			if want := tt.after.Score - tt.before.Score; got.Delta != want {
				t.Errorf("Delta = %d, want %d", got.Delta, want)
			}
		})
	}
}

func TestCompareScoreMissing(t *testing.T) {
	present := &ScoreRun{Score: 100, Passed: true}

	// A comparison needs both sides. Reporting a change against a run with no
	// recorded score would invent a baseline that was never measured.
	tests := []struct {
		name          string
		before, after *ScoreRun
	}{
		{"neither side recorded", nil, nil},
		{"only the earlier run recorded", present, nil},
		{"only the later run recorded", nil, present},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CompareScore(tt.before, tt.after, DirNeutral); got != nil {
				t.Errorf("CompareScore() = %+v, want nil", got)
			}
		})
	}
}
