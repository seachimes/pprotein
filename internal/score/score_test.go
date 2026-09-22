package score

import (
	"testing"
	"time"

	"github.com/kaz/pprotein/internal/collect"
)

type fakeSource struct{ entries []*collect.Entry }

func (f *fakeSource) List() []*collect.Entry { return f.entries }

func entry(group string, at time.Time, status collect.Status) *collect.Entry {
	return &collect.Entry{
		Snapshot: &collect.Snapshot{
			SnapshotMeta:   &collect.SnapshotMeta{Datetime: at},
			SnapshotTarget: &collect.SnapshotTarget{GroupId: group},
		},
		Status: status,
	}
}

func at(min int) time.Time {
	return time.Date(2026, 1, 1, 10, min, 0, 0, time.UTC)
}

func TestLatestGroup(t *testing.T) {
	tests := []struct {
		name    string
		sources []Source
		want    string
	}{
		{
			name: "picks the most recent group",
			sources: []Source{&fakeSource{[]*collect.Entry{
				entry("old", at(0), collect.StatusOk),
				entry("new", at(5), collect.StatusOk),
			}}},
			want: "new",
		},
		{
			// A collection that errored tells us nothing, so a score must not be
			// filed against it just because it happened last.
			name: "ignores failed collections",
			sources: []Source{&fakeSource{[]*collect.Entry{
				entry("good", at(0), collect.StatusOk),
				entry("broken", at(5), collect.StatusFail),
			}}},
			want: "good",
		},
		{
			// Groups span collectors; the newest snapshot in any of them decides.
			name: "compares across sources",
			sources: []Source{
				&fakeSource{[]*collect.Entry{entry("a", at(1), collect.StatusOk)}},
				&fakeSource{[]*collect.Entry{entry("b", at(9), collect.StatusOk)}},
			},
			want: "b",
		},
		{
			// A group's time is its newest member, so a group that started
			// earlier but finished later still wins.
			name: "uses the newest snapshot within a group",
			sources: []Source{&fakeSource{[]*collect.Entry{
				entry("early", at(0), collect.StatusOk),
				entry("early", at(9), collect.StatusOk),
				entry("late", at(5), collect.StatusOk),
			}}},
			want: "early",
		},
		{
			name: "ignores entries without a group",
			sources: []Source{&fakeSource{[]*collect.Entry{
				entry("", at(9), collect.StatusOk),
				entry("real", at(1), collect.StatusOk),
			}}},
			want: "real",
		},
		{
			name:    "reports nothing when there is no collection",
			sources: []Source{&fakeSource{nil}},
			want:    "",
		},
		{
			name:    "tolerates a nil source",
			sources: []Source{nil},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LatestGroup(tt.sources); got != tt.want {
				t.Errorf("LatestGroup() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValueSummary(t *testing.T) {
	tests := []struct {
		name  string
		value Value
		want  string
	}{
		{
			name:  "passing run",
			value: Value{Score: 18902, Passed: true},
			want:  "score=18902 pass",
		},
		{
			// A failed run must be obvious: its score is meaningless, and the
			// change that caused it is what the reader is looking for.
			name:  "failed run",
			value: Value{Score: 0, Passed: false},
			want:  "score=0 FAIL",
		},
		{
			name:  "reports errors and target",
			value: Value{Score: 100, Passed: true, ErrorCount: 3, Target: "isu1"},
			want:  "score=100 pass errors=3 isu1",
		},
		{
			name: "includes duration when known",
			value: Value{
				Score: 100, Passed: true,
				StartedAt: at(0), FinishedAt: at(1),
			},
			want: "score=100 pass 60s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Summary(); got != tt.want {
				t.Errorf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValueDuration(t *testing.T) {
	tests := []struct {
		name  string
		value Value
		want  time.Duration
	}{
		{"both known", Value{StartedAt: at(0), FinishedAt: at(2)}, 2 * time.Minute},
		{"missing start", Value{FinishedAt: at(2)}, 0},
		{"missing finish", Value{StartedAt: at(0)}, 0},
		// Clocks on separate machines can disagree; a negative span is not a
		// duration worth reporting.
		{"finish before start", Value{StartedAt: at(5), FinishedAt: at(1)}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.value.Duration(); got != tt.want {
				t.Errorf("Duration() = %v, want %v", got, tt.want)
			}
		})
	}
}
