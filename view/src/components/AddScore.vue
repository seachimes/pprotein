<template>
  <div class="add-score">
    <label>
      スコア
      <input v-model.number="score" type="number" min="0" />
    </label>
    <label class="passed">
      <input v-model="passed" type="checkbox" />
      成功
    </label>
    <button :disabled="sending" @click="submit">スコアを記録</button>
    <span v-if="message" :class="{ error: failed }">{{ message }}</span>
  </div>
</template>

<script lang="ts">
import { defineComponent } from "vue";

export default defineComponent({
  props: {
    groupId: {
      type: String,
      required: true,
    },
  },
  data() {
    return {
      score: 0,
      // Most recorded runs succeed; a failed run is the exception worth
      // unticking, and defaulting to failed would mislabel ordinary entries.
      passed: true,
      sending: false,
      message: "",
      failed: false,
    };
  },
  methods: {
    async submit() {
      this.sending = true;
      this.message = "";
      try {
        const resp = await fetch("/api/score", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            GroupId: this.groupId,
            Score: this.score,
            Passed: this.passed,
          }),
        });
        if (!resp.ok) {
          throw new Error(await resp.text());
        }
        this.failed = false;
        this.message = "記録しました";
      } catch (e) {
        this.failed = true;
        this.message = e instanceof Error ? e.message : String(e);
      } finally {
        this.sending = false;
      }
    },
  },
});
</script>

<style scoped lang="scss">
.add-score {
  margin-top: 1em;
  display: flex;
  align-items: center;
  gap: 1em;

  input[type="number"] {
    inline-size: 8em;
  }
  .passed {
    display: flex;
    align-items: center;
    gap: 0.3em;
  }
  .error {
    color: #c0392b;
  }
}
</style>
