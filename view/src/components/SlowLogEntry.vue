<template>
  <TsvPanel endpoint="slowlog" :id="id" />
</template>

<script lang="ts">
import { defineComponent } from "vue";
import TsvPanel from "./TsvPanel.vue";

export default defineComponent({
  components: {
    TsvPanel,
  },
  computed: {
    id() {
      const id = this.$route.params.id;
      return typeof id == "string" ? id : id[0];
    },
  },
});
</script>
