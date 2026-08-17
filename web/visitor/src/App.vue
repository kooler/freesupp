<script setup lang="ts">
import { resolveRoute } from './route'
import ThreadPage from './views/ThreadPage.vue'
import WidgetForm from './views/WidgetForm.vue'

// The visitor app never navigates client-side: the widget iframe opens the
// form, magic links open a thread. One resolve at startup is enough.
const route = resolveRoute(location.pathname)
</script>

<template>
  <ThreadPage v-if="route.name === 'thread'" :token="route.token" />
  <WidgetForm v-else />
</template>
