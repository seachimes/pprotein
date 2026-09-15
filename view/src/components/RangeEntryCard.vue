<template>
  <details class="card" @toggle="open($event)">
    <summary>
      <span class="label">{{ snapshot.Label || "[no label]" }}</span>
      <span class="time">{{ timeText }}</span>
      <Status
        :status="$props.entry.Status"
        :message="statusMessage"
        class="status"
      />
      <router-link v-if="ready && detailPath" :to="detailPath" class="detail">
        Open
      </router-link>
    </summary>
    <div class="body">
      <Commit :repository="snapshot.Repository" />
      <template v-if="ready && opened">
        <TsvPanel
          v-if="snapshot.Type == 'httplog' || snapshot.Type == 'slowlog'"
          :id="snapshot.ID"
          :endpoint="snapshot.Type"
        />
        <iframe
          v-else-if="snapshot.Type == 'pprof'"
          :src="`/api/pprof/${snapshot.ID}/flamegraph`"
        />
        <pre v-else-if="snapshot.Type == 'memo'">{{ memoText }}</pre>
      </template>
    </div>
  </details>
</template>

<script lang="ts">
import { defineComponent, PropType } from "vue";
import { Entry } from "../store";
import Commit from "./Commit.vue";
import Status from "./Status.vue";
import TsvPanel from "./TsvPanel.vue";

export default defineComponent({
  components: {
    Commit,
    Status,
    TsvPanel,
  },
  props: {
    entry: {
      type: Object as PropType<Entry>,
      required: true,
    },
    withDate: {
      type: Boolean,
      default: false,
    },
  },
  data() {
    return {
      // bodies mount on first open; a range can hold many pprof iframes
      opened: false,
      memoText: "Loading ...",
    };
  },
  computed: {
    snapshot() {
      return this.$props.entry.Snapshot;
    },
    ready() {
      return this.$props.entry.Status == "ok";
    },
    timeText() {
      const { Datetime, Duration } = this.snapshot;
      const time = this.$props.withDate
        ? Datetime.toLocaleString()
        : Datetime.toLocaleTimeString();
      return Duration ? `${time} +${Duration}s` : time;
    },
    // a memo's Message is its entire text, which the body already renders
    statusMessage() {
      return this.ready && this.snapshot.Type == "memo"
        ? "Ready"
        : this.$props.entry.Message;
    },
    detailPath() {
      const { GroupId, Type, ID } = this.snapshot;
      if (GroupId) {
        return `/group/${GroupId}/${Type}/${ID}/`;
      }
      // memo only has a route nested under its group
      return Type == "memo" ? undefined : `/${Type}/${ID}/`;
    },
  },
  methods: {
    async open(event: Event) {
      if (!(event.target as HTMLDetailsElement).open || this.opened) {
        return;
      }
      this.opened = true;

      if (this.snapshot.Type == "memo") {
        try {
          const resp = await fetch(`/api/memo/${this.snapshot.ID}`);
          this.memoText = (await resp.json()).Text;
        } catch (e) {
          this.memoText = `Error: ${e instanceof Error ? e.message : e}`;
        }
      }
    },
  },
});
</script>

<style scoped lang="scss">
.card {
  border: 1px solid #ccc;
  margin-bottom: 0.5em;
}

summary {
  display: flex;
  align-items: center;
  cursor: pointer;
  padding: 0.6em 1em;
  background-color: #f4f4f4;

  .label {
    font-weight: bold;
    margin-right: 1em;
  }

  .time {
    color: #666;
    margin-right: 1em;
  }

  .status {
    margin-right: 1em;
  }

  .detail {
    margin-left: auto;
  }
}

.body {
  padding: 0.5em 1em;

  iframe {
    width: 100%;
    height: 60vh;
    border: 0;
  }

  pre {
    white-space: pre-wrap;
  }
}
</style>
