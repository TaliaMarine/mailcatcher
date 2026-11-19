<script setup lang="ts">
const model = defineModel<boolean>({ default: false })

defineProps<{
  title: string
  message: string
  confirmText?: string
}>()

const emit = defineEmits<{
  confirm: []
}>()
</script>

<template>
  <v-dialog v-model="model" max-width="440">
    <v-card>
      <v-card-title class="pt-4">{{ title }}</v-card-title>
      <v-card-text>{{ message }}</v-card-text>
      <v-card-actions class="px-4 pb-4">
        <v-spacer />
        <v-btn @click="model = false">Cancel</v-btn>
        <v-btn color="error" variant="flat" @click="emit('confirm')">{{ confirmText || 'Delete' }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
