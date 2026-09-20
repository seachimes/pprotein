<template>
  <section>
    <div class="controls">
      <label>
        計測グループ<br />
        <select v-model="selected" @change="load">
          <option v-if="!groups.length" value="">（計測データなし）</option>
          <option v-for="g in groups" :key="g.GroupID" :value="g.GroupID">
            {{ g.GroupID }}
            {{ g.HasHTTPLog ? "" : " [httplogなし]"
            }}{{ g.HasSlowLog ? "" : " [slowlogなし]" }}
          </option>
        </select>
      </label>
      <label>
        &nbsp;<br />
        <button :disabled="!selected || loading" @click="load">
          {{ loading ? "解析中..." : "再解析" }}
        </button>
      </label>
    </div>

    <p v-if="!report && !loading" class="empty">
      計測グループを選択すると、ボトルネックの分類と優先順位を表示します。
    </p>

    <template v-if="report">
      <!-- Measurement health comes first: acting on bad data is the most
           expensive mistake, so it must be seen before any finding. -->
      <div v-if="report.Health.length" class="health">
        <h2>計測の健全性</h2>
        <ul>
          <li
            v-for="(h, i) in report.Health"
            :key="i"
            :class="['issue', h.Severity]"
          >
            <span class="badge" aria-hidden="true">{{
              severitySymbol(h.Severity)
            }}</span>
            <div>
              <strong>{{ h.Title }}</strong>
              <span class="src">({{ h.Source }})</span>
              <p>{{ h.Detail }}</p>
              <p class="hint">→ {{ h.Hint }}</p>
            </div>
          </li>
        </ul>
      </div>

      <h2>サマリ</h2>
      <dl class="summary">
        <div>
          <dt>総リクエスト</dt>
          <dd>{{ formatInt(report.Summary.TotalRequests) }}</dd>
        </div>
        <div>
          <dt>合計レスポンス時間</dt>
          <dd>{{ formatSec(report.Summary.TotalAppTime) }}</dd>
        </div>
        <div>
          <dt>総クエリ</dt>
          <dd>{{ formatInt(report.Summary.TotalQueries) }}</dd>
        </div>
        <div>
          <dt>合計クエリ時間</dt>
          <dd>{{ formatSec(report.Summary.TotalQueryTime) }}</dd>
        </div>
        <div>
          <dt>DB時間の割合</dt>
          <dd>{{ dbShare }}</dd>
        </div>
      </dl>

      <h2>改善候補（優先度順）</h2>
      <p v-if="!report.Findings.length" class="empty">
        閾値を超えるボトルネックは検出されませんでした。
      </p>

      <ol class="findings">
        <li v-for="(f, i) in report.Findings" :key="i" class="finding">
          <div class="head">
            <span class="rank">#{{ i + 1 }}</span>
            <span :class="['cat', catClass(f.Category)]">{{ f.Category }}</span>
            <span class="title">{{ f.Title }}</span>
          </div>

          <div class="meta">
            <span
              >影響度
              <strong>{{ (f.ImpactShare * 100).toFixed(1) }}%</strong></span
            >
            <span
              >工数 <strong>{{ effortLabel(f.Effort) }}</strong></span
            >
            <span
              >確信度 <strong>{{ confidenceLabel(f.Confidence) }}</strong></span
            >
            <span class="src">{{ f.Source }}</span>
          </div>

          <div class="impactbar" aria-hidden="true">
            <div :style="{ width: barWidth(f.ImpactShare) }"></div>
          </div>

          <p class="subject">{{ f.Subject }}</p>

          <!-- Every finding shows the numbers it was derived from, so a false
               positive can be dismissed at a glance. -->
          <table class="evidence">
            <caption class="visually-hidden">
              判定根拠
            </caption>
            <tbody>
              <tr v-for="(e, j) in f.Evidence" :key="j">
                <th scope="row">{{ e.Label }}</th>
                <td>{{ e.Value }}</td>
              </tr>
            </tbody>
          </table>

          <p class="suggestion">{{ f.Suggestion }}</p>
        </li>
      </ol>
    </template>
  </section>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import {
  confidenceLabel,
  effortLabel,
  fetchGroups,
  fetchReport,
  formatInt,
  formatSec,
  severitySymbol,
  type Category,
  type GroupInfo,
  type Report,
} from "../diag";

export default defineComponent({
  data() {
    return {
      groups: [] as GroupInfo[],
      selected: "",
      report: null as Report | null,
      loading: false,
    };
  },
  computed: {
    dbShare(): string {
      const s = this.report?.Summary;
      if (!s || s.TotalAppTime <= 0) return "-";
      return `${((s.TotalQueryTime / s.TotalAppTime) * 100).toFixed(0)}%`;
    },
  },
  async mounted() {
    const groups = await fetchGroups();
    this.groups = groups || [];
    // Default to the newest group: it is almost always the one just measured.
    if (this.groups.length) {
      this.selected = this.groups[0].GroupID;
      await this.load();
    }
  },
  methods: {
    formatInt,
    formatSec,
    effortLabel,
    confidenceLabel,
    severitySymbol,
    async load() {
      if (!this.selected) return;
      this.loading = true;
      try {
        this.report = await fetchReport(this.selected);
      } finally {
        this.loading = false;
      }
    },
    barWidth(share: number): string {
      // Clamp so a single dominant finding cannot overflow the track.
      return `${Math.min(100, Math.max(1, share * 100))}%`;
    },
    catClass(c: Category): string {
      return c.toLowerCase().replace("+", "plus").replace("-", "");
    },
  },
});
</script>

