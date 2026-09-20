package diag

import (
	"fmt"
	"sort"
	"strings"
)

// Input is the set of already-processed artifacts for one measurement group.
// Any field may be empty; the analyzer degrades to whatever it was given and
// records the gap as a health issue.
type Input struct {
	GroupID string

	HTTP  []HTTPStat
	Query []QueryStat
	Funcs []FuncStat

	// FuncStat.FlatPct already carries each function's share of the profile, so
	// the absolute sample total is not retained here.

	HasHTTPLog bool
	HasSlowLog bool
	HasPprof   bool
}

// Thresholds collects the tunable constants. They are exposed so the values are
// visible and adjustable in one place rather than scattered as magic numbers.
type Thresholds struct {
	// ExaminedPerSent above which a query is considered to be missing an index.
	IndexExaminedRatio float64
	// Queries per request above which a query looks like an N+1 loop.
	NPlusOneRatio float64
	// Minimum share of total time before a finding is worth reporting at all.
	MinImpactShare float64
	// Share of 4xx/5xx responses that constitutes an error problem.
	ErrorRateWarn float64
	// Share of CPU a single function must hold to be reported.
	CPUHotspotShare float64
	// Average response body size (bytes) above which payload size is flagged.
	LargeBodyBytes float64
}

// DefaultThresholds are tuned for a typical ISUCON workload: aggressive enough
// to surface the real problems in the first measurement, conservative enough
// that the list stays short.
func DefaultThresholds() Thresholds {
	return Thresholds{
		IndexExaminedRatio: 100,
		NPlusOneRatio:      5,
		MinImpactShare:     0.01,
		ErrorRateWarn:      0.01,
		CPUHotspotShare:    0.05,
		LargeBodyBytes:     1 << 20,
	}
}

// Analyze runs every rule and returns findings ordered by score.
func Analyze(in Input, th Thresholds) Report {
	sum := summarize(in)

	var findings []Finding
	findings = append(findings, analyzeQueries(in, sum, th)...)
	findings = append(findings, analyzeHTTP(in, sum, th)...)
	findings = append(findings, analyzeCPU(in, sum, th)...)

	// Highest score first; ties broken by impact then title so the ordering is
	// stable across runs and tests are deterministic.
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Score != findings[j].Score {
			return findings[i].Score > findings[j].Score
		}
		if findings[i].ImpactShare != findings[j].ImpactShare {
			return findings[i].ImpactShare > findings[j].ImpactShare
		}
		return findings[i].Title < findings[j].Title
	})

	// Marshal empty results as [] rather than null so that consumers can
	// iterate unconditionally.
	if findings == nil {
		findings = []Finding{}
	}
	health := CheckHealth(in)
	if health == nil {
		health = []HealthIssue{}
	}

	return Report{
		GroupID:  in.GroupID,
		Health:   health,
		Findings: findings,
		Summary:  sum,
	}
}

func summarize(in Input) Summary {
	s := Summary{
		HasHTTPLog: in.HasHTTPLog,
		HasSlowLog: in.HasSlowLog,
		HasPprof:   in.HasPprof,
	}
	for _, h := range in.HTTP {
		s.TotalRequests += h.Count
		s.TotalAppTime += h.Sum
	}
	for _, q := range in.Query {
		s.TotalQueries += q.Count
		s.TotalQueryTime += q.SumQueryTime
	}
	return s
}

