<script setup lang="ts">
import { ref } from 'vue'
import { ApiError, loginWithPassword, type Me } from '@/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

defineProps<{ message?: string }>()
const emit = defineEmits<{ signedIn: [me: Me] }>()

const email = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    emit('signedIn', await loginWithPassword(email.value, password.value))
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : 'Could not sign you in. Please try again.'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="flex min-h-screen items-center justify-center bg-muted/40 p-6">
    <div class="w-full max-w-sm rounded-xl border bg-card p-8 text-center shadow-sm">
      <h1 class="text-xl font-semibold tracking-tight">FreeSupp</h1>
      <p class="mt-1 text-sm text-muted-foreground">Support inbox for your team.</p>

      <p v-if="message" class="mt-4 rounded-md bg-destructive/10 p-3 text-sm text-destructive">
        {{ message }}
      </p>

      <form class="mt-6 space-y-3 text-left" @submit.prevent="submit">
        <Input
          v-model="email"
          type="email"
          autocomplete="email"
          placeholder="you@example.com"
          aria-label="Email"
          required
          data-testid="login-email"
        />
        <Input
          v-model="password"
          type="password"
          autocomplete="current-password"
          placeholder="Password"
          aria-label="Password"
          required
          data-testid="login-password"
        />
        <p v-if="error" class="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
          {{ error }}
        </p>
        <Button type="submit" class="w-full" :disabled="busy" data-testid="password-login">
          Sign in
        </Button>
      </form>

      <p class="mt-4 text-xs text-muted-foreground">
        No account? Ask an admin of this inbox to add you.
      </p>
    </div>
  </main>
</template>
