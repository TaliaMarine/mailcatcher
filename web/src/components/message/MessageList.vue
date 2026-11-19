<script setup lang="ts">
import { computed } from 'vue'
import { useMessageStore } from '../../stores/messageStore'
import { plural } from '../../utils/format'
import MessageRow from './MessageRow.vue'

defineProps<{
  mailboxId: number | null
  mailboxLabel?: string
}>()

const emit = defineEmits<{
  select: [id: number]
}>()

const messageStore = useMessageStore()

const allSelected = computed(
  () => messageStore.messages.length > 0 && messageStore.messages.every((m) => messageStore.selectedIds.has(m.id)),
)

function toggleSelectAll() {
  if (allSelected.value) messageStore.clearSelection()
  else messageStore.selectAll()
}

function onScroll(e: Event) {
  const el = e.target as HTMLElement
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 100) messageStore.loadMore()
}
</script>

<template>
  <section class="list" aria-label="Messages">
    <header class="list__head mc-rule-b">
      <h1 class="mono list__title">{{ mailboxLabel ?? 'Messages' }}</h1>
      <div class="list__tools">
        <v-checkbox-btn
          :model-value="allSelected"
          :indeterminate="messageStore.selectedIds.size > 0 && !allSelected"
          density="compact"
          aria-label="Select all messages"
          @update:model-value="toggleSelectAll"
        />
        <v-btn
          v-if="messageStore.selectedIds.size > 0"
          size="small"
          color="error"
          prepend-icon="mdi-delete-outline"
          @click="messageStore.deleteSelected()"
        >
          Delete {{ messageStore.selectedIds.size }}
        </v-btn>
        <v-spacer />
        <span class="text-caption mc-muted">{{ plural(messageStore.messages.length, 'message') }}</span>
      </div>
    </header>

    <div v-if="messageStore.loading" class="list__scroll">
      <v-skeleton-loader v-for="i in 8" :key="i" type="list-item-three-line" />
    </div>

    <div v-else-if="messageStore.messages.length === 0" class="list__empty">
      <p class="font-weight-medium">No messages{{ mailboxId !== null ? ' in this mailbox' : '' }}</p>
      <p class="mc-muted text-body-2">New mail appears here the moment it arrives.</p>
    </div>

    <div v-else class="list__scroll" @scroll="onScroll">
      <MessageRow
        v-for="msg in messageStore.messages"
        :key="msg.id"
        :msg="msg"
        selectable
        :selected="messageStore.selectedIds.has(msg.id)"
        :active="messageStore.currentMessage?.id === msg.id"
        @open="emit('select', msg.id)"
        @toggle="messageStore.toggleSelect(msg.id)"
      />
      <div v-if="messageStore.loadingMore" class="d-flex justify-center pa-3">
        <v-progress-circular indeterminate size="22" width="2" />
      </div>
    </div>
  </section>
</template>

<style scoped>
.list {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}
.list__head {
  padding: 10px 16px 4px 16px;
}
.list__title {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.list__tools {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 40px;
  margin-left: -8px;
}
.list__scroll {
  flex: 1;
  overflow-y: auto;
}
.list__empty {
  padding: 32px 24px;
}
</style>
