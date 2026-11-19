<script setup lang="ts">
import type { MessageSummary } from '../../types/api'
import { formatDate, formatSize } from '../../utils/format'

defineProps<{
  msg: MessageSummary
  active: boolean
  selectable?: boolean
  selected?: boolean
}>()

defineEmits<{
  open: []
  toggle: []
}>()
</script>

<template>
  <div
    class="mc-row row"
    :class="{ 'mc-row--active': active }"
    role="button"
    tabindex="0"
    @click="$emit('open')"
    @keydown.enter="$emit('open')"
    @keydown.space.prevent="$emit('open')"
  >
    <v-checkbox-btn
      v-if="selectable"
      :model-value="selected"
      density="compact"
      :aria-label="`Select message ${msg.subject || 'with no subject'}`"
      @click.stop
      @update:model-value="$emit('toggle')"
    />
    <div class="row__body">
      <div class="row__top">
        <span class="row__from">{{ msg.from_name || msg.from_address }}</span>
        <span class="mono mc-muted row__date">{{ formatDate(msg.created_at) }}</span>
      </div>
      <div class="row__subject">{{ msg.subject || '(no subject)' }}</div>
      <div class="row__meta mono mc-muted">
        <v-icon v-if="msg.has_attachments" size="14" aria-label="Has attachments">mdi-paperclip</v-icon>
        {{ formatSize(msg.size) }}
      </div>
    </div>
  </div>
</template>

<style scoped>
.row {
  display: flex;
  align-items: flex-start;
  gap: 4px;
  padding: 10px 16px 10px 8px;
}
.row :deep(.v-selection-control) {
  flex: 0 0 auto;
  width: auto;
  min-height: 0;
}
.row__body {
  flex: 1;
  min-width: 0;
  padding-left: 8px;
}
.row__top {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}
.row__from {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.row__date {
  flex-shrink: 0;
  font-size: 0.75rem;
}
.row__subject {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.875rem;
}
.row__meta {
  margin-top: 2px;
  font-size: 0.75rem;
  display: flex;
  align-items: center;
  gap: 4px;
}
</style>
