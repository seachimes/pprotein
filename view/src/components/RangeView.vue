<template>
  <section>
    <RangeControls
      :range="range"
      :auto="isAuto"
      :types="types"
      :counts="counts"
      @change="applyRange($event)"
    />

    <div v-for="type in types" :key="type" class="typeSection">
      <h2>{{ rangeTypeLabels[type] || type }}</h2>
      <template v-if="counts[type]">
        <div
          v-for="group in labelGroups[type]"
          :key="group.label"
          class="labelGroup"
        >
          <h3>{{ group.label || "[no label]" }}</h3>
          <RangeEntryCard
            v-for="entry in group.entries"
            :key="entry.Snapshot.ID"
            :entry="entry"
            :with-date="datedTimes"
          />
        </div>
      </template>
      <p v-else class="empty">No entries in this range</p>
    </div>
  </section>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import {
  rangeFromQuery,
  rangeToQuery,
  rangeTypeLabels,
  rangeTypes,
  spansDays,
  TimeRange,
} from "../range";
import { Entry } from "../store";
import RangeControls from "./RangeControls.vue";
import RangeEntryCard from "./RangeEntryCard.vue";

const DEFAULT_SPAN = 10 * 60 * 1000;

export default defineComponent({
  components: {
    RangeControls,
    RangeEntryCard,
  },
  data() {
    return {
      rangeTypeLabels,
    };
  },
  computed: {
    queryRange(): TimeRange | undefined {
      return rangeFromQuery(this.$route.query);
    },
    isAuto(): boolean {
      return !this.queryRange;
    },
    // without a range in the URL, follow the newest collection: entries arrive
    // asynchronously, so a range fixed at mount would often be empty
    range(): TimeRange {
      if (this.queryRange) {
        return this.queryRange;
      }

      const [latest] = this.$store.state.groups;
      const groupRange = latest && this.$store.getters.groupRange(latest);
      if (groupRange) {
        return groupRange;
      }

      const to = new Date();
      return { from: new Date(to.getTime() - DEFAULT_SPAN), to };
    },
    entriesByType(): { [type: string]: Entry[] } {
      const grouped: { [type: string]: Entry[] } = {};
      for (const entry of this.$store.getters.entriesInRange(
        this.range,
      ) as Entry[]) {
        (grouped[entry.Snapshot.Type] ||= []).push(entry);
      }
      return grouped;
    },
    // one host is one column of history: keeping its measurements together is
    // what makes before/after comparable, so Label groups win over time order
    labelGroups(): { [type: string]: { label: string; entries: Entry[] }[] } {
      const grouped: { [type: string]: { label: string; entries: Entry[] }[] } =
        {};
      for (const [type, entries] of Object.entries(this.entriesByType)) {
        const byLabel: { [label: string]: Entry[] } = {};
        for (const entry of entries) {
          (byLabel[entry.Snapshot.Label || ""] ||= []).push(entry);
        }
        grouped[type] = Object.keys(byLabel)
          .sort((a, b) => a.localeCompare(b))
          .map((label) => ({ label, entries: byLabel[label] }));
      }
      return grouped;
    },
    // known types first, then anything a newer collector adds
    types(): string[] {
      const extra = Object.keys(this.entriesByType).filter(
        (type) => !rangeTypes.includes(type),
      );
      return [...rangeTypes, ...extra];
    },
    datedTimes(): boolean {
      return spansDays(this.range);
    },
    counts(): { [type: string]: number } {
      return Object.fromEntries(
        this.types.map((type) => [
          type,
          (this.entriesByType[type] || []).length,
        ]),
      );
    },
  },
  methods: {
    applyRange(range: TimeRange) {
      // replace, not push: narrowing down a range takes several edits, and none
      // of them is a place the back button should have to walk through
      this.$router.replace({ path: "/range/", query: rangeToQuery(range) });
    },
  },
});
</script>

<style scoped lang="scss">
section {
  margin: 2em;
}

.typeSection {
  margin-bottom: 2em;

  h2 {
    font-size: 1.1em;
    border-bottom: 2px solid #333;
    padding-bottom: 0.3em;
  }

  .empty {
    color: #888;
  }
}

.labelGroup {
  margin-bottom: 1em;

  h3 {
    font-size: 0.95em;
    font-weight: bold;
    color: #555;
    margin: 0.8em 0 0.4em;
  }
}
</style>
