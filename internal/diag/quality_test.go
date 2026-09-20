package diag

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

// mergeHTTP/mergeQuery/mergeFuncs previously returned the source slice directly
// when the destination was empty. Merging a second server then wrote through
// that alias into the caller's data, corrupting one server's stats with
// another's. This is the multi-server path, which is the normal ISUCON setup.
func TestMergeDoesNotAliasInput(t *testing.T) {
	t.Run("http", func(t *testing.T) {
		a := []HTTPStat{{Count: 10, Method: "GET", URI: "/a", Sum: 1.0}}
		b := []HTTPStat{{Count: 20, Method: "GET", URI: "/a", Sum: 2.0}}
		orig := a[0]

		acc := mergeHTTP(mergeHTTP(nil, a), b)

		if a[0] != orig {
			t.Errorf("input mutated: %+v -> %+v", orig, a[0])
		}
		if acc[0].Count != 30 || acc[0].Sum != 3.0 {
			t.Errorf("merge wrong: count=%d sum=%v", acc[0].Count, acc[0].Sum)
		}
	})

	t.Run("query", func(t *testing.T) {
		a := []QueryStat{{Count: 10, Query: "SELECT 1", SumQueryTime: 1.0}}
		b := []QueryStat{{Count: 20, Query: "SELECT 1", SumQueryTime: 2.0}}
		orig := a[0]

		acc := mergeQuery(mergeQuery(nil, a), b)

		if a[0] != orig {
			t.Errorf("input mutated: %+v -> %+v", orig, a[0])
		}
		if acc[0].Count != 30 {
			t.Errorf("merged count = %d, want 30", acc[0].Count)
		}
	})

	t.Run("funcs", func(t *testing.T) {
		a := []FuncStat{{Function: "f", Flat: 10, FlatPct: 1}}
		b := []FuncStat{{Function: "f", Flat: 30, FlatPct: 1}}
		orig := a[0]

		acc := mergeFuncs(mergeFuncs(nil, a), b)

		if a[0] != orig {
			t.Errorf("input mutated: %+v -> %+v", orig, a[0])
		}
		if acc[0].Flat != 40 {
			t.Errorf("merged flat = %d, want 40", acc[0].Flat)
		}
	})
}

// A wrong ALTER TABLE is worse than no suggestion: the user runs it, gains
// nothing and loses time. When a statement reads from more than one table this
// regex-level parser cannot tell which table a column belongs to, so it must
// fall back to generic advice.
func TestNoIndexSuggestionAcrossMultipleTables(t *testing.T) {
	multi := []string{
		"SELECT * FROM `outer` WHERE `id` IN (SELECT `oid` FROM `inner` WHERE `y` = N)",
		"SELECT * FROM `a` JOIN `b` ON `a`.`id` = `b`.`a_id` WHERE `b`.`x` = N",
		"SELECT * FROM `a` LEFT JOIN `b` ON `a`.`id` = `b`.`a_id` WHERE `a`.`k` = N",
	}
	for _, q := range multi {
		got := indexSuggestion(QueryStat{Query: q})
		if strings.HasPrefix(got, "ALTER TABLE") {
			t.Errorf("emitted a concrete DDL for a multi-table query\n  query: %s\n  got:   %s", q, got)
		}
	}
}

// The single-table case must still produce a usable statement.
func TestIndexSuggestionStillWorksForSingleTable(t *testing.T) {
	q := QueryStat{Query: "SELECT * FROM `courses` WHERE `teacher_id` = N ORDER BY `created_at`"}
	want := "ALTER TABLE `courses` ADD INDEX idx_teacher_id_created_at (`teacher_id`, `created_at`);"
	if got := indexSuggestion(q); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// Degenerate inputs must not produce NaN/Inf: the report is served as JSON, and
// encoding/json rejects non-finite floats, which would turn the endpoint into a
// 500 rather than a merely odd-looking number.
func TestDegenerateInputsStayFiniteAndSerializable(t *testing.T) {
	inputs := map[string]Input{
		"empty": {},
		"zero counts": {
			HasHTTPLog: true, HasSlowLog: true,
			HTTP:  []HTTPStat{{Count: 0, Method: "GET", URI: "/a"}},
			Query: []QueryStat{{Count: 0, Query: "SELECT * FROM `t` WHERE `x` = N"}},
		},
		"time without requests": {
			HasHTTPLog: true, HasSlowLog: true,
			HTTP:  []HTTPStat{{Count: 0, Method: "GET", URI: "/a", Sum: 5}},
			Query: []QueryStat{{Count: 0, Query: "SELECT * FROM `t` WHERE `x` = N", SumQueryTime: 5, SumRowsExamined: 9e5}},
		},
		"huge": {
			HasHTTPLog: true, HasSlowLog: true,
			HTTP:  []HTTPStat{{Count: 1 << 40, Method: "GET", URI: "/a", Sum: 1e18, Avg: 1e9}},
			Query: []QueryStat{{Count: 1 << 40, Query: "SELECT * FROM `t` WHERE `x` = N", SumQueryTime: 1e18, SumRowsSent: 1, SumRowsExamined: 1e18}},
		},
	}

	for name, in := range inputs {
		rep := Analyze(in, DefaultThresholds())
		for _, f := range rep.Findings {
			for label, v := range map[string]float64{"impact": f.ImpactShare, "score": f.Score} {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Errorf("%s: %s of %v finding is %v", name, label, f.Category, v)
				}
			}
			for _, e := range f.Evidence {
				if strings.Contains(e.Value, "NaN") || strings.Contains(e.Value, "Inf") {
					t.Errorf("%s: evidence %q rendered as %q", name, e.Label, e.Value)
				}
			}
		}
		if _, err := json.Marshal(rep); err != nil {
			t.Errorf("%s: report does not marshal: %v", name, err)
		}
		if _, err := json.Marshal(Compare("a", "b", in, in)); err != nil {
			t.Errorf("%s: diff does not marshal: %v", name, err)
		}
	}
}

// Empty results must serialize as [] so that consumers can iterate without a
// null check.
func TestEmptyCollectionsSerializeAsArrays(t *testing.T) {
	b, err := json.Marshal(Analyze(Input{}, DefaultThresholds()))
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if strings.Contains(string(b), `"Findings":null`) {
		t.Error(`Findings serialized as null; want []`)
	}
	if strings.Contains(string(b), `"Health":null`) {
		t.Error(`Health serialized as null; want []`)
	}
}

// truncate() previously sliced bytes, so a query containing non-ASCII text —
// a Japanese column name or string literal, which is entirely normal here —
// was cut mid-rune and displayed as U+FFFD.
func TestTruncatePreservesMultibyteRunes(t *testing.T) {
	long := "SELECT * FROM `t` WHERE `名前` = 'あいうえおかきくけこさしすせそたちつてとなにぬねの'"
	got := truncate(long, 40)

	if strings.ContainsRune(got, '\uFFFD') {
		t.Errorf("truncate produced a replacement character: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("truncate produced invalid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(strings.TrimSuffix(got, "...")); n > 40 {
		t.Errorf("kept %d runes, want <= 40", n)
	}

	// Short strings must pass through untouched.
	short := "SELECT 1"
	if truncate(short, 40) != short {
		t.Errorf("short string altered: %q", truncate(short, 40))
	}
}
