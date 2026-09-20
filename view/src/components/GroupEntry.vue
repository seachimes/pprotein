<template>
  <div class="container">
    <nav>
      <router-link
        v-slot="{ navigate, isActive }"
        :to="`/group/${$route.params.gid}/index/`"
        custom
      >
        <div :class="{ active: isActive }" @click="navigate">index</div>
      </router-link>
      <router-link
        v-for="entry in $store.getters.availableEntriesByGroup(
          $route.params.gid,
        )"
        v-slot="{ navigate, isActive }"
        :key="entry.Snapshot.ID"
        :to="`/group/${$route.params.gid}/${entry.Snapshot.Type}/${entry.Snapshot.ID}/`"
        custom
      >
        <div :class="{ active: isActive }" @click="navigate">
          {{ entry.Snapshot.Type }}: {{ entry.Snapshot.Label }}
        </div>
      </router-link>
    </nav>
    <router-view />
  </div>
</template>

<style scoped lang="scss">
.container {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  // This container is itself a flex item of main. Without this it grows to fit
  // the child view instead of shrinking to the space available, which pushes
  // the content past the bottom of the window.
  min-height: 0;
}

nav {
  // Keep the group's own tab strip at its natural height so it is not squeezed
  // when the content below is tall.
  flex-shrink: 0;
  background-color: #555;
}
</style>
