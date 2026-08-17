<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import {
  ApiError,
  getConversation,
  listConversations,
  logout,
  replyToConversation,
  setArchived,
  type ConversationDetail as Detail,
  type ConversationStatus,
  type ConversationSummary,
  type Me,
} from '@/api'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import ConversationList from '@/components/ConversationList.vue'
import ConversationDetail from '@/components/ConversationDetail.vue'
import { usePolling } from '@/composables/usePolling'
import { pageTitle, unreadCount } from '@/presentation'
import { conversationFromPath, conversationPathFor } from '@/route'

const props = defineProps<{ me: Me }>()
const emit = defineEmits<{ signedOut: []; openSettings: [] }>()

const listRefreshMs = 20_000

const status = ref<ConversationStatus>('open')
const conversations = ref<ConversationSummary[]>([])
const listLoading = ref(true)
const listError = ref('')

const activeId = ref<number | null>(conversationFromPath(location.pathname))
const active = ref<Detail | null>(null)
const detailLoading = ref(false)
const detailError = ref('')
const sending = ref(false)

const unread = computed(() => unreadCount(conversations.value))

watch(unread, (n) => {
  document.title = pageTitle(n)
})

async function loadList() {
  try {
    conversations.value = await listConversations(status.value)
    listError.value = ''
  } catch (err) {
    listError.value = err instanceof ApiError ? err.message : 'Could not load conversations.'
  } finally {
    listLoading.value = false
  }
}

async function loadActive() {
  const id = activeId.value
  if (id === null) {
    active.value = null
    detailError.value = ''
    detailLoading.value = false
    return
  }
  detailLoading.value = true
  try {
    const detail = await getConversation(id)
    // A slower response for a row the operator already left must not win.
    if (activeId.value !== id) return
    active.value = detail
    detailError.value = ''
    markReadLocally(id)
  } catch (err) {
    if (activeId.value !== id) return
    // A failed refresh (this runs on every window focus) must keep the thread
    // on screen: dropping it would clear the reply the operator is typing.
    if (err instanceof ApiError && err.notFound) {
      active.value = null
      detailError.value = 'This conversation no longer exists.'
    } else {
      detailError.value = 'Could not load this conversation.'
    }
  } finally {
    // The response for a row the operator already left must not stop the
    // spinner for the one they are waiting on.
    if (activeId.value === id) detailLoading.value = false
  }
}

/** the server clears unread on open; mirror it without refetching the list. */
function markReadLocally(id: number) {
  const row = conversations.value.find((c) => c.id === id)
  if (row) row.unread = false
}

function select(id: number) {
  if (activeId.value === id) {
    // The detail pane otherwise only reloads on tab focus, which never fires
    // for an operator sitting on the thread. Re-clicking the row refreshes it.
    void loadActive()
    return
  }
  activeId.value = id
  history.pushState(null, '', conversationPathFor(id))
  void loadActive()
}

function closeDetail() {
  activeId.value = null
  active.value = null
  // An in-flight request for the row being closed no longer clears these.
  detailError.value = ''
  detailLoading.value = false
  history.pushState(null, '', conversationPathFor(null))
}

/** Reports whether the reply was stored, so the detail pane can keep the draft. */
async function reply(message: string): Promise<boolean> {
  const id = activeId.value
  if (id === null) return false
  sending.value = true
  try {
    await replyToConversation(id, message)
    detailError.value = ''
    await Promise.all([loadActive(), loadList()])
    return true
  } catch (err) {
    detailError.value = err instanceof ApiError ? err.message : 'Could not send your reply.'
    return false
  } finally {
    sending.value = false
  }
}

async function archive(archived: boolean) {
  const id = activeId.value
  if (id === null) return
  let updated: Detail
  try {
    updated = await setArchived(id, archived)
  } catch (err) {
    if (activeId.value !== id) return
    detailError.value = err instanceof ApiError ? err.message : 'Could not update this conversation.'
    return
  }
  // The operator may have opened another row while this was in flight; its
  // messages must not end up under this conversation's header.
  if (activeId.value !== id) return
  active.value = { ...updated, messages: active.value?.messages ?? [] }
  detailError.value = ''
  // The row belongs to the other tab now.
  await loadList()
  if (activeId.value === id && !conversations.value.some((c) => c.id === id)) closeDetail()
}

async function signOut() {
  try {
    await logout()
  } finally {
    emit('signedOut')
  }
}

watch(status, () => {
  listLoading.value = true
  void loadList()
})

const listPolling = usePolling(loadList, { intervalMs: listRefreshMs, onFocus: true })
// The open conversation is refreshed only when the operator comes back to the
// tab — polling it on a timer would fight with the reply box.
const detailPolling = usePolling(loadActive, { intervalMs: 0, onFocus: true })

onMounted(() => {
  void loadList()
  void loadActive()
  listPolling.start()
  detailPolling.start()
  window.addEventListener('popstate', onPopState)
})

// popstate lives on window, so it outlives the component unless removed.
onUnmounted(() => window.removeEventListener('popstate', onPopState))

function onPopState() {
  activeId.value = conversationFromPath(location.pathname)
  void loadActive()
}
</script>

<template>
  <div class="flex h-screen flex-col">
    <header class="flex items-center gap-4 border-b px-5 py-3">
      <h1 class="text-sm font-semibold tracking-tight">FreeSupp</h1>
      <Tabs
        :model-value="status"
        class="ml-2"
        @update:model-value="(v) => (status = v as ConversationStatus)"
      >
        <TabsList>
          <TabsTrigger value="open">Open</TabsTrigger>
          <TabsTrigger value="archived">Archived</TabsTrigger>
        </TabsList>
      </Tabs>
      <span class="ml-auto truncate text-sm text-muted-foreground">{{ props.me.email }}</span>
      <Button variant="ghost" size="sm" data-testid="open-settings" @click="emit('openSettings')">
        Settings
      </Button>
      <Button variant="ghost" size="sm" data-testid="logout" @click="signOut">Sign out</Button>
    </header>

    <div class="flex min-h-0 flex-1">
      <aside
        :class="[
          'w-full shrink-0 border-r lg:w-96',
          activeId !== null ? 'hidden lg:block' : 'block',
        ]"
      >
        <p v-if="listError" class="border-b bg-destructive/10 p-3 text-sm text-destructive">
          {{ listError }}
        </p>
        <ConversationList
          :conversations="conversations"
          :active-id="activeId"
          :loading="listLoading"
          @select="select"
        />
      </aside>

      <main :class="['min-w-0 flex-1', activeId === null ? 'hidden lg:block' : 'block']">
        <ConversationDetail
          :conversation="active"
          :loading="detailLoading"
          :error="detailError"
          :sending="sending"
          :reply="reply"
          @archive="archive"
          @close="closeDetail"
        />
      </main>
    </div>
  </div>
</template>
