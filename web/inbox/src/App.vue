<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { fetchAuthConfig, fetchMe, onUnauthorized, type AuthConfig, type Me } from '@/api'
import InboxView from '@/views/InboxView.vue'
import LoginScreen from '@/views/LoginScreen.vue'
import SettingsScreen from '@/views/SettingsScreen.vue'
import SetupScreen from '@/views/SetupScreen.vue'

const me = ref<Me | null>(null)
const authConfig = ref<AuthConfig | null>(null)
const ready = ref(false)
const notice = ref('')
const settingsOpen = ref(false)

// Any request may discover the session is gone — polls included.
onUnauthorized(() => {
  if (me.value === null) return
  me.value = null
  settingsOpen.value = false
  notice.value = 'Your session expired. Please sign in again.'
})

onMounted(async () => {
  try {
    const [user, config] = await Promise.all([fetchMe(), fetchAuthConfig()])
    me.value = user
    authConfig.value = config
  } catch {
    notice.value = 'Could not reach the server. Please try again.'
  } finally {
    ready.value = true
  }
})

function signedIn(user: Me) {
  me.value = user
  notice.value = ''
  // Setup can only ever succeed once; signing in proves it happened.
  if (authConfig.value) authConfig.value = { ...authConfig.value, setup_required: false }
}

function signedOut() {
  me.value = null
  settingsOpen.value = false
  notice.value = ''
}
</script>

<template>
  <div v-if="!ready" class="flex min-h-screen items-center justify-center text-sm text-muted-foreground">
    Loading…
  </div>
  <SettingsScreen v-else-if="me && settingsOpen" :me="me" @close="settingsOpen = false" />
  <InboxView
    v-else-if="me"
    :me="me"
    @signed-out="signedOut"
    @open-settings="settingsOpen = true"
  />
  <SetupScreen v-else-if="authConfig?.setup_required" @signed-in="signedIn" />
  <LoginScreen v-else :message="notice" @signed-in="signedIn" />
</template>
