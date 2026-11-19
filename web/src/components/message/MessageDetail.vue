<script setup lang="ts">
import { ref, computed } from 'vue'
import { useMessageStore } from '../../stores/messageStore'
import { api } from '../../services/api'
import type { MessageRecipient } from '../../types/api'
import { formatDateLong, formatSize } from '../../utils/format'
import ConfirmDialog from '../common/ConfirmDialog.vue'

const messageStore = useMessageStore()
const activeTab = ref('html')
const showDeleteDialog = ref(false)

const msg = computed(() => messageStore.currentMessage)

function recipientsByType(type: string): MessageRecipient[] {
  return msg.value?.recipients.filter((r) => r.recipient_type === type) ?? []
}

function formatRecipient(r: MessageRecipient): string {
  return r.name ? `${r.name} <${r.address}>` : r.address
}

const headers = computed(() => {
  const m = msg.value
  if (!m) return []
  const rows: { label: string; value: string }[] = [
    { label: 'From', value: m.from_name ? `${m.from_name} <${m.from_address}>` : m.from_address },
  ]
  for (const [type, label] of [['to', 'To'], ['cc', 'Cc'], ['bcc', 'Bcc']] as const) {
    const list = recipientsByType(type)
    if (list.length) rows.push({ label, value: list.map(formatRecipient).join(', ') })
  }
  rows.push({ label: 'Received', value: formatDateLong(m.created_at) })
  if (m.message_id) rows.push({ label: 'Message-ID', value: m.message_id })
  rows.push({ label: 'Size', value: formatSize(m.size) })
  return rows
})

async function doDelete() {
  if (msg.value) await messageStore.deleteMessage(msg.value.id)
  showDeleteDialog.value = false
}
</script>

<template>
  <div v-if="messageStore.loadingDetail" class="d-flex justify-center align-center fill">
    <v-progress-circular indeterminate />
  </div>

  <div v-else-if="!msg" class="empty fill">
    <p class="font-weight-medium">Select a message</p>
    <p class="mc-muted text-body-2">Its headers, body and attachments open here.</p>
  </div>

  <article v-else class="detail">
    <header class="detail__head">
      <div class="d-flex align-start ga-2">
        <h1 class="text-h6 flex-grow-1 detail__subject">{{ msg.subject || '(no subject)' }}</h1>
        <v-btn icon size="small" :href="api.getMessageRawUrl(msg.id)" download aria-label="Download as .eml">
          <v-icon>mdi-download-outline</v-icon>
          <v-tooltip activator="parent">Download .eml</v-tooltip>
        </v-btn>
        <v-btn icon size="small" color="error" aria-label="Delete message" @click="showDeleteDialog = true">
          <v-icon>mdi-delete-outline</v-icon>
          <v-tooltip activator="parent">Delete message</v-tooltip>
        </v-btn>
      </div>

      <dl class="mc-inset readout">
        <template v-for="row in headers" :key="row.label">
          <dt class="mc-muted">{{ row.label }}</dt>
          <dd class="mono">{{ row.value }}</dd>
        </template>
      </dl>

      <div v-if="msg.attachments.length > 0" class="d-flex flex-wrap ga-2 mt-3">
        <v-chip
          v-for="att in msg.attachments"
          :key="att.id"
          :href="api.getAttachmentUrl(att.id)"
          variant="outlined"
          prepend-icon="mdi-paperclip"
        >
          {{ att.filename || 'attachment' }}
          <span class="mono mc-muted ml-2">{{ formatSize(att.size) }}</span>
        </v-chip>
      </div>
    </header>

    <v-tabs v-model="activeTab" density="compact" color="primary" class="mc-rule-b">
      <v-tab value="html" class="text-none">HTML</v-tab>
      <v-tab value="text" class="text-none">Plain text</v-tab>
    </v-tabs>

    <div class="detail__body">
      <!-- Served with a sandboxing CSP: no scripts, no forms, links open in a new tab. -->
      <iframe
        v-show="activeTab === 'html'"
        :key="msg.id"
        :src="api.getMessageHtmlUrl(msg.id)"
        title="Message HTML"
        sandbox="allow-popups allow-popups-to-escape-sandbox"
        referrerpolicy="no-referrer"
      />
      <pre v-show="activeTab === 'text'" class="mono plain">{{ msg.body_text || '(this message has no plain text part)' }}</pre>
    </div>

    <ConfirmDialog
      v-model="showDeleteDialog"
      title="Delete message"
      message="This deletes the message and its attachments. It can't be undone."
      @confirm="doDelete"
    />
  </article>
</template>

<style scoped>
.fill {
  height: 100%;
}
.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  text-align: center;
  padding: 32px;
}
.detail {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}
.detail__head {
  padding: 16px 20px 12px;
}
.detail__subject {
  overflow-wrap: anywhere;
}
.readout {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 4px 16px;
  margin: 8px 0 0;
  padding: 10px 14px;
  font-size: 0.8125rem;
}
.readout dt {
  font-size: 0.8125rem;
}
.readout dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.detail__body {
  flex: 1;
  min-height: 0;
  position: relative;
}
.detail__body iframe {
  width: 100%;
  height: 100%;
  border: none;
  background: #fff; /* email HTML assumes a white page */
}
.plain {
  height: 100%;
  margin: 0;
  padding: 16px 20px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
  max-width: 100ch;
}
</style>
