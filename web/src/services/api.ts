import type { Mailbox, MessageSummary, MessageDetail, AttachmentMeta, UserInfo, ImapAccess } from '../types/api'

const BASE = '/api/v1'

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init)
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.detail || body.error || `Request failed: ${res.status}`)
  }
  if (res.status === 204) return undefined as T
  return res.json()
}

export const api = {
  // Mailboxes
  listMailboxes(sortBy = 'last_message_at', sortOrder = 'desc'): Promise<Mailbox[]> {
    return request(`${BASE}/mailboxes?sort_by=${sortBy}&sort_order=${sortOrder}`)
  },

  getMailbox(id: number): Promise<Mailbox> {
    return request(`${BASE}/mailboxes/${id}`)
  },

  deleteMailbox(id: number): Promise<void> {
    return request(`${BASE}/mailboxes/${id}`, { method: 'DELETE' })
  },

  deleteAllMailboxes(): Promise<void> {
    return request(`${BASE}/mailboxes`, { method: 'DELETE' })
  },

  // Messages
  listAllMessages(cursor = 0, limit = 50): Promise<MessageSummary[]> {
    const params = new URLSearchParams({ limit: String(limit) })
    if (cursor > 0) params.set('cursor', String(cursor))
    return request(`${BASE}/messages?${params}`)
  },

  listMessages(mailboxId: number, cursor = 0, limit = 50): Promise<MessageSummary[]> {
    const params = new URLSearchParams({ limit: String(limit) })
    if (cursor > 0) params.set('cursor', String(cursor))
    return request(`${BASE}/mailboxes/${mailboxId}/messages?${params}`)
  },

  getMessage(id: number): Promise<MessageDetail> {
    return request(`${BASE}/messages/${id}`)
  },

  deleteMessage(id: number): Promise<void> {
    return request(`${BASE}/messages/${id}`, { method: 'DELETE' })
  },

  deleteMessages(ids: number[]): Promise<void> {
    return request(`${BASE}/messages`, {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ids }),
    })
  },

  // Attachments
  listAttachments(messageId: number): Promise<AttachmentMeta[]> {
    return request(`${BASE}/messages/${messageId}/attachments`)
  },

  getAttachmentUrl(id: number): string {
    return `${BASE}/attachments/${id}`
  },

  getMessageRawUrl(id: number): string {
    return `${BASE}/messages/${id}/raw`
  },

  // User
  getMe(): Promise<UserInfo> {
    return request(`${BASE}/me`)
  },

  getMessageHtmlUrl(id: number): string {
    return `${BASE}/messages/${id}/html`
  },

  // IMAP access
  getImapAccess(): Promise<ImapAccess> {
    return request(`${BASE}/imap/credentials`)
  },

  setImapCredentials(username: string, password: string): Promise<ImapAccess> {
    return request(`${BASE}/imap/credentials`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    })
  },

  deleteImapCredentials(): Promise<void> {
    return request(`${BASE}/imap/credentials`, { method: 'DELETE' })
  },

  async signOut(): Promise<void> {
    await fetch('/auth/logout', { method: 'POST' })
  },

  // Search
  searchMessages(q: string, cursor = 0, limit = 50): Promise<MessageSummary[]> {
    const params = new URLSearchParams({ q, limit: String(limit) })
    if (cursor > 0) params.set('cursor', String(cursor))
    return request(`${BASE}/search?${params}`)
  },
}
