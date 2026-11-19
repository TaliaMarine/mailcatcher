<script setup lang="ts">
import { watch, computed } from 'vue'
import { useRoute } from 'vue-router'
import { useMessageStore } from '../stores/messageStore'
import { useMailboxStore } from '../stores/mailboxStore'
import MessageList from '../components/message/MessageList.vue'
import MessageDetail from '../components/message/MessageDetail.vue'

const route = useRoute()
const messageStore = useMessageStore()
const mailboxStore = useMailboxStore()

const isAllMailboxes = computed(() => route.name === 'all-messages')

const mailboxId = computed<number | null>(() => {
  if (isAllMailboxes.value) return null
  const id = route.params.id
  return id ? Number(id) : null
})

const mailboxLabel = computed(() => {
  if (isAllMailboxes.value) return 'All mailboxes'
  return mailboxStore.mailboxes.find((m) => m.id === mailboxId.value)?.address
})

const hasMailbox = computed(() => isAllMailboxes.value || !!route.params.id)

watch(
  [mailboxId, isAllMailboxes],
  () => {
    if (isAllMailboxes.value) messageStore.fetchMessages(null)
    else if (mailboxId.value !== null) messageStore.fetchMessages(mailboxId.value)
  },
  { immediate: true },
)
</script>

<template>
  <div v-if="!hasMailbox" class="welcome">
    <h1 class="text-h5">Choose a mailbox</h1>
    <p class="mc-muted">
      Mail is grouped by recipient address. Pick one on the left, or open
      <router-link :to="{ name: 'all-messages' }">all mailboxes</router-link> to see everything in arrival order.
    </p>
  </div>

  <div v-else class="panes">
    <div class="pane-list mc-rule-r">
      <MessageList :mailbox-id="mailboxId" :mailbox-label="mailboxLabel" @select="messageStore.fetchMessage" />
    </div>
    <div class="pane-detail">
      <MessageDetail />
    </div>
  </div>
</template>

<style scoped>
.welcome {
  padding: 48px 32px;
  max-width: 56ch;
}
.panes {
  display: flex;
  height: calc(100vh - 56px);
}
.pane-list {
  width: 380px;
  min-width: 300px;
}
.pane-detail {
  flex: 1;
  min-width: 0;
}
@media (max-width: 960px) {
  .pane-list {
    width: 40%;
    min-width: 220px;
  }
}
@media (max-width: 640px) {
  .panes {
    flex-direction: column;
  }
  .pane-list {
    width: 100%;
    height: 45%;
    border-right: none;
    border-bottom: 1px solid rgb(var(--v-border-color));
  }
}
</style>