// analyzeQueries covers INDEX, N+1, LOCK and CACHE, all derived from slp output
// and — for N+1 — the request count from alp.
func analyzeQueries(in Input, sum Summary, th Thresholds) []Finding {
	if len(in.Query) == 0 {
		return nil
	}

	// Impact is measured against whichever total is available. Query time is
	// the natural denominator; when the HTTP log is present we prefer total app
	// time because it puts DB cost in context of the whole request budget.
	denom := sum.TotalQueryTime
	if sum.TotalAppTime > 0 {
		denom = sum.TotalAppTime
	}
	if denom <= 0 {
		return nil
	}

	// Count writes per table so a read-heavy table can be recommended for
	// caching with some justification.
	writesByTable := map[string]int{}
	readsByTable := map[string]int{}
	readTimeByTable := map[string]float64{}
	for _, q := range in.Query {
		tbl := tableOf(q.Query)
		if tbl == "" {
			continue
		}
		switch kindOf(q.Query) {
		case KindSelect:
			readsByTable[tbl] += q.Count
			readTimeByTable[tbl] += q.SumQueryTime
		case KindInsert, KindUpdate, KindDelete, KindReplace:
			writesByTable[tbl] += q.Count
		}
	}

	// Tables for which an index was recommended. Suggesting a cache for a table
	// whose real problem is a missing index sends people down a far more
	// expensive path than the one-line fix that actually solves it.
	indexedTables := map[string]bool{}

	var out []Finding
	for _, q := range in.Query {
		impact := q.SumQueryTime / denom
		if impact < th.MinImpactShare {
			continue
		}

		kind := kindOf(q.Query)
		ratio := q.ExaminedPerSent()

		// --- INDEX ---------------------------------------------------------
		// A high examined/sent ratio is the single most reliable signal that a
		// query lacks an index, and the fix is the cheapest available.
		if ratio >= th.IndexExaminedRatio && (kind == KindSelect || kind == KindUpdate || kind == KindDelete) {
			f := Finding{
				Category:    CatIndex,
				Title:       "インデックス未使用の可能性が高いクエリ",
				Subject:     truncate(q.Query, 160),
				ImpactShare: impact,
				Confidence:  0.9,
				Effort:      EffortTrivial,
				Source:      "slowlog",
				Evidence: []Evidence{
					{Label: "実行回数", Value: fmt.Sprintf("%d", q.Count)},
					{Label: "合計実行時間", Value: fmt.Sprintf("%.3fs", q.SumQueryTime), Ratio: impact},
					{Label: "平均実行時間", Value: fmt.Sprintf("%.3fs", q.AvgQueryTime)},
					{Label: "走査行/返却行", Value: fmt.Sprintf("%.0f / %.0f = %.0f倍", q.SumRowsExamined, q.SumRowsSent, ratio)},
				},
				Suggestion: indexSuggestion(q),
			}
			f.Score = score(f.ImpactShare, f.Confidence, f.Effort)
			out = append(out, f)

			if tbl := tableOf(q.Query); tbl != "" {
				indexedTables[tbl] = true
			}
		}

		// --- N+1 -----------------------------------------------------------
		// Detected by joining slp against alp: many executions per request,
		// each individually fast. Without the request count we cannot tell an
		// N+1 loop from a legitimately popular query, so this rule stays off
		// when the HTTP log is missing.
		if sum.TotalRequests > 0 && kind == KindSelect {
			perReq := float64(q.Count) / float64(sum.TotalRequests)
			if perReq >= th.NPlusOneRatio && q.AvgQueryTime < 0.05 {
				f := Finding{
					Category:    CatNPlusOne,
					Title:       "N+1クエリの疑い",
					Subject:     truncate(q.Query, 160),
					ImpactShare: impact,
					// Slightly lower than INDEX: a batch job or a fan-out
					// endpoint can produce the same shape legitimately.
					Confidence: 0.8,
					Effort:     EffortMedium,
					Source:     "slowlog x httplog",
					Evidence: []Evidence{
						{Label: "実行回数", Value: fmt.Sprintf("%d", q.Count)},
						{Label: "1リクエストあたり", Value: fmt.Sprintf("%.1f回", perReq)},
						{Label: "合計実行時間", Value: fmt.Sprintf("%.3fs", q.SumQueryTime), Ratio: impact},
						{Label: "平均実行時間", Value: fmt.Sprintf("%.4fs", q.AvgQueryTime)},
					},
					Suggestion: "ループ内の単体SELECTをIN句での一括取得かJOINにまとめる。取得済みデータのマップ化も有効",
				}
				f.Score = score(f.ImpactShare, f.Confidence, f.Effort)
				out = append(out, f)
			}
		}

		// --- LOCK ----------------------------------------------------------
		// Lock time that is a large fraction of query time means contention,
		// not slow execution; adding an index will not help.
		if q.SumLockTime > 0 && q.SumQueryTime > 0 {
			lockShare := q.SumLockTime / q.SumQueryTime
			lockImpact := q.SumLockTime / denom
			if lockShare > 0.3 && lockImpact >= th.MinImpactShare {
				f := Finding{
					Category:    CatLock,
					Title:       "ロック待ちが支配的なクエリ",
					Subject:     truncate(q.Query, 160),
					ImpactShare: lockImpact,
					Confidence:  0.7,
					Effort:      EffortLarge,
					Source:      "slowlog",
					Evidence: []Evidence{
						{Label: "合計ロック時間", Value: fmt.Sprintf("%.3fs", q.SumLockTime), Ratio: lockImpact},
						{Label: "クエリ時間に占める割合", Value: fmt.Sprintf("%.0f%%", lockShare*100)},
						{Label: "実行回数", Value: fmt.Sprintf("%d", q.Count)},
					},
					Suggestion: "トランザクション範囲の縮小、更新順序の統一、行ロック範囲の見直しを検討。インデックス追加では解消しない",
				}
				f.Score = score(f.ImpactShare, f.Confidence, f.Effort)
				out = append(out, f)
			}
		}
	}

	// --- CACHE -------------------------------------------------------------
	// Emitted per table rather than per query: the decision to cache is made at
	// the table level. Requires a strong read/write skew, which is what makes
	// a cache safe to introduce under time pressure.
	for tbl, readTime := range readTimeByTable {
		impact := readTime / denom
		if impact < th.MinImpactShare*3 {
			continue
		}
		// An unindexed table is an INDEX problem, not a cache problem. Adding
		// the index is cheaper, safer and usually removes the cost entirely.
		if indexedTables[tbl] {
			continue
		}
		reads := readsByTable[tbl]
		writes := writesByTable[tbl]
		if reads < 100 {
			continue
		}
		// A table with no observed writes at all is the ideal cache candidate;
		// allow a small amount of writes before confidence drops.
		rw := float64(reads)
		if writes > 0 {
			rw = float64(reads) / float64(writes)
		}
		if writes > 0 && rw < 50 {
			continue
		}

		conf := Confidence(0.6)
		if writes == 0 {
			conf = 0.75
		}

		f := Finding{
			Category:    CatCache,
			Title:       "キャッシュ候補のテーブル（参照が更新を大きく上回る）",
			Subject:     tbl,
			ImpactShare: impact,
			Confidence:  conf,
			Effort:      EffortMedium,
			Source:      "slowlog",
			Evidence: []Evidence{
				{Label: "参照クエリ回数", Value: fmt.Sprintf("%d", reads)},
				{Label: "更新クエリ回数", Value: fmt.Sprintf("%d", writes)},
				{Label: "参照/更新比", Value: formatRW(reads, writes)},
				{Label: "参照の合計時間", Value: fmt.Sprintf("%.3fs", readTime), Ratio: impact},
			},
			Suggestion: fmt.Sprintf("%s の参照結果をアプリ内メモリにキャッシュする。更新箇所でのキャッシュ破棄を忘れずに", tbl),
		}
		f.Score = score(f.ImpactShare, f.Confidence, f.Effort)
		out = append(out, f)
	}

	return out
}

