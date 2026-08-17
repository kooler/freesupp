<script setup lang="ts">
import { ref } from 'vue'
import { ApiError, setupAccount, type Me } from '@/api'
import { passwordProblem } from '@/password'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

const emit = defineEmits<{ signedIn: [me: Me] }>()

const email = ref('')
const password = ref('')
const confirmation = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  if (busy.value) return
  const problem = passwordProblem(password.value)
  if (problem) {
    error.value = problem
    return
  }
  if (password.value !== confirmation.value) {
    error.value = 'The passwords do not match.'
    return
  }
  busy.value = true
  error.value = ''
  try {
    emit('signedIn', await setupAccount(email.value, password.value))
  } catch (err) {
    error.value =
      err instanceof ApiError ? err.message : 'Could not create the account. Please try again.'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="flex min-h-screen items-center justify-center bg-muted/40 p-6">
    <div class="w-full max-w-sm rounded-xl border bg-card p-8 shadow-sm">
      <h1 class="text-center text-xl font-semibold tracking-tight">Welcome to FreeSupp</h1>
      <p class="mt-1 text-center text-sm text-muted-foreground">
        This inbox has no users yet. Create the first account — it becomes the admin.
      </p>

      <form class="mt-6 space-y-3" @submit.prevent="submit">
        <Input
          v-model="email"
          type="email"
          autocomplete="email"
          placeholder="you@example.com"
          aria-label="Email"
          required
          data-testid="setup-email"
        />
        <Input
          v-model="password"
          type="password"
          autocomplete="new-password"
          placeholder="Password"
          aria-label="Password"
          required
          data-testid="setup-password"
        />
        <Input
          v-model="confirmation"
          type="password"
          autocomplete="new-password"
          placeholder="Confirm password"
          aria-label="Confirm password"
          required
          data-testid="setup-confirmation"
        />
        <p class="text-xs text-muted-foreground">
          At least 8 characters, with an uppercase letter, a lowercase letter and a digit.
        </p>
        <p v-if="error" class="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
          {{ error }}
        </p>
        <Button type="submit" class="w-full" :disabled="busy" data-testid="setup-submit">
          Create account
        </Button>
      </form>
    </div>
  </main>
</template>
