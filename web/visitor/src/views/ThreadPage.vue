<script setup lang="ts">
import { computed, nextTick, onMounted, ref, useTemplateRef } from 'vue'
import { ApiError, fetchThread, replyToThread, type Thread } from '../api'
import { isArchived, toBubbles } from '../thread'
import { validateMessage } from '../validation'

const props = defineProps<{ token: string }>()

const token = ref(props.token)
const thread = ref<Thread | null>(null)
const loading = ref(true)
const loadError = ref('')
const invalid = ref(false)

const body = ref('')
const replyError = ref('')
const sending = ref(false)
const scroller = useTemplateRef<HTMLDivElement>('scroller')

const bubbles = computed(() => (thread.value ? toBubbles(thread.value) : []))
const archived = computed(() => isArchived(thread.value))

onMounted(load)

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    thread.value = await fetchThread(token.value)
    invalid.value = false
    await scrollToEnd()
  } catch (err) {
    if (err instanceof ApiError && err.notFound) invalid.value = true
    else loadError.value = err instanceof ApiError ? err.message : 'Could not load this conversation.'
  } finally {
    loading.value = false
  }
}

async function send() {
  if (sending.value) return
  const problem = validateMessage(body.value)
  replyError.value = problem ?? ''
  if (problem) return

  sending.value = true
  try {
    const next = await replyToThread(token.value, body.value)
    body.value = ''
    // An archived thread starts a new conversation, so follow the new token.
    if (next !== token.value) {
      token.value = next
      history.replaceState(null, '', `/t/${encodeURIComponent(next)}`)
    }
    await load()
  } catch (err) {
    replyError.value = err instanceof ApiError ? err.message : 'Could not send your message.'
  } finally {
    sending.value = false
  }
}

async function scrollToEnd() {
  await nextTick()
  if (scroller.value) scroller.value.scrollTop = scroller.value.scrollHeight
}
</script>

<template>
  <div class="fs-page">
    <header class="fs-header">
      <h1>Your support conversation</h1>
      <p v-if="thread">{{ thread.visitor_email }}</p>
    </header>

    <div ref="scroller" class="fs-body">
      <p v-if="loading" class="fs-state">Loading…</p>
      <p v-else-if="invalid" class="fs-state">
        This conversation link is not valid. It may have been mistyped — please use the link from
        your latest email.
      </p>
      <p v-else-if="loadError" class="fs-state">{{ loadError }}</p>

      <div v-else class="fs-bubbles">
        <div
          v-for="b in bubbles"
          :key="b.id"
          class="fs-bubble"
          :class="`fs-bubble--${b.side}`"
        >
          <div v-if="b.showAuthor" class="fs-bubble__author">{{ b.author }}</div>
          <p class="fs-bubble__body">{{ b.body }}</p>
          <span class="fs-bubble__time">{{ b.time }}</span>
        </div>
      </div>
    </div>

    <form v-if="!loading && !invalid" class="fs-reply" @submit.prevent="send">
      <p v-if="archived" class="fs-note">
        This conversation was closed. Sending a message starts a new one.
      </p>
      <textarea
        v-model="body"
        placeholder="Write a reply…"
        :aria-invalid="!!replyError"
        @keydown.enter.meta.prevent="send"
        @keydown.enter.ctrl.prevent="send"
      />
      <div class="fs-reply__actions">
        <span class="fs-hint" :class="{ 'fs-error': replyError }">
          {{ replyError || 'We reply by email too.' }}
        </span>
        <button type="submit" class="fs-button" :disabled="sending">
          {{ sending ? 'Sending…' : 'Send' }}
        </button>
      </div>
    </form>
  </div>
</template>