func formatRW(reads, writes int) string {
	if writes == 0 {
		return fmt.Sprintf("%d : 0 (更新なし)", reads)
	}
	return fmt.Sprintf("%.0f : 1", float64(reads)/float64(writes))
}

func indexSuggestion(q QueryStat) string {
	tbl := tableOf(q.Query)
	cols := indexColumns(q.Query)
	if tbl == "" || len(cols) == 0 {
		return "WHERE / ORDER BY の対象カラムに複合インデックスを追加する。EXPLAINで走査行数を確認"
	}
	name := "idx_" + strings.Join(cols, "_")
	return fmt.Sprintf("ALTER TABLE `%s` ADD INDEX %s (%s);", tbl, name, "`"+strings.Join(cols, "`, `")+"`")
}

// analyzeHTTP covers ERROR, STATIC and PAYLOAD from alp output.
func analyzeHTTP(in Input, sum Summary, th Thresholds) []Finding {
	if len(in.HTTP) == 0 || sum.TotalAppTime <= 0 {
		return nil
	}

	var out []Finding
	for _, h := range in.HTTP {
		impact := h.Sum / sum.TotalAppTime

		// --- STATIC --------------------------------------------------------
		// Static assets reaching the application at all is pure waste, and
		// moving them to the web server is a config-only change.
		if isStaticPath(h.URI) && h.Count > 0 {
			f := Finding{
				Category:    CatStatic,
				Title:       "静的ファイルがアプリケーションを経由している",
				Subject:     h.Endpoint(),
				ImpactShare: impact,
				Confidence:  0.85,
				Effort:      EffortTrivial,
				Source:      "httplog",
				Evidence: []Evidence{
					{Label: "リクエスト数", Value: fmt.Sprintf("%d", h.Count)},
					{Label: "合計時間", Value: fmt.Sprintf("%.3fs", h.Sum), Ratio: impact},
					{Label: "平均サイズ", Value: fmt.Sprintf("%.0f bytes", h.AvgBody)},
				},
				Suggestion: "nginx等のWebサーバで直接配信し、アプリに到達させない。あわせてCache-Controlとgzipを設定",
			}
			f.Score = score(f.ImpactShare, f.Confidence, f.Effort)
			out = append(out, f)
			continue
		}

		// --- ERROR ---------------------------------------------------------
		// Errors are not a latency problem but they invalidate the rest of the
		// measurement, so they are reported regardless of time share.
		errCount := h.Status4xx + h.Status5xx
		if h.Count > 0 && errCount > 0 {
			rate := float64(errCount) / float64(h.Count)
			if rate >= th.ErrorRateWarn && errCount >= 5 {
				conf := Confidence(0.9)
				effort := EffortMedium
				if h.Status5xx == 0 {
					// 4xx alone is often the benchmark probing on purpose.
					conf = 0.5
					effort = EffortSmall
				}
				f := Finding{
					Category: CatError,
					Title:    "エラーレスポンスが発生している",
					Subject:  h.Endpoint(),
					// Errors distort every other measurement, so they are given
					// a floor on impact rather than their raw time share.
					ImpactShare: maxF(impact, rate*0.1),
					Confidence:  conf,
					Effort:      effort,
					Source:      "httplog",
					Evidence: []Evidence{
						{Label: "リクエスト数", Value: fmt.Sprintf("%d", h.Count)},
						{Label: "4xx", Value: fmt.Sprintf("%d", h.Status4xx)},
						{Label: "5xx", Value: fmt.Sprintf("%d", h.Status5xx)},
						{Label: "エラー率", Value: fmt.Sprintf("%.1f%%", rate*100), Ratio: rate},
					},
					Suggestion: "アプリログを確認し失敗原因を特定する。5xxはベンチのスコア減点に直結するため最優先",
				}
				f.Score = score(f.ImpactShare, f.Confidence, f.Effort)
				out = append(out, f)
			}
		}

		// --- PAYLOAD -------------------------------------------------------
		if h.AvgBody >= th.LargeBodyBytes && h.Count > 0 {
			f := Finding{
				Category:    CatPayload,
				Title:       "レスポンスサイズが大きいエンドポイント",
				Subject:     h.Endpoint(),
				ImpactShare: impact,
				Confidence:  0.6,
				Effort:      EffortSmall,
				Source:      "httplog",
				Evidence: []Evidence{
					{Label: "リクエスト数", Value: fmt.Sprintf("%d", h.Count)},
					{Label: "平均サイズ", Value: fmt.Sprintf("%.1f MB", h.AvgBody/(1<<20))},
					{Label: "合計転送量", Value: fmt.Sprintf("%.1f MB", h.SumBody/(1<<20))},
					{Label: "合計時間", Value: fmt.Sprintf("%.3fs", h.Sum), Ratio: impact},
				},
				Suggestion: "返却件数の制限、不要フィールドの削除、gzip圧縮の有効化を検討",
			}
			f.Score = score(f.ImpactShare, f.Confidence, f.Effort)
			out = append(out, f)
		}
	}
	return out
}

