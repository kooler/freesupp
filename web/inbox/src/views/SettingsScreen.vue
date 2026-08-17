<script setup lang="ts">
import { onMounted, ref } from 'vue'
import {
  ApiError,
  changeMyPassword,
  createUser,
  deleteUser,
  listUsers,
  resetUserPassword,
  setUserAdmin,
  type InboxUser,
  type Me,
} from '@/api'
import { passwordProblem } from '@/password'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

const props = defineProps<{ me: Me }>()
const emit = defineEmits<{ close: [] }>()

function messageOf(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback
}

// --- own password -----------------------------------------------------------

const currentPassword = ref('')
const newPassword = ref('')
const newConfirmation = ref('')
const passwordError = ref('')
const passwordChanged = ref(false)
const changingPassword = ref(false)

async function submitPassword() {
  if (changingPassword.value) return
  passwordChanged.value = false
  const problem = passwordProblem(newPassword.value)
  if (problem) {
    passwordError.value = problem
    return
  }
  if (newPassword.value !== newConfirmation.value) {
    passwordError.value = 'The passwords do not match.'
    return
  }
  changingPassword.value = true
  passwordError.value = ''
  try {
    await changeMyPassword(currentPassword.value, newPassword.value)
    passwordChanged.value = true
    currentPassword.value = ''
    newPassword.value = ''
    newConfirmation.value = ''
  } catch (err) {
    passwordError.value = messageOf(err, 'Could not change your password. Please try again.')
  } finally {
    changingPassword.value = false
  }
}

// --- team management (admins only) -----------------------------------------

const users = ref<InboxUser[]>([])
const usersError = ref('')
const usersLoading = ref(false)

const newUserEmail = ref('')
const newUserPassword = ref('')
const newUserIsAdmin = ref(false)
const addError = ref('')
const adding = ref(false)

// The row whose inline password-reset form is open, if any.
const resettingId = ref<number | null>(null)
const resetPassword = ref('')

// The row awaiting delete confirmation, if any.
const confirmingDeleteId = ref<number | null>(null)

async function loadUsers() {
  usersLoading.value = true
  try {
    users.value = await listUsers()
    usersError.value = ''
  } catch (err) {
    usersError.value = messageOf(err, 'Could not load users.')
  } finally {
    usersLoading.value = false
  }
}

async function addUser() {
  if (adding.value) return
  const problem = passwordProblem(newUserPassword.value)
  if (problem) {
    addError.value = problem
    return
  }
  adding.value = true
  addError.value = ''
  try {
    users.value.push(await createUser(newUserEmail.value, newUserPassword.value, newUserIsAdmin.value))
    newUserEmail.value = ''
    newUserPassword.value = ''
    newUserIsAdmin.value = false
  } catch (err) {
    addError.value = messageOf(err, 'Could not create the user. Please try again.')
  } finally {
    adding.value = false
  }
}

async function toggleAdmin(user: InboxUser) {
  try {
    const updated = await setUserAdmin(user.id, !user.is_admin)
    users.value = users.value.map((u) => (u.id === updated.id ? updated : u))
    usersError.value = ''
  } catch (err) {
    usersError.value = messageOf(err, 'Could not update the user.')
  }
}

function askDelete(user: InboxUser) {
  confirmingDeleteId.value = confirmingDeleteId.value === user.id ? null : user.id
  resettingId.value = null
  usersError.value = ''
}

async function removeUser(user: InboxUser) {
  try {
    await deleteUser(user.id)
    users.value = users.value.filter((u) => u.id !== user.id)
    confirmingDeleteId.value = null
    usersError.value = ''
  } catch (err) {
    usersError.value = messageOf(err, 'Could not delete the user.')
  }
}

function openReset(user: InboxUser) {
  resettingId.value = resettingId.value === user.id ? null : user.id
  confirmingDeleteId.value = null
  resetPassword.value = ''
  usersError.value = ''
}

async function submitReset(user: InboxUser) {
  const problem = passwordProblem(resetPassword.value)
  if (problem) {
    usersError.value = problem
    return
  }
  try {
    await resetUserPassword(user.id, resetPassword.value)
    resettingId.value = null
    resetPassword.value = ''
    usersError.value = ''
  } catch (err) {
    usersError.value = messageOf(err, 'Could not reset the password.')
  }
}

onMounted(() => {
  if (props.me.is_admin) void loadUsers()
})
</script>

