package websocket

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait = 5 * time.Second
	readLimit = 512
)

type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type Hub struct {
	mu          sync.Mutex // guards clients, subscribers and serializes socket writes
	clients     map[*websocket.Conn]struct{}
	subscribers map[chan Event]struct{}
	upgrader    websocket.Upgrader
	logger      *slog.Logger
}

// NewHub creates a hub. checkOrigin decides which browser origins may open a
// socket; nil accepts only same-origin requests (gorilla's default).
func NewHub(logger *slog.Logger, checkOrigin func(*http.Request) bool) *Hub {
	return &Hub{
		clients:     make(map[*websocket.Conn]struct{}),
		subscribers: make(map[chan Event]struct{}),
		upgrader:    websocket.Upgrader{CheckOrigin: checkOrigin},
		logger:      logger,
	}
}

// SetOriginCheck replaces the origin check used for new connections.
func (h *Hub) SetOriginCheck(f func(*http.Request) bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.upgrader.CheckOrigin = f
}

// Subscribe returns a channel of every broadcast event, and a function that
// ends the subscription. Events are dropped for a subscriber that falls behind.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 16)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subscribers, ch)
		h.mu.Unlock()
	}
}

func (h *Hub) HandleConnect(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	upgrader := h.upgrader
	h.mu.Unlock()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("websocket upgrade failed", "error", err)
		return
	}

	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()

	h.logger.Debug("websocket client connected", "remote", conn.RemoteAddr())

	conn.SetReadLimit(readLimit)

	// Read loop — just drain messages and detect disconnect
	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.clients, conn)
			h.mu.Unlock()
			_ = conn.Close()
			h.logger.Debug("websocket client disconnected", "remote", conn.RemoteAddr())
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
}

func (h *Hub) Broadcast(event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		h.logger.Error("failed to marshal websocket event", "error", err)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for ch := range h.subscribers {
		select {
		case ch <- event:
		default:
		}
	}

	for conn := range h.clients {
		_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			h.logger.Debug("failed to write to websocket client, dropping it", "error", err)
			delete(h.clients, conn)
			_ = conn.Close()
		}
	}
}

func (h *Hub) ClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}