// analyzeCPU reports application-level hotspots from the CPU profile.
func analyzeCPU(in Input, sum Summary, th Thresholds) []Finding {
	if len(in.Funcs) == 0 {
		return nil
	}

	var out []Finding
	reported := 0
	for _, fn := range in.Funcs {
		if reported >= 5 {
			break
		}
		if fn.FlatPct < th.CPUHotspotShare {
			break
		}
		if isNoise(fn.Function) {
			continue
		}

		hs := classifyHotspot(fn.Function)
		title := "CPU使用率の高い関数"
		suggestion := "この関数の呼び出し回数削減、または結果のキャッシュを検討"
		effort := EffortLarge
		conf := Confidence(0.65)
		if hs != nil {
			title = hs.title
			suggestion = hs.suggestion
			effort = hs.effort
			conf = 0.8
		}

		f := Finding{
			Category: CatAppCPU,
			Title:    title,
			Subject:  fn.Function,
			// CPU share is a share of CPU, not of wall time. Scale it down so
			// it does not outrank DB findings measured against total app time,
			// which is the more meaningful denominator for latency.
			ImpactShare: fn.FlatPct * cpuWeight(sum),
			Confidence:  conf,
			Effort:      effort,
			Source:      "pprof",
			Evidence: []Evidence{
				{Label: "CPU占有率(flat)", Value: fmt.Sprintf("%.1f%%", fn.FlatPct*100), Ratio: fn.FlatPct},
			},
			Suggestion: suggestion,
		}
		f.Score = score(f.ImpactShare, f.Confidence, f.Effort)
		out = append(out, f)
		reported++
	}
	return out
}

