import { ref, onUnmounted } from 'vue'
import type { WsEvent } from '../types/api'

type EventHandler = (event: WsEvent) => void

const handlers = new Set<EventHandler>()
let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
const connected = ref(false)

function getWsUrl(): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${location.host}/ws`
}

function connect() {
  if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
    return
  }

  ws = new WebSocket(getWsUrl())

  ws.onopen = () => {
    connected.value = true
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
  }

  ws.onmessage = (e) => {
    try {
      const event: WsEvent = JSON.parse(e.data)
      handlers.forEach((h) => h(event))
    } catch {
      // ignore malformed messages
    }
  }

  ws.onclose = () => {
    connected.value = false
    ws = null
    scheduleReconnect()
  }

  ws.onerror = () => {
    ws?.close()
  }
}

function scheduleReconnect() {
  if (!reconnectTimer) {
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null
      connect()
    }, 2000)
  }
}

// Auto-connect on first import
connect()

export function useWebSocket(handler: EventHandler) {
  handlers.add(handler)

  onUnmounted(() => {
    handlers.delete(handler)
  })

  return { connected }
}
