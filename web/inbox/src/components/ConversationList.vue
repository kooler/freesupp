<script setup lang="ts">
import { computed } from 'vue'
import type { ConversationSummary } from '@/api'
import { initials, toRows } from '@/presentation'
import { cn } from '@/lib/utils'

const props = defineProps<{
  conversations: ConversationSummary[]
  activeId: number | null
  loading: boolean
}>()

const emit = defineEmits<{ select: [id: number] }>()

const rows = computed(() => toRows(props.conversations))
</script>

<template>
  <div class="h-full overflow-y-auto" data-testid="conversation-list">
    <p v-if="loading && rows.length === 0" class="p-6 text-sm text-muted-foreground">Loading…</p>
    <p v-else-if="rows.length === 0" class="p-6 text-sm text-muted-foreground">
      Nothing here yet.
    </p>

    <ul v-else class="divide-y">
      <li v-for="row in rows" :key="row.id">
        <button
          type="button"
          :data-testid="`row-${row.id}`"
          :data-unread="row.unread ? 'true' : 'false'"
          :aria-current="row.id === activeId ? 'true' : undefined"
          :class="
            cn(
              'flex w-full items-start gap-3 px-4 py-3 text-left transition-colors hover:bg-accent/60',
              row.id === activeId && 'bg-accent',
            )
          "
          @click="emit('select', row.id)"
        >
          <span
            class="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-full bg-secondary text-xs font-medium text-secondary-foreground"
            aria-hidden="true"
          >
            {{ initials(row.name) }}
          </span>

          <span class="min-w-0 flex-1">
            <span class="flex items-baseline gap-2">
              <span :class="cn('truncate text-sm', row.unread ? 'font-semibold' : 'font-medium')">
                {{ row.name }}
              </span>
              <span class="ml-auto shrink-0 text-xs text-muted-foreground">{{ row.time }}</span>
              <span
                v-if="row.unread"
                class="size-2 shrink-0 rounded-full bg-primary"
                data-testid="unread-dot"
              >
                <span class="sr-only">unread</span>
              </span>
            </span>
            <span
              :class="
                cn(
                  'mt-0.5 block truncate text-sm',
                  row.unread ? 'font-medium text-foreground' : 'text-muted-foreground',
                )
              "
            >
              <span v-if="row.fromOperator" class="text-muted-foreground">You: </span>{{
                row.snippet
              }}
            </span>
          </span>
        </button>
      </li>
    </ul>
  </div>
</template>
