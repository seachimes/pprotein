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
        <RangeEntryCard
          v-for="entry in entriesByType[type]"
          :key="entry.Snapshot.ID"
          :entry="entry"
          :with-date="datedTimes"
        />
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
</style>
