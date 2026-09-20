package diag

import (
	"fmt"
	"strings"
)

// CheckHealth validates the measurement itself.
//
// These checks exist because the most expensive failure mode in a contest is
// not a missed optimization but a confident decision made on bad data: a log
// that was never rotated, a slow log that was never enabled, or an alp config
// that scatters one endpoint across thousands of rows. Each of those silently
// produces a plausible-looking report.
func CheckHealth(in Input) []HealthIssue {
	var issues []HealthIssue

	issues = append(issues, checkAvailability(in)...)
	issues = append(issues, checkHTTPLog(in)...)
	issues = append(issues, checkSlowLog(in)...)
	issues = append(issues, checkConsistency(in)...)

	return issues
}

func checkAvailability(in Input) []HealthIssue {
	var issues []HealthIssue

	if !in.HasHTTPLog {
		issues = append(issues, HealthIssue{
			Severity: SevWarn,
			Source:   "httplog",
			Title:    "アクセスログが収集されていない",
			Detail:   "httplogのスナップショットがこのグループに存在しません。N+1判定はリクエスト数を必要とするため無効化されます。",
			Hint:     "targetsにhttplogのエントリがあるか、nginxのltsvログ出力が有効かを確認してください。",
		})
	} else if len(in.HTTP) == 0 {
		issues = append(issues, HealthIssue{
			Severity: SevError,
			Source:   "httplog",
			Title:    "アクセスログが空",
			Detail:   "httplogは収集されましたが、集計結果が0件です。",
			Hint:     "計測期間中にリクエストが到達していないか、ログのパスやフォーマット(ltsv)が誤っている可能性があります。",
		})
	}

	if !in.HasSlowLog {
		issues = append(issues, HealthIssue{
			Severity: SevWarn,
			Source:   "slowlog",
			Title:    "スロークエリログが収集されていない",
			Detail:   "slowlogのスナップショットがこのグループに存在しません。INDEX/N+1の判定ができません。",
			Hint:     "slow_query_log=1, long_query_time=0 が設定されているか確認してください。",
		})
	} else if len(in.Query) == 0 {
		issues = append(issues, HealthIssue{
			Severity: SevError,
			Source:   "slowlog",
			Title:    "スロークエリログが空",
			Detail:   "slowlogは収集されましたが、集計結果が0件です。",
			Hint:     "long_query_time が大きすぎる可能性があります。計測時は long_query_time=0 を推奨します。",
		})
	}

	if !in.HasPprof {
		issues = append(issues, HealthIssue{
			Severity: SevInfo,
			Source:   "pprof",
			Title:    "CPUプロファイルが収集されていない",
			Detail:   "pprofのスナップショットがこのグループに存在しません。アプリCPUの判定ができません。",
			Hint:     "アプリにpprotein-agentを組み込み、targetsにpprofのエントリを追加してください。",
		})
	}

	return issues
}

func checkHTTPLog(in Input) []HealthIssue {
	if len(in.HTTP) == 0 {
		return nil
	}

	var issues []HealthIssue

	// A very large number of distinct endpoints almost always means alp's
	// matching_groups is not configured, so /api/users/1 and /api/users/2 are
	// counted separately. The per-endpoint totals are then meaningless and the
	// real hotspot is hidden below the fold.
	if len(in.HTTP) > 200 {
		issues = append(issues, HealthIssue{
			Severity: SevError,
			Source:   "httplog",
			Title:    "エンドポイントが分散しすぎている（alpの正規化未設定の疑い）",
			Detail: fmt.Sprintf(
				"集計されたエンドポイントが%d件あります。IDを含むパスが正規化されていない可能性が高く、集計値が分散して実際のボトルネックが埋もれます。",
				len(in.HTTP)),
			Hint: "設定画面の httplog/config で matching_groups に ^/api/users/[0-9]+$ のようなパターンを追加してください。",
		})
	} else {
		// Even below the threshold, a cluster of numeric-tail paths is a
		// reliable sign of the same problem.
		if n := countNumericTailPaths(in.HTTP); n >= 10 {
			issues = append(issues, HealthIssue{
				Severity: SevWarn,
				Source:   "httplog",
				Title:    "IDらしきパスが正規化されていない可能性",
				Detail:   fmt.Sprintf("末尾が数字のパスが%d種類あります。", n),
				Hint:     "httplog/config の matching_groups で正規化すると集計が正確になります。",
			})
		}
	}

	var total, errors int
	for _, h := range in.HTTP {
		total += h.Count
		errors += h.Status4xx + h.Status5xx
	}
	if total > 0 {
		if rate := float64(errors) / float64(total); rate > 0.3 {
			issues = append(issues, HealthIssue{
				Severity: SevError,
				Source:   "httplog",
				Title:    "エラー率が異常に高い",
				Detail:   fmt.Sprintf("全リクエストの%.0f%%が4xx/5xxです。", rate*100),
				Hint:     "アプリが正常に動作していない状態の計測結果である可能性が高く、性能分析の前に機能面の修正が必要です。",
			})
		}
	}

	if total < 10 {
		issues = append(issues, HealthIssue{
			Severity: SevWarn,
			Source:   "httplog",
			Title:    "リクエスト数が少なすぎる",
			Detail:   fmt.Sprintf("計測期間中のリクエストは%d件です。", total),
			Hint:     "ベンチマーカーの実行と計測タイミングがずれていないか確認してください。",
		})
	}

	return issues
}