<template>
  <div class="flex h-screen flex-col">
    <header class="flex items-center gap-4 border-b px-5 py-3">
      <h1 class="text-sm font-semibold tracking-tight">Settings</h1>
      <span class="ml-auto truncate text-sm text-muted-foreground">{{ me.email }}</span>
      <Button variant="ghost" size="sm" data-testid="close-settings" @click="emit('close')">
        Back to inbox
      </Button>
    </header>

    <main class="min-h-0 flex-1 overflow-y-auto p-6">
      <div class="mx-auto max-w-2xl space-y-10">
        <section>
          <h2 class="text-base font-semibold">Your password</h2>
          <form class="mt-3 max-w-sm space-y-3" @submit.prevent="submitPassword">
            <Input
              v-model="currentPassword"
              type="password"
              autocomplete="current-password"
              placeholder="Current password"
              aria-label="Current password"
              required
              data-testid="current-password"
            />
            <Input
              v-model="newPassword"
              type="password"
              autocomplete="new-password"
              placeholder="New password"
              aria-label="New password"
              required
              data-testid="new-password"
            />
            <Input
              v-model="newConfirmation"
              type="password"
              autocomplete="new-password"
              placeholder="Confirm new password"
              aria-label="Confirm new password"
              required
              data-testid="new-password-confirmation"
            />
            <p v-if="passwordError" class="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
              {{ passwordError }}
            </p>
            <p v-else-if="passwordChanged" class="text-sm text-muted-foreground">
              Password changed.
            </p>
            <Button type="submit" :disabled="changingPassword" data-testid="change-password">
              Change password
            </Button>
          </form>
        </section>

        <section v-if="me.is_admin">
          <h2 class="text-base font-semibold">Team</h2>
          <p class="mt-1 text-sm text-muted-foreground">
            Users can read and answer conversations; admins can also manage users. Share the
            initial password with the person you add — they can change it here afterwards.
          </p>

          <p v-if="usersError" class="mt-3 rounded-md bg-destructive/10 p-3 text-sm text-destructive">
            {{ usersError }}
          </p>

          <ul v-if="!usersLoading" class="mt-4 divide-y rounded-md border" data-testid="user-list">
            <li v-for="user in users" :key="user.id" class="p-3">
              <div class="flex flex-wrap items-center gap-2">
                <span class="truncate text-sm">{{ user.email }}</span>
                <span
                  v-if="user.is_admin"
                  class="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground"
                >
                  admin
                </span>
                <span v-if="user.email === me.email" class="text-xs text-muted-foreground">
                  (you)
                </span>
                <div class="ml-auto flex gap-1">
                  <Button variant="ghost" size="sm" @click="toggleAdmin(user)">
                    {{ user.is_admin ? 'Remove admin' : 'Make admin' }}
                  </Button>
                  <Button variant="ghost" size="sm" @click="openReset(user)">Reset password</Button>
                  <Button
                    v-if="user.email !== me.email"
                    variant="ghost"
                    size="sm"
                    class="text-destructive"
                    :data-testid="`delete-user-${user.id}`"
                    @click="askDelete(user)"
                  >
                    Delete
                  </Button>
                </div>
              </div>
              <div
                v-if="confirmingDeleteId === user.id"
                class="mt-2 flex flex-wrap items-center gap-2 rounded-md bg-destructive/10 p-2 text-sm"
              >
                <span>Delete {{ user.email }}? They will no longer be able to sign in.</span>
                <div class="ml-auto flex gap-1">
                  <Button
                    variant="ghost"
                    size="sm"
                    class="text-destructive"
                    :data-testid="`confirm-delete-${user.id}`"
                    @click="removeUser(user)"
                  >
                    Delete
                  </Button>
                  <Button variant="ghost" size="sm" @click="confirmingDeleteId = null">
                    Cancel
                  </Button>
                </div>
              </div>
              <form
                v-if="resettingId === user.id"
                class="mt-2 flex gap-2"
                @submit.prevent="submitReset(user)"
              >
                <Input
                  v-model="resetPassword"
                  type="password"
                  autocomplete="new-password"
                  placeholder="New password"
                  :aria-label="`New password for ${user.email}`"
                  required
                />
                <Button type="submit" size="sm">Set</Button>
              </form>
            </li>
          </ul>
          <p v-else class="mt-4 text-sm text-muted-foreground">Loading…</p>

          <form class="mt-6 max-w-sm space-y-3" @submit.prevent="addUser">
            <h3 class="text-sm font-medium">Add user</h3>
            <Input
              v-model="newUserEmail"
              type="email"
              autocomplete="off"
              placeholder="teammate@example.com"
              aria-label="New user email"
              required
              data-testid="new-user-email"
            />
            <Input
              v-model="newUserPassword"
              type="password"
              autocomplete="new-password"
              placeholder="Initial password"
              aria-label="New user initial password"
              required
              data-testid="new-user-password"
            />
            <label class="flex items-center gap-2 text-sm">
              <input v-model="newUserIsAdmin" type="checkbox" data-testid="new-user-admin" />
              Admin — can manage users
            </label>
            <p v-if="addError" class="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
              {{ addError }}
            </p>
            <Button type="submit" :disabled="adding" data-testid="add-user">Add user</Button>
          </form>
        </section>
      </div>
    </main>
  </div>
</template>
