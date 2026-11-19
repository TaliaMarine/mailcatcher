<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { api } from '../services/api'
import type { ImapAccess } from '../types/api'
import { formatDateLong } from '../utils/format'
import ConfirmDialog from '../components/common/ConfirmDialog.vue'

const access = ref<ImapAccess | null>(null)
const loading = ref(true)
const loadError = ref<string | null>(null)

const username = ref('')
const password = ref('')
const showPassword = ref(false)
const saving = ref(false)
const saveError = ref<string | null>(null)
const saved = ref(false)
const showDelete = ref(false)
const copied = ref<string | null>(null)

const usernameRules = [
  (v: string) => /^[a-z0-9][a-z0-9._@+-]{2,63}$/.test(v) || 'Use 3-64 lowercase letters, digits or . _ @ + -',
]
const passwordRules = [
  (v: string) => v.length >= 12 || 'Use at least 12 characters',
  (v: string) => new TextEncoder().encode(v).length <= 72 || 'Use at most 72 bytes',
]

const valid = computed(() => usernameRules.every((r) => r(username.value) === true) && passwordRules.every((r) => r(password.value) === true))

const settings = computed(() => {
  const a = access.value
  if (!a?.configured) return []
  return [
    { label: 'Server', value: a.host || location.hostname },
    { label: 'Port', value: `${a.tls_port}  (SSL/TLS)` },
    { label: 'Alternative', value: `${a.starttls_port}  (STARTTLS)` },
    { label: 'Username', value: a.username ?? '' },
    { label: 'Password', value: 'the one you set here' },
  ]
})

const smtpSettings = computed(() => {
  const a = access.value
  if (!a || (!a.smtp_port && !a.smtp_tls_port)) return []
  const rows = [{ label: 'Server', value: a.host || location.hostname }]
  if (a.smtp_tls_port) rows.push({ label: 'Port', value: `${a.smtp_tls_port}  (SSL/TLS)` })
  if (a.smtp_port) rows.push({ label: a.smtp_tls_port ? 'Alternative' : 'Port', value: `${a.smtp_port}  (STARTTLS)` })
  rows.push({ label: 'Login', value: 'not needed; any username and password is accepted' })
  return rows
})

