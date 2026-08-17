<script setup lang="ts">
import { onMounted, ref, useTemplateRef } from 'vue'
import { ApiError, fetchConfig, submitMessage } from '../api'
import { loadTurnstile } from '../turnstile'
import { hasErrors, validateForm, type FormErrors } from '../validation'

const values = ref({ email: '', name: '', message: '' })
const errors = ref<FormErrors>({})
const serverError = ref('')
const submitting = ref(false)
const sent = ref(false)

const siteKey = ref('')
const captchaToken = ref('')
const captchaError = ref('')
const captchaBox = useTemplateRef<HTMLDivElement>('captchaBox')
// Turnstile tokens are single-use, so a failed submission has to reset the
// widget before the visitor can try again.
let captchaWidgetId: string | null = null
let turnstileAPI: { reset: (id?: string) => void } | null = null

const embedded = typeof window !== 'undefined' && window.parent !== window

onMounted(async () => {
  const cfg = await fetchConfig()
  if (!cfg.turnstileSiteKey) return
  siteKey.value = cfg.turnstileSiteKey
  try {
    const turnstile = await loadTurnstile()
    if (!captchaBox.value) return
    turnstileAPI = turnstile
    captchaWidgetId = turnstile.render(captchaBox.value, {
      sitekey: cfg.turnstileSiteKey,
      callback: (t: string) => {
        captchaToken.value = t
        captchaError.value = ''
      },
      'expired-callback': () => (captchaToken.value = ''),
      'error-callback': () => (captchaToken.value = ''),
    })
  } catch {
    captchaError.value = 'The captcha could not be loaded. Please reload the page.'
  }
})

async function submit() {
  if (submitting.value) return
  serverError.value = ''
  errors.value = validateForm(values.value)
  if (hasErrors(errors.value)) return
  if (siteKey.value && !captchaToken.value) {
    captchaError.value = 'Please complete the captcha.'
    return
  }

  submitting.value = true
  try {
    await submitMessage({ ...values.value, turnstileToken: captchaToken.value })
    sent.value = true
  } catch (err) {
    serverError.value =
      err instanceof ApiError ? err.message : 'Something went wrong. Please try again.'
    resetCaptcha()
  } finally {
    submitting.value = false
  }
}

/** The consumed token is worthless; ask Turnstile for a fresh challenge. */
function resetCaptcha() {
  captchaToken.value = ''
  if (turnstileAPI && captchaWidgetId !== null) {
    turnstileAPI.reset(captchaWidgetId)
  }
}

function close() {
  window.parent.postMessage({ source: 'freesupp', type: 'close' }, '*')
}
</script>

<template>
  <div class="fs-page">
    <header class="fs-header">
      <h1>Contact support</h1>
    </header>

    <div class="fs-body">
      <div v-if="sent" class="fs-success">
        <h2>Thanks — we got your message.</h2>
        <p>We'll reply to <strong>{{ values.email.trim() }}</strong>.</p>
        <button v-if="embedded" type="button" class="fs-button" @click="close">Close</button>
      </div>

      <form v-else novalidate @submit.prevent="submit">
        <p v-if="serverError" class="fs-alert" role="alert">{{ serverError }}</p>

        <div class="fs-field">
          <label for="fs-email">Email</label>
          <input
            id="fs-email"
            v-model="values.email"
            type="email"
            autocomplete="email"
            :aria-invalid="!!errors.email"
          />
          <p v-if="errors.email" class="fs-error">{{ errors.email }}</p>
        </div>

        <div class="fs-field">
          <label for="fs-name">Name <span class="fs-optional">(optional)</span></label>
          <input id="fs-name" v-model="values.name" type="text" autocomplete="name" />
          <p v-if="errors.name" class="fs-error">{{ errors.name }}</p>
        </div>

        <div class="fs-field">
          <label for="fs-message">Message</label>
          <textarea id="fs-message" v-model="values.message" :aria-invalid="!!errors.message" />
          <p v-if="errors.message" class="fs-error">{{ errors.message }}</p>
        </div>

        <div v-show="siteKey" class="fs-field">
          <div ref="captchaBox" />
          <p v-if="captchaError" class="fs-error">{{ captchaError }}</p>
        </div>

        <button type="submit" class="fs-button fs-button--wide" :disabled="submitting">
          {{ submitting ? 'Sending…' : 'Send message' }}
        </button>
      </form>
    </div>
  </div>
</template>
