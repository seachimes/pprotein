<template>
  <TsvTable :tsv="tsv" :link="`/api/${$props.endpoint}/data/${$props.id}`" />
</template>

<script lang="ts">
import { defineComponent } from "vue";
import TsvTable from "./TsvTable.vue";

export default defineComponent({
  components: {
    TsvTable,
  },
  props: {
    endpoint: {
      type: String,
      required: true,
    },
    id: {
      type: String,
      required: true,
    },
  },
  data() {
    return {
      tsv: "",
    };
  },
  watch: {
    id: {
      handler: "updateTsv",
      immediate: true,
    },
  },
  methods: {
    async updateTsv() {
      const resp = await fetch(
        `/api/${this.$props.endpoint}/${this.$props.id}`,
      );
      this.tsv = await resp.text();
    },
  },
});
</script>
