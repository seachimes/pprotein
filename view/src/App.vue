<template>
  <main>
    <header>
      <router-link to="/">{{ $data.title }}</router-link>
      <!-- Which build is actually serving. During a contest several builds get
           deployed in quick succession, and a stale process is easy to mistake
           for a change that did not work. -->
      <span v-if="version" class="version" :title="versionTitle">{{
        version
      }}</span>
    </header>
    <nav>
      <router-link v-slot="{ navigate, isActive }" to="/diag/" custom>
        <div :class="{ active: isActive }" @click="navigate">diag</div>
      </router-link>
      <router-link v-slot="{ navigate, isActive }" to="/diff/" custom>
        <div :class="{ active: isActive }" @click="navigate">diff</div>
      </router-link>
      <router-link v-slot="{ navigate, isActive }" to="/range/" custom>
        <div :class="{ active: isActive }" @click="navigate">range</div>
      </router-link>
      <router-link v-slot="{ navigate, isActive }" to="/group/" custom>
        <div :class="{ active: isActive }" @click="navigate">group</div>
      </router-link>
      <router-link v-slot="{ navigate, isActive }" to="/pprof/" custom>
        <div :class="{ active: isActive }" @click="navigate">pprof</div>
      </router-link>
      <router-link v-slot="{ navigate, isActive }" to="/httplog/" custom>
        <div :class="{ active: isActive }" @click="navigate">httplog</div>
      </router-link>
      <router-link v-slot="{ navigate, isActive }" to="/slowlog/" custom>
        <div :class="{ active: isActive }" @click="navigate">slowlog</div>
      </router-link>
      <router-link v-slot="{ navigate, isActive }" to="/setting/" custom>
        <div :class="{ active: isActive }" @click="navigate">setting</div>
      </router-link>
    </nav>
    <router-view />
  </main>
</template>

<script lang="ts">
import "@fontsource/courier-prime";
import { defineComponent } from "vue";

type Dict = { [key: string]: string };

interface VersionInfo {
  Version: string;
  Revision: string;
  Modified: boolean;
  GoVersion: string;
}

export default defineComponent({
  data() {
    return {
      title: "pprotein ⚙",
      version: "",
      versionTitle: "",
    };
  },
  async mounted() {
    // A failure here must not disturb the page: the version is informational,
    // so it is simply omitted when unavailable.
    try {
      const resp = await fetch("/api/version");
      if (!resp.ok) {
        return;
      }
      const info = (await resp.json()) as VersionInfo;
      this.version = info.Version;
      this.versionTitle = [
        info.Revision ? `revision: ${info.Revision}` : "",
        info.GoVersion ? `built with ${info.GoVersion}` : "",
        info.Modified ? "working tree had uncommitted changes" : "",
      ]
        .filter(Boolean)
        .join("\n");
    } catch {
      // ignore
    }
  },
  watch: {
    $route({ params, meta }) {
      document.title = `${this.getTitle(params, meta)} | ${this.$data.title}`;
    },
  },
  methods: {
    getTitle(params: Dict, meta: Dict) {
      return Object.entries(params).reduce(
        (title, [key, val]) => title.replace(`{{${key}}}`, val),
        meta.title || "",
      );
    },
  },
});
</script>

<style lang="scss">
* {
  box-sizing: border-box;
  font-family: "Courier Prime", monospace;
}
// The app is a fixed-height shell: the header and nav stay put and only the
// content area scrolls. Pinning the document to the viewport keeps the page
// itself from becoming a second scroll container — without this, content taller
// than the window scrolled both the section and the document, which showed up
// as a blank area appearing past the end of the content and as scroll position
// getting stranded between the two.
html,
body {
  height: 100%;
  overflow: hidden;
}
body {
  padding: 0;
  margin: 0;
  font-size: 14px;
}
main {
  display: flex;
  flex-direction: column;
  // 100vw ignores the vertical scrollbar, so on any page tall enough to scroll
  // the layout overflowed horizontally by the scrollbar's width and the browser
  // pinned a horizontal scrollbar across the bottom of the window. 100% is
  // measured against the actual content box and has no such gap.
  height: 100dvh;
  width: 100%;
}

a {
  text-decoration: none;
}

header {
  flex-shrink: 0;
  display: flex;
  align-items: baseline;
  gap: 0.8em;
  padding: 1em 2em;
  background-color: #111;

  a {
    color: #fff;
  }

  // Secondary to the product name: legible when looked for, quiet otherwise.
  .version {
    color: #999;
    font-size: 0.8em;
    cursor: help;
  }
}

nav {
  flex-shrink: 0;
  display: flex;
  overflow: auto;
  background-color: #333;
  color: #fff;

  div {
    cursor: pointer;
    white-space: nowrap;
    padding: 0.7em 2em 0.4em 2em;
    border-bottom: 0.3em solid transparent;
    &.active {
      border-bottom: 0.3em solid orange;
    }
  }
}

section {
  padding: 1em 2em;
  // A flex item defaults to min-height: auto, which lets it grow to fit its
  // content instead of shrinking to the container. That pushed the section past
  // the bottom of main and made the document scroll as well as the section, so
  // the view ended up with two nested scrollbars: scrolling to the end of one
  // jumped into the other and left a blank strip behind.
  min-height: 0;
  // Reaching either end must not chain the scroll outwards.
  overscroll-behavior: contain;
}

button {
  padding: 0.4em 1em;
  background-color: white;
  border: 1px solid lightgray;
  cursor: pointer;

  &:hover {
    border-color: orangered;
  }

  &:active {
    color: white;
    background-color: orangered;
  }
}
</style>
