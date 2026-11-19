<script setup lang="ts">
import { useTheme, useDisplay } from 'vuetify'
import { ref, onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useMailboxStore } from './stores/mailboxStore'
import { useMessageStore } from './stores/messageStore'
import { useWebSocket } from './composables/useWebSocket'
import { api } from './services/api'
import type { UserInfo } from './types/api'
import MailboxList from './components/mailbox/MailboxList.vue'

const theme = useTheme()
const router = useRouter()
const { mdAndUp } = useDisplay()
const mailboxStore = useMailboxStore()
const messageStore = useMessageStore()
const searchQuery = ref('')
const drawer = ref(true)
const user = ref<UserInfo | null>(null)

const isDark = computed(() => theme.global.current.value.dark)

function toggleTheme() {
  const next = isDark.value ? 'light' : 'dark'
  theme.change(next)
  localStorage.setItem('mc-theme', next)
}

function onSearch() {
  const q = searchQuery.value.trim()
  if (q) router.push({ name: 'search', query: { q } })
}

function userInitials(u: UserInfo): string {
  const source = u.name || u.preferred_username || u.email
  return (
    source
      .split(/[\s@._-]+/)
      .filter(Boolean)
      .map((p) => p[0])
      .join('')
      .toUpperCase()
      .slice(0, 2) || '?'
  )
}

async function signOut() {
  await api.signOut()
  location.assign('/')
}

onMounted(async () => {
  try {
    user.value = await api.getMe()
  } catch {
    // Not signed in: the server redirects browsers to the login page.
  }
})

const { connected } = useWebSocket((event) => {
  mailboxStore.handleWsEvent(event.type, event.data as Record<string, unknown>)
  messageStore.handleWsEvent(event.type, event.data as Record<string, unknown>)
})

mailboxStore.fetchMailboxes()
</script>

<template>
  <v-app>
    <v-app-bar flat density="comfortable" class="mc-rule-b" color="surface">
      <v-app-bar-nav-icon v-if="!mdAndUp" aria-label="Toggle mailbox list" @click="drawer = !drawer" />

      <router-link to="/mailboxes" class="brand" aria-label="MailCatcher home">
        <img src="/favicon.svg" alt="" width="24" height="24" />
        <span>MailCatcher</span>
      </router-link>

      <v-text-field
        v-model="searchQuery"
        prepend-inner-icon="mdi-magnify"
        placeholder="Search subject, sender or body"
        aria-label="Search messages"
        density="compact"
        hide-details
        single-line
        clearable
        class="search mx-4"
        @keyup.enter="onSearch"
      />

      <v-spacer />

      <v-tooltip :text="connected ? 'Live: new mail appears as it arrives' : 'Disconnected: reconnecting'">
        <template #activator="{ props }">
          <span v-bind="props" class="live" :class="{ 'live--on': connected }" role="status">
            <span class="live__dot" />
            <span class="d-none d-sm-inline">{{ connected ? 'Live' : 'Offline' }}</span>
          </span>
        </template>
      </v-tooltip>

      <v-btn icon :aria-label="isDark ? 'Switch to light theme' : 'Switch to dark theme'" @click="toggleTheme">
        <v-icon>{{ isDark ? 'mdi-weather-sunny' : 'mdi-weather-night' }}</v-icon>
      </v-btn>

      <v-menu v-if="user" location="bottom end">
        <template #activator="{ props }">
          <v-btn icon v-bind="props" aria-label="Account menu">
            <v-avatar size="30" color="surface-variant" class="avatar">
              <span class="text-caption font-weight-medium">{{ userInitials(user) }}</span>
            </v-avatar>
          </v-btn>
        </template>
        <v-card min-width="280">
          <div class="pa-4">
            <div class="font-weight-medium">{{ user.name || user.preferred_username }}</div>
            <div class="mono mc-muted">{{ user.email }}</div>
          </div>
          <v-divider />
          <v-list density="compact" nav>
            <v-list-item to="/settings/imap" prepend-icon="mdi-key-outline" title="Mail client access" />
            <v-list-item v-if="user.can_sign_out" prepend-icon="mdi-logout" title="Sign out" @click="signOut" />
          </v-list>
        </v-card>
      </v-menu>
    </v-app-bar>

    <v-navigation-drawer
      v-model="drawer"
      :permanent="mdAndUp"
      :temporary="!mdAndUp"
      width="288"
      class="mc-rule-r"
      color="surface"
    >
      <MailboxList />
    </v-navigation-drawer>

    <v-main>
      <router-view />
    </v-main>
  </v-app>
</template>

<style scoped>
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-left: 12px;
  color: rgb(var(--v-theme-on-surface));
  text-decoration: none;
  font-weight: 600;
  letter-spacing: -0.01em;
}
.search {
  max-width: 440px;
  min-width: 120px;
}
.live {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-right: 8px;
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-on-surface-variant));
}
.live__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: rgb(var(--v-theme-warning));
}
.live--on .live__dot {
  background: rgb(var(--v-theme-success));
}
.avatar {
  border: 1px solid rgb(var(--v-border-color));
}
</style>
