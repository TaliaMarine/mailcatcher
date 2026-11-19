<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import type { MessageSummary } from '../types/api'
import { api } from '../services/api'
import { useMessageStore } from '../stores/messageStore'
import { plural } from '../utils/format'
import MessageDetail from '../components/message/MessageDetail.vue'
import MessageRow from '../components/message/MessageRow.vue'

const route = useRoute()
const messageStore = useMessageStore()
const results = ref<MessageSummary[]>([])
const loading = ref(false)
const loadingMore = ref(false)
const hasMore = ref(true)
const error = ref<string | null>(null)

async function search(q: string) {
  loading.value = true
  results.value = []
  hasMore.value = true
  error.value = null
  messageStore.currentMessage = null
  try {
    const data = await api.searchMessages(q)
    results.value = data
    hasMore.value = data.length >= 50
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

async function loadMore() {
  const q = route.query.q as string
  if (!q || loadingMore.value || !hasMore.value || results.value.length === 0) return
  loadingMore.value = true
  try {
    const data = await api.searchMessages(q, results.value[results.value.length - 1].id)
    results.value.push(...data)
    hasMore.value = data.length >= 50
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loadingMore.value = false
  }
}

function onScroll(e: Event) {
  const el = e.target as HTMLElement
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 100) loadMore()
}

watch(
  () => route.query.q,
  (q) => {
    if (q) search(q as string)
  },
  { immediate: true },
)
</script>

<template>
  <div class="panes">
    <section class="pane-list mc-rule-r" aria-label="Search results">
      <header class="head mc-rule-b">
        <h1 class="text-subtitle-1 font-weight-medium">
          Results for <span class="mono">"{{ route.query.q }}"</span>
        </h1>
        <p class="text-caption mc-muted">{{ plural(results.length, 'result') }}{{ hasMore && results.length ? ' so far' : '' }}</p>
      </header>

      <div v-if="loading" class="scroll">
        <v-skeleton-loader v-for="i in 8" :key="i" type="list-item-three-line" />
      </div>
      <v-alert v-else-if="error" type="error" variant="tonal" class="ma-4">{{ error }}</v-alert>
      <div v-else-if="results.length === 0" class="pa-6">
        <p class="font-weight-medium">Nothing matches</p>
        <p class="mc-muted text-body-2">Search covers subject, sender and message text. Try a shorter phrase.</p>
      </div>
      <div v-else class="scroll" @scroll="onScroll">
        <MessageRow
          v-for="msg in results"
          :key="msg.id"
          :msg="msg"
          :active="messageStore.currentMessage?.id === msg.id"
          @open="messageStore.fetchMessage(msg.id)"
        />
        <div v-if="loadingMore" class="d-flex justify-center pa-3">
          <v-progress-circular indeterminate size="22" width="2" />
        </div>
      </div>
    </section>

    <div class="pane-detail">
      <MessageDetail />
    </div>
  </div>
</template>

<style scoped>
.panes {
  display: flex;
  height: calc(100vh - 56px);
}
.pane-list {
  display: flex;
  flex-direction: column;
  width: 380px;
  min-width: 300px;
}
.head {
  padding: 12px 16px;
}
.scroll {
  flex: 1;
  overflow-y: auto;
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
</style>
