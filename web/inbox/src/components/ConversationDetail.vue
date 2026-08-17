<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ConversationDetail } from '@/api'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { displayName, toBubbles } from '@/presentation'
import { cn } from '@/lib/utils'

const props = defineProps<{
  conversation: ConversationDetail | null
  loading: boolean
  error: string
  sending: boolean
  // A callback rather than an emit: send() has to await the outcome before it
  // may touch the draft.
  reply: (message: string) => Promise<boolean>
}>()

const emit = defineEmits<{
  archive: [archived: boolean]
  close: []
}>()

const draft = ref('')

// A draft belongs to one conversation; switching rows must not carry it over.
watch(
  () => props.conversation?.id,
  () => {
    draft.value = ''
  },
)

const bubbles = computed(() =>
  props.conversation
    ? toBubbles(props.conversation.messages, props.conversation)
    : [],
)
const canSend = computed(() => draft.value.trim().length > 0 && !props.sending)
const archived = computed(() => props.conversation?.status === 'archived')

async function send() {
  if (!canSend.value) return
  // Clear only once the reply is stored; a rejected send must not throw away
  // what the operator typed.
  if (await props.reply(draft.value)) draft.value = ''
}

/** Cmd/Ctrl+Enter sends; plain Enter stays a newline. */
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
    e.preventDefault()
    void send()
  }
}
</script>

<template>
  <section class="flex h-full flex-col bg-background" data-testid="conversation-detail">
    <!-- With no conversation to render the footer, this is the only place an
         error (a 404 on opening a row) can be shown. -->
    <p
      v-if="!conversation && !loading && error"
      class="m-auto text-sm text-destructive"
      data-testid="detail-error"
    >
      {{ error }}
    </p>

    <p v-else-if="!conversation && !loading" class="m-auto text-sm text-muted-foreground">
      Select a conversation to read it.
    </p>

    <template v-else-if="conversation">
      <header class="flex items-start gap-3 border-b px-5 py-4">
        <div class="min-w-0 flex-1">
          <h2 class="truncate text-sm font-semibold">{{ displayName(conversation) }}</h2>
          <p class="truncate text-xs text-muted-foreground">{{ conversation.visitor_email }}</p>
        </div>
        <Button variant="outline" size="sm" @click="emit('archive', !archived)">
          {{ archived ? 'Unarchive' : 'Archive' }}
        </Button>
        <Button variant="ghost" size="sm" class="lg:hidden" @click="emit('close')">Close</Button>
      </header>

      <div class="flex-1 space-y-3 overflow-y-auto px-5 py-4">
        <div
          v-for="bubble in bubbles"
          :key="bubble.id"
          :class="cn('flex flex-col', bubble.side === 'operator' ? 'items-end' : 'items-start')"
          :data-side="bubble.side"
        >
          <p v-if="bubble.showAuthor" class="mb-1 text-xs text-muted-foreground">
            {{ bubble.author }}
          </p>
          <div
            :class="
              cn(
                'max-w-[85%] whitespace-pre-wrap break-words rounded-lg px-3 py-2 text-sm',
                bubble.side === 'operator'
                  ? 'bg-primary text-primary-foreground'
                  : 'bg-muted text-foreground',
              )
            "
          >
            {{ bubble.body }}
          </div>
          <p class="mt-1 text-[11px] text-muted-foreground">{{ bubble.time }}</p>
        </div>
      </div>

      <footer class="border-t px-5 py-4">
        <p v-if="error" class="mb-2 text-sm text-destructive" data-testid="detail-error">
          {{ error }}
        </p>
        <Textarea
          v-model="draft"
          rows="3"
          placeholder="Write a reply…"
          data-testid="reply-input"
          :disabled="sending"
          @keydown="onKeydown"
        />
        <div class="mt-2 flex items-center justify-between">
          <p class="text-xs text-muted-foreground">
            {{ archived ? 'This conversation is archived.' : '' }}
          </p>
          <div class="flex items-center gap-3">
            <span class="text-xs text-muted-foreground">⌘/Ctrl + Enter</span>
            <Button size="sm" :disabled="!canSend" data-testid="send-reply" @click="send">
              {{ sending ? 'Sending…' : 'Send' }}
            </Button>
          </div>
        </div>
      </footer>
    </template>
  </section>
</template>
