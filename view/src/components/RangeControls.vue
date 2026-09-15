<template>
  <div class="controls">
    <div class="form">
      <label>
        From<br />
        <input
          type="datetime-local"
          :value="fromValue"
          @change="updateFrom($event)"
        />
      </label>
      <label>
        To<br />
        <input
          type="datetime-local"
          :value="toValue"
          @change="updateTo($event)"
        />
      </label>
      <label>
        &nbsp;<br />
        <button @click="widen(5)">Widen 5 min</button>
      </label>
      <label>
        &nbsp;<br />
        <button :disabled="!shrinkable" @click="widen(-5)">Narrow 5 min</button>
      </label>
      <label>
        Start from collection<br />
        <select :value="''" @change="seedFromGroup($event)">
          <option value="">Select …</option>
          <option v-for="g in $store.state.groups" :key="g" :value="g">
            {{ g }}
          </option>
        </select>
      </label>
    </div>
    <div class="summary">
      <span :class="['mode', { auto: $props.auto }]">
        {{ $props.auto ? "Following latest collection" : "Pinned range" }}
      </span>
      <span v-for="t in $props.types" :key="t" class="count">
        {{ rangeTypeLabels[t] || t }}:
        <strong :class="{ zero: !$props.counts[t] }">
          {{ $props.counts[t] || 0 }}
        </strong>
      </span>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, PropType } from "vue";
import {
  canShrink,
  fromInputValue,
  rangeTypeLabels,
  TimeRange,
  toInputValue,
  widened,
  withFrom,
  withTo,
} from "../range";

export default defineComponent({
  props: {
    range: {
      type: Object as PropType<TimeRange>,
      required: true,
    },
    auto: {
      type: Boolean,
      default: false,
    },
    types: {
      type: Array as PropType<string[]>,
      required: true,
    },
    counts: {
      type: Object as PropType<{ [type: string]: number }>,
      required: true,
    },
  },
  emits: ["change"],
  data() {
    return {
      rangeTypeLabels,
    };
  },
  computed: {
    fromValue() {
      return toInputValue(this.$props.range.from);
    },
    toValue() {
      return toInputValue(this.$props.range.to);
    },
    shrinkable() {
      return canShrink(this.$props.range);
    },
  },
  methods: {
    inputValue(event: Event) {
      return fromInputValue((event.target as HTMLInputElement).value);
    },
    updateFrom(event: Event) {
      const from = this.inputValue(event);
      if (from) {
        this.$emit("change", withFrom(this.$props.range, from));
      }
    },
    updateTo(event: Event) {
      const to = this.inputValue(event);
      if (to) {
        this.$emit("change", withTo(this.$props.range, to));
      }
    },
    widen(minutes: number) {
      this.$emit("change", widened(this.$props.range, minutes));
    },
    seedFromGroup(event: Event) {
      const select = event.target as HTMLSelectElement;
      const range = this.$store.getters.groupRange(select.value);
      // the bound value never changes, so Vue would not reset the placeholder
      select.value = "";

      if (range) {
        this.$emit("change", range);
      }
    },
  },
});
</script>

<style scoped lang="scss">
.controls {
  margin-bottom: 2em;
}

.form {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;

  label {
    margin-right: 1em;

    &:focus-within {
      color: orangered;
    }

    input,
    select {
      border: 1px solid lightgray;
      padding: 0.4em 1em;

      &:focus {
        border-color: orangered;
        outline: 0;
      }
    }
  }

  button:disabled {
    color: #bbb;
    border-color: lightgray;
    pointer-events: none;
  }
}

.summary {
  display: flex;
  flex-wrap: wrap;
  margin-top: 1em;

  .mode {
    margin-right: 1.5em;
    color: #666;

    &.auto {
      color: orangered;
    }
  }

  .count {
    margin-right: 1.5em;

    .zero {
      color: #bbb;
    }
  }
}
</style>