// cpuWeight decides how much a CPU share counts relative to DB time.
//
// When the slow log shows the database is already consuming most of the request
// budget, CPU hotspots are secondary and are discounted. With no slow log we
// cannot make that comparison, so CPU is taken at closer to face value.
func cpuWeight(sum Summary) float64 {
	if !sum.HasSlowLog || sum.TotalAppTime <= 0 {
		return 0.8
	}
	dbShare := sum.TotalQueryTime / sum.TotalAppTime
	if dbShare > 0.5 {
		return 0.4
	}
	return 0.8
}

var staticExts = []string{
	".css", ".js", ".mjs", ".png", ".jpg", ".jpeg", ".gif", ".svg",
	".ico", ".woff", ".woff2", ".ttf", ".eot", ".map", ".webp", ".avif",
}

var staticPrefixes = []string{
	"/static/", "/assets/", "/images/", "/img/", "/css/", "/js/", "/fonts/", "/public/",
}

func isStaticPath(uri string) bool {
	// Strip a query string before matching on the extension.
	if i := strings.IndexByte(uri, '?'); i >= 0 {
		uri = uri[:i]
	}
	lower := strings.ToLower(uri)
	for _, e := range staticExts {
		if strings.HasSuffix(lower, e) {
			return true
		}
	}
	for _, p := range staticPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