func checkSlowLog(in Input) []HealthIssue {
	if len(in.Query) == 0 {
		return nil
	}

	var issues []HealthIssue

	var total int
	var minAvg = -1.0
	for _, q := range in.Query {
		total += q.Count
		if minAvg < 0 || q.AvgQueryTime < minAvg {
			minAvg = q.AvgQueryTime
		}
	}

	// If the fastest query recorded is still slow, the log is only capturing
	// the tail. The aggregate totals then understate the database's real cost
	// and N+1 loops of fast queries are invisible.
	if minAvg > 0.05 {
		issues = append(issues, HealthIssue{
			Severity: SevWarn,
			Source:   "slowlog",
			Title:    "long_query_time が大きい可能性",
			Detail:   fmt.Sprintf("記録された最速のクエリでも平均%.3fsです。閾値以下のクエリが記録されていません。", minAvg),
			Hint:     "SET GLOBAL long_query_time=0; で全クエリを記録すると、N+1や軽量クエリの大量発行を検出できます。",
		})
	}

	if total < 10 {
		issues = append(issues, HealthIssue{
			Severity: SevWarn,
			Source:   "slowlog",
			Title:    "記録されたクエリ数が少なすぎる",
			Detail:   fmt.Sprintf("計測期間中のクエリは%d件です。", total),
			Hint:     "スロークエリログのローテート漏れ、または計測タイミングのずれを確認してください。",
		})
	}

	return issues
}

// checkConsistency cross-checks the sources against each other. Disagreements
// between them usually mean the logs cover different time windows — the classic
// "forgot to truncate the log before the benchmark" mistake.
func checkConsistency(in Input) []HealthIssue {
	if len(in.HTTP) == 0 || len(in.Query) == 0 {
		return nil
	}

	var issues []HealthIssue

	var reqs int
	var appTime float64
	for _, h := range in.HTTP {
		reqs += h.Count
		appTime += h.Sum
	}
	var queryTime float64
	for _, q := range in.Query {
		queryTime += q.SumQueryTime
	}

	// Database time cannot legitimately exceed total request time by a wide
	// margin unless the two logs cover different periods. Some overshoot is
	// normal with concurrency, so the threshold is deliberately generous.
	if appTime > 0 && queryTime > appTime*3 {
		issues = append(issues, HealthIssue{
			Severity: SevError,
			Source:   "httplog x slowlog",
			Title:    "ログの計測期間が一致していない疑い",
			Detail: fmt.Sprintf(
				"クエリ合計時間(%.1fs)がリクエスト合計時間(%.1fs)を大きく超えています。",
				queryTime, appTime),
			Hint: "ベンチ実行前に両方のログをローテート/truncateしてください。前回計測分が混入している可能性があります。",
		})
	}

	return issues
}

func countNumericTailPaths(stats []HTTPStat) int {
	n := 0
	for _, h := range stats {
		uri := h.URI
		if i := strings.IndexByte(uri, '?'); i >= 0 {
			uri = uri[:i]
		}
		uri = strings.TrimSuffix(uri, "/")
		idx := strings.LastIndexByte(uri, '/')
		if idx < 0 || idx == len(uri)-1 {
			continue
		}
		last := uri[idx+1:]
		if last == "" {
			continue
		}
		allDigit := true
		for _, r := range last {
			if r < '0' || r > '9' {
				allDigit = false
				break
			}
		}
		if allDigit {
			n++
		}
	}
	return n
}
