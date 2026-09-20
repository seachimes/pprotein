// Package diag turns the raw alp / slp / pprof artifacts that pprotein already
// collects into a prioritized list of actionable findings.
//
// The guiding principle is that a finding must never be a bare assertion: every
// item carries the numbers it was derived from so that a human can reject a
// false positive at a glance. During a contest a wrong-but-confident hint costs
// more time than no hint at all.
package diag

import "math"

type (
	// Category is the kind of fix a finding suggests.
	Category string

	// Effort is a coarse estimate of how long a fix takes to apply. It is part
	// of the score because a contest is a race against the clock: a small fix
	// with moderate impact usually beats a large fix with high impact.
	Effort int

	// Confidence expresses how reliable the rule that produced a finding is.
	Confidence float64
)

const (
	CatIndex    Category = "INDEX"
	CatNPlusOne Category = "N+1"
	CatCache    Category = "CACHE"
	CatAppCPU   Category = "APP-CPU"
	CatStatic   Category = "STATIC"
	CatLock     Category = "LOCK"
	CatError    Category = "ERROR"
	CatPayload  Category = "PAYLOAD"
)

const (
	EffortTrivial Effort = 1 // add an index, serve a file from nginx
	EffortSmall   Effort = 2
	EffortMedium  Effort = 3 // introduce a cache, bulk-load a query
	EffortLarge   Effort = 5 // restructure application logic
	EffortHuge    Effort = 8 // change the data model
)

// Evidence is a single labelled number backing a finding. Keeping these as
// structured data rather than a pre-rendered string lets the UI align them and
// lets tests assert on them.
type Evidence struct {
	Label string  `json:"Label"`
	Value string  `json:"Value"`
	Ratio float64 `json:"Ratio,omitempty"`
}

// Finding is one prioritized recommendation.
type Finding struct {
	Category Category `json:"Category"`
	Title    string   `json:"Title"`
	Subject  string   `json:"Subject"`

	// ImpactShare is the fraction (0..1) of total observed time attributable
	// to this finding. It is the dominant term in Score.
	ImpactShare float64    `json:"ImpactShare"`
	Confidence  Confidence `json:"Confidence"`
	Effort      Effort     `json:"Effort"`
	Score       float64    `json:"Score"`

	Evidence   []Evidence `json:"Evidence"`
	Suggestion string     `json:"Suggestion"`
	Source     string     `json:"Source"`
}

// Severity buckets a health issue.
type Severity string

const (
	SevError Severity = "error"
	SevWarn  Severity = "warn"
	SevInfo  Severity = "info"
)

// HealthIssue reports a problem with the measurement itself rather than with
// the system under test. Acting on data that was collected wrongly is the most
// expensive mistake available, so these are surfaced above findings.
type HealthIssue struct {
	Severity Severity `json:"Severity"`
	Source   string   `json:"Source"`
	Title    string   `json:"Title"`
	Detail   string   `json:"Detail"`
	Hint     string   `json:"Hint"`
}

// Report is the full diagnosis of one measurement group.
type Report struct {
	GroupID  string        `json:"GroupID"`
	Health   []HealthIssue `json:"Health"`
	Findings []Finding     `json:"Findings"`
	Summary  Summary       `json:"Summary"`
}

// Summary holds the aggregate numbers the findings were scored against.
type Summary struct {
	TotalRequests  int     `json:"TotalRequests"`
	TotalAppTime   float64 `json:"TotalAppTime"`
	TotalQueries   int     `json:"TotalQueries"`
	TotalQueryTime float64 `json:"TotalQueryTime"`
	HasHTTPLog     bool    `json:"HasHTTPLog"`
	HasSlowLog     bool    `json:"HasSlowLog"`
	HasPprof       bool    `json:"HasPprof"`
}

// score combines impact, confidence and effort into the ordering key.
//
// Impact dominates, confidence discounts speculative rules, and effort divides
// so that cheap fixes float up. Effort is applied with a square root rather
// than linearly: a fix that takes 8x longer is not 8x less attractive, because
// the payoff is still permanent.
func score(impact float64, conf Confidence, effort Effort) float64 {
	if effort <= 0 {
		effort = EffortSmall
	}
	return impact * float64(conf) / sqrt(float64(effort))
}

// sqrt guards against a non-positive effort so that score() cannot divide by
// zero or produce NaN.
func sqrt(f float64) float64 {
	if f <= 0 {
		return 1
	}
	return math.Sqrt(f)
}
