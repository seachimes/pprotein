<template>
  <section>
    <div class="controls">
      <label>
        変更前<br />
        <select v-model="before" @change="load">
          <option v-for="g in groups" :key="g.GroupID" :value="g.GroupID">
            {{ g.GroupID }}
          </option>
        </select>
      </label>
      <span class="arrow" aria-hidden="true">→</span>
      <label>
        変更後<br />
        <select v-model="after" @change="load">
          <option v-for="g in groups" :key="g.GroupID" :value="g.GroupID">
            {{ g.GroupID }}
          </option>
        </select>
      </label>
      <label>
        &nbsp;<br />
        <button :disabled="!canCompare || loading" @click="load">
          {{ loading ? "比較中..." : "比較" }}
        </button>
      </label>
    </div>

    <p v-if="groups.length < 2" class="empty">
      比較には2つ以上の計測グループが必要です。
    </p>
    <p v-else-if="!diff && !loading" class="empty">
      比較する2つの計測を選択してください。
    </p>

    <template v-if="diff">
      <h2>全体</h2>
      <dl class="totals">
        <div>
          <dt>リクエスト数</dt>
          <dd>
            {{ formatInt(diff.Totals.RequestsBefore) }} →
            {{ formatInt(diff.Totals.RequestsAfter) }}
          </dd>
        </div>
        <div>
          <dt>1リクエストあたり応答時間</dt>
          <dd :class="pctClass(diff.Totals.AppTimePct)">
            {{ pctSymbol(diff.Totals.AppTimePct) }}
            {{ formatPct(diff.Totals.AppTimePct) }}
          </dd>
        </div>
        <div>
          <dt>1リクエストあたりDB時間</dt>
          <dd :class="pctClass(diff.Totals.QueryTimePct)">
            {{ pctSymbol(diff.Totals.QueryTimePct) }}
            {{ formatPct(diff.Totals.QueryTimePct) }}
          </dd>
        </div>
        <div>
          <dt>エラー数</dt>
          <dd :class="errClass">
            {{ diff.Totals.ErrorsBefore }} → {{ diff.Totals.ErrorsAfter }}
          </dd>
        </div>
      </dl>

      <h2>エンドポイント</h2>
      <table class="diff">
        <caption class="visually-hidden">
          エンドポイント別の変化
        </caption>
        <thead>
          <tr>
            <th scope="col">判定</th>
            <th scope="col">エンドポイント</th>
            <th scope="col">回数</th>
            <th scope="col">平均</th>
            <th scope="col">変化</th>
            <th scope="col">エラー</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(h, i) in diff.HTTP" :key="i" :class="h.Direction">
            <td class="dir">
              {{ directionSymbol(h.Direction) }}
              <span v-if="h.Status !== 'both'" class="tag">{{
                h.Status === "added" ? "新規" : "消失"
              }}</span>
            </td>
            <th scope="row" class="name">{{ h.Endpoint }}</th>
            <td class="num">
              {{ formatInt(h.CountBefore) }} → {{ formatInt(h.CountAfter) }}
            </td>
            <td class="num">
              {{ formatSec(h.AvgBefore) }} → {{ formatSec(h.AvgAfter) }}
            </td>
            <td class="num">{{ formatPct(h.AvgPct) }}</td>
            <td class="num">
              <span v-if="h.ErrBefore || h.ErrAfter">
                {{ h.ErrBefore }} → {{ h.ErrAfter }}
              </span>
              <span v-else>-</span>
            </td>
          </tr>
        </tbody>
      </table>

      <h2>クエリ</h2>
      <table class="diff">
        <caption class="visually-hidden">
          クエリ別の変化
        </caption>
        <thead>
          <tr>
            <th scope="col">判定</th>
            <th scope="col">クエリ</th>
            <th scope="col">回数</th>
            <th scope="col">合計時間</th>
            <th scope="col">変化</th>
            <th scope="col">走査行</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(q, i) in diff.Query" :key="i" :class="q.Direction">
            <td class="dir">
              {{ directionSymbol(q.Direction) }}
              <span v-if="q.Status !== 'both'" class="tag">{{
                q.Status === "added" ? "新規" : "消失"
              }}</span>
            </td>
            <th scope="row" class="name query">{{ q.Query }}</th>
            <td class="num">
              {{ formatInt(q.CountBefore) }} → {{ formatInt(q.CountAfter) }}
            </td>
            <td class="num">
              {{ formatSec(q.SumBefore) }} → {{ formatSec(q.SumAfter) }}
            </td>
            <td class="num">{{ formatPct(q.SumPct) }}</td>
            <td class="num">
              {{ formatInt(q.ExaminedBefore) }} →
              {{ formatInt(q.ExaminedAfter) }}
            </td>
          </tr>
        </tbody>
      </table>
    </template>
  </section>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import {
  directionSymbol,
  fetchDiff,
  fetchGroups,
  formatInt,
  formatPct,
  formatSec,
  type DiffReport,
  type GroupInfo,
} from "../diag";