<style scoped lang="scss">
section {
  padding: 2em;
  overflow: auto;
}

h2 {
  font-size: 1.1em;
  margin: 1.5em 0 0.6em;
}

.controls {
  display: flex;
  gap: 1em;
  align-items: flex-end;

  label:focus-within {
    color: orangered;
  }

  select {
    padding: 0.4em;
    border: 1px solid lightgray;

    &:focus-visible {
      outline: 2px solid orangered;
      outline-offset: 1px;
    }
  }
}

.empty {
  color: #666;
}

.health ul {
  list-style: none;
  padding: 0;
  margin: 0;
}

.issue {
  display: flex;
  gap: 0.8em;
  padding: 0.7em 1em;
  margin-bottom: 0.5em;
  border-left: 0.4em solid #999;
  background: #f6f6f6;

  p {
    margin: 0.3em 0 0;
  }

  .src {
    color: #666;
    margin-inline-start: 0.5em;
  }

  .hint {
    color: #444;
  }

  // The leading symbol carries the same meaning as the colour, so severity is
  // never communicated by colour alone.
  .badge {
    flex-shrink: 0;
    width: 1.6em;
    height: 1.6em;
    line-height: 1.6em;
    text-align: center;
    border-radius: 50%;
    font-weight: bold;
    color: #fff;
    background: #999;
  }

  &.error {
    border-left-color: #c0392b;
    background: #fdf0ee;
    .badge {
      background: #c0392b;
    }
  }
  &.warn {
    border-left-color: #d68910;
    background: #fdf8ec;
    .badge {
      background: #d68910;
    }
  }
  &.info {
    border-left-color: #2874a6;
    background: #eef4fa;
    .badge {
      background: #2874a6;
    }
  }
}

.summary {
  display: flex;
  flex-wrap: wrap;
  gap: 2em;
  margin: 0;

  dt {
    color: #666;
    font-size: 0.9em;
  }
  dd {
    margin: 0.2em 0 0;
    font-size: 1.2em;
    font-weight: bold;
  }
}

.findings {
  list-style: none;
  padding: 0;
  margin: 0;
}

.finding {
  border: 1px solid #ddd;
  border-inline-start: 0.4em solid #bbb;
  padding: 1em;
  margin-bottom: 1em;

  .head {
    display: flex;
    align-items: center;
    gap: 0.7em;
    flex-wrap: wrap;
  }

  .rank {
    font-weight: bold;
    font-size: 1.1em;
  }

  .cat {
    padding: 0.15em 0.6em;
    border: 1px solid currentcolor;
    border-radius: 0.2em;
    font-size: 0.85em;
    font-weight: bold;
  }

  // Each category keeps a distinct colour, but the label text is always present
  // so the colour is redundant rather than load-bearing.
  .cat.index {
    color: #1e8449;
  }
  .cat.nplus1,
  .cat.nplus {
    color: #b9770e;
  }
  .cat.cache {
    color: #7d3c98;
  }
  .cat.appcpu {
    color: #1a5276;
  }
  .cat.static {
    color: #616a6b;
  }
  .cat.lock {
    color: #922b21;
  }
  .cat.error {
    color: #c0392b;
  }
  .cat.payload {
    color: #117864;
  }

  .title {
    font-weight: bold;
  }

  .meta {
    display: flex;
    gap: 1.2em;
    flex-wrap: wrap;
    margin: 0.6em 0;
    font-size: 0.9em;
    color: #555;

    .src {
      color: #888;
    }
  }

  .impactbar {
    height: 0.4em;
    background: #eee;
    margin-bottom: 0.8em;

    div {
      height: 100%;
      background: orangered;
    }
  }

  .subject {
    font-family: "Courier Prime", monospace;
    background: #f4f4f4;
    padding: 0.6em 0.8em;
    margin: 0 0 0.8em;
    overflow-wrap: anywhere;
  }

  .evidence {
    border-collapse: collapse;
    margin-bottom: 0.8em;

    th,
    td {
      text-align: start;
      padding: 0.2em 1.5em 0.2em 0;
      font-weight: normal;
    }
    th {
      color: #666;
      white-space: nowrap;
    }
    td {
      font-variant-numeric: tabular-nums;
    }
  }

  .suggestion {
    margin: 0;
    padding: 0.6em 0.8em;
    background: #eef7ee;
    border-inline-start: 0.25em solid #1e8449;
    overflow-wrap: anywhere;
  }
}

.visually-hidden {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
</style>
