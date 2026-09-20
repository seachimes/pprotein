package diag

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/google/pprof/profile"
)

// FuncStat is the flat cost of a single function in a CPU profile.
type FuncStat struct {
	Function string
	Flat     int64
	FlatPct  float64
}

// runtimeNoise are frames that are real CPU cost but never directly actionable:
// pointing a contestant at runtime.mallocgc wastes their time. GC pressure is
// reported separately as an aggregate instead.
var runtimeNoise = []string{
	"runtime.",
	"syscall.",
	"internal/poll.",
	"net.(*conn)",
	"bufio.",
}

func isNoise(fn string) bool {
	for _, p := range runtimeNoise {
		if strings.HasPrefix(fn, p) {
			return true
		}
	}
	return false
}

// ParsePprof reads a pprof protobuf and returns per-function flat cost, sorted
// descending, along with the total sample value.
//
// Only CPU-like sample types are considered. For a profile with multiple sample
// value types the last one is used, matching pprof's own default.
func ParsePprof(r io.Reader) ([]FuncStat, int64, error) {
	p, err := profile.Parse(r)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to parse profile: %w", err)
	}
	if len(p.SampleType) == 0 {
		return nil, 0, nil
	}

	vi := len(p.SampleType) - 1

	flat := map[string]int64{}
	var total int64
	for _, s := range p.Sample {
		if vi >= len(s.Value) {
			continue
		}
		v := s.Value[vi]
		if v == 0 {
			continue
		}
		total += v

		// The leaf frame carries the flat cost.
		if len(s.Location) == 0 {
			continue
		}
		leaf := s.Location[0]
		if len(leaf.Line) == 0 || leaf.Line[0].Function == nil {
			continue
		}
		flat[leaf.Line[0].Function.Name] += v
	}

	if total == 0 {
		return nil, 0, nil
	}

	stats := make([]FuncStat, 0, len(flat))
	for fn, v := range flat {
		stats = append(stats, FuncStat{
			Function: fn,
			Flat:     v,
			FlatPct:  float64(v) / float64(total),
		})
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Flat != stats[j].Flat {
			return stats[i].Flat > stats[j].Flat
		}
		return stats[i].Function < stats[j].Function
	})
	return stats, total, nil
}

// knownHotspot maps a function name fragment to a human explanation and the
// effort of addressing it. These cover the costs that show up in essentially
// every ISUCON-style workload.
type knownHotspot struct {
	match      string
	title      string
	suggestion string
	effort     Effort
}

var knownHotspots = []knownHotspot{
	{
		match:      "bcrypt",
		title:      "bcryptのハッシュ計算がCPUを占有",
		suggestion: "コストパラメータを下げる、または初回ログイン時にハッシュを軽量方式へ移行する",
		effort:     EffortSmall,
	},
	{
		match:      "crypto/sha",
		title:      "ハッシュ計算がCPUを占有",
		suggestion: "計算結果のキャッシュ、または呼び出し回数の削減を検討",
		effort:     EffortMedium,
	},
	{
		match:      "image/",
		title:      "画像処理がCPUを占有",
		suggestion: "生成済み画像をファイル/CDNにキャッシュし、リクエスト毎の再生成を避ける",
		effort:     EffortMedium,
	},
	{
		match:      "regexp",
		title:      "正規表現の実行がCPUを占有",
		suggestion: "正規表現をコンパイル済みで使い回す、または単純な文字列操作へ置換",
		effort:     EffortSmall,
	},
	{
		match:      "encoding/json",
		title:      "JSONのエンコード/デコードがCPUを占有",
		suggestion: "goccy/go-json等への差し替え、レスポンス量の削減を検討",
		effort:     EffortSmall,
	},
	{
		match:      "text/template",
		title:      "テンプレート描画がCPUを占有",
		suggestion: "テンプレートを起動時に一度だけパースし、描画結果をキャッシュ",
		effort:     EffortSmall,
	},
	{
		match:      "html/template",
		title:      "テンプレート描画がCPUを占有",
		suggestion: "テンプレートを起動時に一度だけパースし、描画結果をキャッシュ",
		effort:     EffortSmall,
	},
	{
		match:      "fmt.Sprintf",
		title:      "文字列整形がCPUを占有",
		suggestion: "ホットパスのSprintfを文字列連結やstrings.Builderへ置換",
		effort:     EffortSmall,
	},
}

func classifyHotspot(fn string) *knownHotspot {
	lower := strings.ToLower(fn)
	for i := range knownHotspots {
		if strings.Contains(lower, knownHotspots[i].match) {
			return &knownHotspots[i]
		}
	}
	return nil
}
