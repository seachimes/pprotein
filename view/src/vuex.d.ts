import store from "./store";

// vuex 4 does not declare $store itself; the application has to augment Vue.
// Both specifiers are augmented because "vue" re-exports @vue/runtime-core and
// which one a component instance resolves through depends on the Vue version.
declare module "vue" {
  interface ComponentCustomProperties {
    $store: typeof store;
  }
}

declare module "@vue/runtime-core" {
  interface ComponentCustomProperties {
    $store: typeof store;
  }
}
