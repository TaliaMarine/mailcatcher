import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { Mailbox } from '../types/api'
import { api } from '../services/api'

export const useMailboxStore = defineStore('mailbox', () => {
  const mailboxes = ref<Mailbox[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetchMailboxes(sortBy = 'last_message_at', sortOrder = 'desc') {
    loading.value = true
    error.value = null
    try {
      mailboxes.value = await api.listMailboxes(sortBy, sortOrder)
    } catch (e) {
      error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }

  async function deleteMailbox(id: number) {
    await api.deleteMailbox(id)
    mailboxes.value = mailboxes.value.filter((m) => m.id !== id)
  }

  async function deleteAllMailboxes() {
    await api.deleteAllMailboxes()
    mailboxes.value = []
  }

  function handleWsEvent(type: string, data: Record<string, unknown>) {
    if (type === 'message:new' || type === 'message:deleted') {
      // Refresh mailbox list to get updated counts
      fetchMailboxes()
    } else if (type === 'mailbox:deleted') {
      const id = data.mailbox_id
      if (id === '*') {
        mailboxes.value = []
      } else if (typeof id === 'number') {
        mailboxes.value = mailboxes.value.filter((m) => m.id !== id)
      }
    }
  }

  return { mailboxes, loading, error, fetchMailboxes, deleteMailbox, deleteAllMailboxes, handleWsEvent }
})
