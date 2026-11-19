<script setup lang="ts">
import { useRouter, useRoute } from 'vue-router'
import { useMailboxStore } from '../../stores/mailboxStore'
import { useMessageStore } from '../../stores/messageStore'
import { ref, computed } from 'vue'
import ConfirmDialog from '../common/ConfirmDialog.vue'
import { formatDate } from '../../utils/format'

const router = useRouter()
const route = useRoute()
const mailboxStore = useMailboxStore()
const messageStore = useMessageStore()
const contextMenu = ref(false)
const contextMenuX = ref(0)
const contextMenuY = ref(0)
const contextMailboxId = ref<number | null>(null)
const showDeleteDialog = ref(false)
const showDeleteAllDialog = ref(false)

const totalMessages = computed(() => mailboxStore.mailboxes.reduce((sum, mb) => sum + mb.message_count, 0))
const isAllSelected = computed(() => route.name === 'all-messages')

function openMenu(e: MouseEvent, id: number) {
  e.preventDefault()
  contextMailboxId.value = id
  contextMenuX.value = e.clientX
  contextMenuY.value = e.clientY
  contextMenu.value = true
}

function askDeleteMailbox(id: number) {
  contextMailboxId.value = id
  contextMenu.value = false
  showDeleteDialog.value = true
}

async function doDeleteMailbox() {
  if (contextMailboxId.value !== null) {
    await mailboxStore.deleteMailbox(contextMailboxId.value)
    if (Number(route.params.id) === contextMailboxId.value) router.push('/mailboxes')
  }
  showDeleteDialog.value = false
}

async function doDeleteAll() {
  await mailboxStore.deleteAllMailboxes()
  messageStore.currentMessage = null
  messageStore.messages = []
  showDeleteAllDialog.value = false
  router.push('/mailboxes')
}
</script>

<template>
  <nav class="rail" aria-label="Mailboxes">
    <div class="rail__head">
      <h2 class="text-subtitle-2 mc-muted">Mailboxes</h2>
      <v-btn
        icon
        size="small"
        :disabled="mailboxStore.mailboxes.length === 0"
        aria-label="Delete all mailboxes"
        @click="showDeleteAllDialog = true"
      >
        <v-icon size="18">mdi-delete-sweep-outline</v-icon>
        <v-tooltip activator="parent">Delete all mailboxes</v-tooltip>
      </v-btn>
    </div>

    <div class="rail__list">
      <router-link
        :to="{ name: 'all-messages' }"
        class="mc-row item"
        :class="{ 'mc-row--active': isAllSelected }"
      >
        <span class="item__main">All mailboxes</span>
        <span class="mono item__count">{{ totalMessages }}</span>
      </router-link>

      <template v-if="mailboxStore.loading && mailboxStore.mailboxes.length === 0">
        <v-skeleton-loader v-for="i in 4" :key="i" type="list-item-two-line" />
      </template>

      <p v-else-if="mailboxStore.mailboxes.length === 0" class="empty">
        No mail yet. Point an application at this server's SMTP port and its mail will show up here.
      </p>

      <router-link
        v-for="mb in mailboxStore.mailboxes"
        :key="mb.id"
        :to="{ name: 'mailbox', params: { id: mb.id } }"
        class="mc-row item"
        :class="{ 'mc-row--active': Number(route.params.id) === mb.id }"
        @contextmenu="openMenu($event, mb.id)"
      >
        <span class="item__main">
          <span class="mono item__addr">{{ mb.address }}</span>
          <span class="item__sub mono mc-muted">{{ formatDate(mb.last_message_at) }}</span>
        </span>
        <span class="mono item__count">{{ mb.message_count }}</span>
      </router-link>
    </div>

    <router-link to="/settings/imap" class="rail__foot">
      <v-icon size="18">mdi-key-outline</v-icon>
      <span>Mail client access</span>
    </router-link>
  </nav>

  <v-menu v-model="contextMenu" :target="[contextMenuX, contextMenuY]">
    <v-list density="compact">
      <v-list-item
        prepend-icon="mdi-delete-outline"
        base-color="error"
        title="Delete mailbox"
        @click="askDeleteMailbox(contextMailboxId!)"
      />
    </v-list>
  </v-menu>

  <ConfirmDialog
    v-model="showDeleteDialog"
    title="Delete mailbox"
    message="This deletes the mailbox and every message in it. It can't be undone."
    @confirm="doDeleteMailbox"
  />
  <ConfirmDialog
    v-model="showDeleteAllDialog"
    title="Delete all mailboxes"
    message="This deletes every captured message. It can't be undone."
    confirm-text="Delete everything"
    @confirm="doDeleteAll"
  />
</template>

<style scoped>
.rail {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.rail__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 8px 6px 16px;
}
.rail__head h2 {
  font-size: 0.8125rem;
  font-weight: 600;
  line-height: 1.5;
}
.rail__list {
  flex: 1;
  overflow-y: auto;
  border-top: 1px solid rgb(var(--v-border-color));
}
.item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 16px;
  color: inherit;
  text-decoration: none;
}
.item__main {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-width: 0;
}
.item__addr {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.item__sub {
  font-size: 0.75rem;
}
.item__count {
  min-width: 2ch;
  text-align: right;
}
.empty {
  padding: 16px;
  font-size: 0.8125rem;
  color: rgb(var(--v-theme-on-surface-variant));
  max-width: 34ch;
}
.rail__foot {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  border-top: 1px solid rgb(var(--v-border-color));
  color: rgb(var(--v-theme-on-surface-variant));
  font-size: 0.8125rem;
  text-decoration: none;
}
.rail__foot:hover,
.rail__foot.router-link-active {
  color: rgb(var(--v-theme-primary));
}
</style>
