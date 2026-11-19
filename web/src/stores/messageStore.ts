import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { MessageSummary, MessageDetail } from '../types/api'
import { api } from '../services/api'

export const useMessageStore = defineStore('message', () => {
  const messages = ref<MessageSummary[]>([])
  const currentMessage = ref<MessageDetail | null>(null)
  const loading = ref(false)
  const loadingMore = ref(false)
  const loadingDetail = ref(false)
  const error = ref<string | null>(null)
  const hasMore = ref(true)
  const currentMailboxId = ref<number | null>(null)
  const selectedIds = ref<Set<number>>(new Set())

  const cursor = computed(() => {
    if (messages.value.length === 0) return 0
    return messages.value[messages.value.length - 1].id
  })

  async function fetchMessages(mailboxId: number | null) {
    currentMailboxId.value = mailboxId
    loading.value = true
    error.value = null
    hasMore.value = true
    messages.value = []
    selectedIds.value.clear()
    try {
      const result = mailboxId === null
        ? await api.listAllMessages()
        : await api.listMessages(mailboxId)
      messages.value = result
      hasMore.value = result.length >= 50
    } catch (e) {
      error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }

  async function loadMore() {
    if (loadingMore.value || !hasMore.value) return
    loadingMore.value = true
    try {
      const result = currentMailboxId.value === null
        ? await api.listAllMessages(cursor.value)
        : await api.listMessages(currentMailboxId.value!, cursor.value)
      messages.value.push(...result)
      hasMore.value = result.length >= 50
    } catch (e) {
      error.value = (e as Error).message
    } finally {
      loadingMore.value = false
    }
  }

  async function fetchMessage(id: number) {
    loadingDetail.value = true
    error.value = null
    try {
      currentMessage.value = await api.getMessage(id)
    } catch (e) {
      error.value = (e as Error).message
    } finally {
      loadingDetail.value = false
    }
  }

  async function deleteMessage(id: number) {
    await api.deleteMessage(id)
    messages.value = messages.value.filter((m) => m.id !== id)
    if (currentMessage.value?.id === id) {
      currentMessage.value = null
    }
    selectedIds.value.delete(id)
  }

  async function deleteSelected() {
    const ids = Array.from(selectedIds.value)
    if (ids.length === 0) return
    await api.deleteMessages(ids)
    messages.value = messages.value.filter((m) => !selectedIds.value.has(m.id))
    if (currentMessage.value && selectedIds.value.has(currentMessage.value.id)) {
      currentMessage.value = null
    }
    selectedIds.value.clear()
  }

  function toggleSelect(id: number) {
    if (selectedIds.value.has(id)) {
      selectedIds.value.delete(id)
    } else {
      selectedIds.value.add(id)
    }
  }

  function selectAll() {
    messages.value.forEach((m) => selectedIds.value.add(m.id))
  }

  function clearSelection() {
    selectedIds.value.clear()
  }

  function handleWsEvent(type: string, data: Record<string, unknown>) {
    if (type === 'message:new') {
      if (currentMailboxId.value === null) {
        // "All Mailboxes" — always refresh
        fetchMessages(null)
      } else {
        const mailboxIds = data.mailbox_ids as number[]
        if (mailboxIds?.includes(currentMailboxId.value)) {
          fetchMessages(currentMailboxId.value)
        }
      }
    } else if (type === 'message:deleted') {
      const deletedIds = data.message_ids as number[]
      if (deletedIds) {
        messages.value = messages.value.filter((m) => !deletedIds.includes(m.id))
        if (currentMessage.value && deletedIds.includes(currentMessage.value.id)) {
          currentMessage.value = null
        }
      }
    }
  }

  return {
    messages, currentMessage, loading, loadingMore, loadingDetail, error, hasMore,
    currentMailboxId, selectedIds,
    fetchMessages, loadMore, fetchMessage,
    deleteMessage, deleteSelected, toggleSelect, selectAll, clearSelection,
    handleWsEvent,
  }
})