export default defineComponent({
  data() {
    return {
      groups: [] as GroupInfo[],
      before: "",
      after: "",
      diff: null as DiffReport | null,
      loading: false,
    };
  },
  computed: {
    canCompare(): boolean {
      return !!this.before && !!this.after && this.before !== this.after;
    },
    errClass(): string {
      const t = this.diff?.Totals;
      if (!t) return "";
      if (t.ErrorsAfter > t.ErrorsBefore) return "worsened";
      if (t.ErrorsAfter < t.ErrorsBefore) return "improved";
      return "";
    },
  },
  async mounted() {
    const groups = await fetchGroups();
    this.groups = groups || [];
    // Groups arrive newest-first, so default to comparing the latest run
    // against the one before it — the usual measure/change/measure loop.
    if (this.groups.length >= 2) {
      this.after = this.groups[0].GroupID;
      this.before = this.groups[1].GroupID;
      await this.load();
    }
  },
  methods: {
    formatInt,
    formatSec,
    formatPct,
    directionSymbol,
    async load() {
      if (!this.canCompare) return;
      this.loading = true;
      try {
        this.diff = await fetchDiff(this.before, this.after);
      } finally {
        this.loading = false;
      }
    },
    // Lower is better for every metric shown here.
    pctClass(pct: number): string {
      if (pct <= -5) return "improved";
      if (pct >= 5) return "worsened";
      return "";
    },
    pctSymbol(pct: number): string {
      if (pct <= -5) return "▼";
      if (pct >= 5) return "▲";
      return "—";
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
    max-width: 22em;

    &:focus-visible {
      outline: 2px solid orangered;
      outline-offset: 1px;
    }
  }

  .arrow {
    padding-bottom: 0.5em;
  }
}

.empty {
  color: #666;
}

.totals {
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

.diff {
  border-collapse: collapse;
  width: 100%;

  th,
  td {
    border: 1px solid #ddd;
    padding: 0.4em 0.8em;
    text-align: start;
    vertical-align: top;
  }

  thead th {
    background: #f4f4f4;
    white-space: nowrap;
  }

  .num {
    text-align: end;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  .name {
    font-weight: normal;
    overflow-wrap: anywhere;
  }

  .query {
    font-size: 0.9em;
    max-width: 32em;
  }

  .dir {
    white-space: nowrap;
  }

  .tag {
    display: inline-block;
    margin-inline-start: 0.4em;
    padding: 0 0.4em;
    border: 1px solid currentcolor;
    border-radius: 0.2em;
    font-size: 0.8em;
  }
}

// The direction word is always rendered alongside the tint, so colour is a
// redundant cue rather than the only one.
.improved {
  color: #1e8449;
}
.worsened {
  color: #c0392b;
}

tr.improved td,
tr.improved th {
  background: #f2faf4;
}
tr.worsened td,
tr.worsened th {
  background: #fdf2f0;
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
