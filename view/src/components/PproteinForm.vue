<template>
  <div class="form">
    <label>
      Source URL<br />
      <input
        :value="$data.url"
        type="text"
        size="50"
        @input="edit('url', $event)"
      />
    </label>
    <label>
      Duration<br />
      <input
        :value="$data.duration"
        type="number"
        size="10"
        @input="edit('duration', $event)"
      />
    </label>
    <label>
      &nbsp;<br />
      <button @click="collect">Collect</button>
    </label>
  </div>
</template>

<script lang="ts">
import { defineComponent } from "vue";
import { addCollectJob } from "../collect";

export default defineComponent({
  props: {
    endpoint: {
      type: String,
      required: true,
    },
  },
  data(): { url: string; duration: string } {
    return {
      url: this.stored("url") ?? "http://",
      duration: this.stored("duration") ?? "60",
    };
  },
  computed: {
    // The target configured for this endpoint under "group/targets", which is
    // what the group Collect uses. Settings load asynchronously.
    configured(): { URL: string; Duration: number } | undefined {
      try {
        const raw = this.$store.state.settings["group/targets"]?.value;
        const targets = JSON.parse(raw || "[]") as {
          Type: string;
          URL: string;
          Duration: number;
        }[];
        return targets.find((t) => t.Type === this.$props.endpoint);
      } catch {
        return undefined;
      }
    },
  },
  watch: {
    // A value the user typed wins; the configured target only fills what they
    // have not touched. It is deliberately not written to localStorage, so a
    // later change to the targets still shows up.
    configured: {
      immediate: true,
      handler(t) {
        if (!t) {
          return;
        }
        if (this.stored("url") === null) {
          this.$data.url = t.URL;
        }
        if (this.stored("duration") === null) {
          this.$data.duration = String(t.Duration);
        }
      },
    },
  },
  methods: {
    stored(key: "url" | "duration") {
      return localStorage.getItem(`${key}[${this.$props.endpoint}]`);
    },
    edit(key: "url" | "duration", e: Event) {
      const val = (e.target as HTMLInputElement).value;
      this.$data[key] = val;
      localStorage.setItem(`${key}[${this.$props.endpoint}]`, val);
    },
    async collect() {
      await addCollectJob(this.$props.endpoint, {
        GroupId: "",
        Label: "",
        URL: this.$data.url,
        Duration: parseInt(this.$data.duration),
      });
    },
  },
});
</script>

<style scoped lang="scss">
.form {
  display: flex;
  margin-bottom: 2em;

  label {
    margin-right: 1em;

    &:focus-within {
      color: orangered;
    }

    input {
      border: 1px solid lightgray;
      padding: 0.4em 1em;

      &:focus {
        border-color: orangered;
        outline: 0;
      }
    }
  }
}
</style>