async function load() {
  try {
    access.value = await api.getImapAccess()
    username.value = access.value.username ?? (await suggestUsername())
  } catch (e) {
    loadError.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

async function suggestUsername(): Promise<string> {
  try {
    const me = await api.getMe()
    return (me.preferred_username || me.email.split('@')[0] || '').toLowerCase().replace(/[^a-z0-9._@+-]/g, '')
  } catch {
    return ''
  }
}

function generate() {
  const alphabet = 'abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789'
  const bytes = crypto.getRandomValues(new Uint8Array(24))
  password.value = Array.from(bytes, (b) => alphabet[b % alphabet.length]).join('')
  showPassword.value = true
}

async function save() {
  saving.value = true
  saveError.value = null
  saved.value = false
  try {
    access.value = await api.setImapCredentials(username.value, password.value)
    password.value = ''
    showPassword.value = false
    saved.value = true
  } catch (e) {
    saveError.value = (e as Error).message
  } finally {
    saving.value = false
  }
}

async function remove() {
  showDelete.value = false
  await api.deleteImapCredentials()
  password.value = ''
  saved.value = false
  access.value = await api.getImapAccess()
}

async function copy(text: string, key: string) {
  await navigator.clipboard.writeText(text)
  copied.value = key
  setTimeout(() => (copied.value = null), 1500)
}

onMounted(load)
</script>

<template>
  <div class="page">
    <h1 class="text-h5">Mail client access</h1>
    <p class="mc-muted lead">
      Read captured mail in Thunderbird, Apple Mail or any IMAP client. You choose the username and password here;
      they're separate from your single sign-on account.
    </p>

    <section v-if="smtpSettings.length" class="block" aria-labelledby="smtp-h">
      <h2 id="smtp-h" class="text-subtitle-1 font-weight-medium mb-2">Send mail to this server</h2>
      <dl class="mc-inset readout">
        <template v-for="row in smtpSettings" :key="row.label">
          <dt class="mc-muted">{{ row.label }}</dt>
          <dd class="mono">
            {{ row.value }}
            <v-btn
              v-if="row.label !== 'Login'"
              icon
              size="x-small"
              :aria-label="`Copy SMTP ${row.label.toLowerCase()}`"
              @click="copy(row.value.split(' ')[0], 'smtp-' + row.label)"
            >
              <v-icon size="14">{{ copied === 'smtp-' + row.label ? 'mdi-check' : 'mdi-content-copy' }}</v-icon>
            </v-btn>
          </dd>
        </template>
      </dl>
      <p class="text-caption mc-muted mt-2">
        Point an application's SMTP settings here and everything it sends is captured. Nothing is delivered onward.
      </p>
    </section>

    <v-progress-linear v-if="loading" indeterminate />
    <v-alert v-else-if="loadError" type="error" variant="tonal">{{ loadError }}</v-alert>

    <v-alert v-else-if="access && !access.enabled" type="info" variant="tonal">
      IMAP isn't turned on for this server. An administrator can enable it with <span class="mono">IMAP_ENABLED=true</span>.
    </v-alert>

    <template v-else-if="access">
      <v-card class="block">
        <v-card-title class="pt-4">{{ access.configured ? 'Change your login' : 'Create your login' }}</v-card-title>
        <v-card-text>
          <v-form @submit.prevent="valid && save()">
            <v-text-field
              v-model="username"
              label="Username"
              autocomplete="username"
              spellcheck="false"
              :rules="usernameRules"
              hint="Lowercase letters, digits and . _ @ +  -"
              persistent-hint
              class="mb-4"
            />
            <v-text-field
              v-model="password"
              label="New password"
              autocomplete="new-password"
              :type="showPassword ? 'text' : 'password'"
              :append-inner-icon="showPassword ? 'mdi-eye-off-outline' : 'mdi-eye-outline'"
              :rules="passwordRules"
              hint="At least 12 characters. Stored only as a bcrypt hash."
              persistent-hint
              class="mb-2"
              @click:append-inner="showPassword = !showPassword"
            />
            <div class="d-flex align-center ga-3 mt-4">
              <v-btn type="submit" color="primary" variant="flat" :loading="saving" :disabled="!valid">
                {{ access.configured ? 'Save changes' : 'Create login' }}
              </v-btn>
              <v-btn prepend-icon="mdi-dice-multiple-outline" @click="generate">Generate password</v-btn>
              <v-btn
                v-if="password && showPassword"
                prepend-icon="mdi-content-copy"
                @click="copy(password, 'password')"
              >
                {{ copied === 'password' ? 'Copied' : 'Copy' }}
              </v-btn>
            </div>
            <v-alert v-if="saveError" type="error" variant="tonal" class="mt-4" role="alert">{{ saveError }}</v-alert>
            <v-alert v-else-if="saved" type="success" variant="tonal" class="mt-4" role="status">
              Login saved. Use the settings below in your mail client.
            </v-alert>
          </v-form>
        </v-card-text>
      </v-card>

      <section v-if="access.configured" class="block" aria-labelledby="settings-h">
        <h2 id="settings-h" class="text-subtitle-1 font-weight-medium mb-2">Client settings</h2>
        <dl class="mc-inset readout">
          <template v-for="row in settings" :key="row.label">
            <dt class="mc-muted">{{ row.label }}</dt>
            <dd class="mono">
              {{ row.value }}
              <v-btn
                v-if="row.label !== 'Password'"
                icon
                size="x-small"
                :aria-label="`Copy ${row.label.toLowerCase()}`"
                @click="copy(row.value.split(' ')[0], row.label)"
              >
                <v-icon size="14">{{ copied === row.label ? 'mdi-check' : 'mdi-content-copy' }}</v-icon>
              </v-btn>
            </dd>
          </template>
        </dl>
        <p class="text-caption mc-muted mt-2">
          Last changed {{ access.updated_at ? formatDateLong(access.updated_at) : '' }}.
          Folders: INBOX holds every message, and each recipient address is also a folder.
        </p>

        <v-btn color="error" class="mt-4" prepend-icon="mdi-delete-outline" @click="showDelete = true">
          Delete login
        </v-btn>
      </section>

      <section class="block notes">
        <h2 class="text-subtitle-1 font-weight-medium mb-2">Good to know</h2>
        <ul>
          <li>Connections must use TLS. Plain-text logins are refused.</li>
          <li>Everyone who can sign in sees the same captured mail; there are no private inboxes.</li>
          <li>Marking a message deleted and expunging it removes it for everyone.</li>
          <li>Repeated wrong passwords from one address block that address for a while.</li>
        </ul>
      </section>
    </template>

    <ConfirmDialog
      v-model="showDelete"
      title="Delete IMAP login"
      message="Mail clients using this login will stop working. You can create a new one at any time."
      confirm-text="Delete login"
      @confirm="remove"
    />
  </div>
</template>

<style scoped>
.page {
  max-width: 720px;
  padding: 32px 24px 64px;
}
.lead {
  max-width: 62ch;
  margin: 4px 0 24px;
}
.block {
  margin-bottom: 28px;
}
.readout {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 6px 20px;
  padding: 12px 16px;
}
.readout dd {
  margin: 0;
}
.notes ul {
  padding-left: 1.2em;
  max-width: 64ch;
  line-height: 1.6;
}
</style>
