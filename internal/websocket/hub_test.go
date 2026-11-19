package websocket

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ws "github.com/gorilla/websocket"
)

func testHub(t *testing.T) *Hub {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewHub(logger, nil)
}

func connectClient(t *testing.T, hub *Hub) (*ws.Conn, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleConnect(w, r)
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := ws.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, server
}

func TestNewHub(t *testing.T) {
	hub := testHub(t)
	if hub == nil {
		t.Fatal("expected non-nil hub")
	}
	if hub.ClientCount() != 0 {
		t.Errorf("ClientCount = %d, want 0", hub.ClientCount())
	}
}

func TestHub_Connect(t *testing.T) {
	hub := testHub(t)
	connectClient(t, hub)

	// Give a moment for the goroutine to register
	time.Sleep(50 * time.Millisecond)

	if hub.ClientCount() != 1 {
		t.Errorf("ClientCount = %d, want 1", hub.ClientCount())
	}
}

func TestHub_MultipleClients(t *testing.T) {
	hub := testHub(t)
	connectClient(t, hub)
	connectClient(t, hub)
	connectClient(t, hub)

	time.Sleep(50 * time.Millisecond)

	if hub.ClientCount() != 3 {
		t.Errorf("ClientCount = %d, want 3", hub.ClientCount())
	}
}

func TestHub_Disconnect(t *testing.T) {
	hub := testHub(t)
	conn, _ := connectClient(t, hub)

	time.Sleep(50 * time.Millisecond)
	if hub.ClientCount() != 1 {
		t.Fatalf("ClientCount = %d, want 1 before close", hub.ClientCount())
	}

	_ = conn.Close()
	time.Sleep(100 * time.Millisecond)

	if hub.ClientCount() != 0 {
		t.Errorf("ClientCount = %d, want 0 after close", hub.ClientCount())
	}
}

func TestHub_Broadcast(t *testing.T) {
	hub := testHub(t)
	conn, _ := connectClient(t, hub)

	time.Sleep(50 * time.Millisecond)

	hub.Broadcast(Event{
		Type: "message:new",
		Data: map[string]any{"message_id": float64(42)},
	})

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, message, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var event Event
	if err := json.Unmarshal(message, &event); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if event.Type != "message:new" {
		t.Errorf("Type = %q, want %q", event.Type, "message:new")
	}
}

func TestHub_BroadcastToMultipleClients(t *testing.T) {
	hub := testHub(t)
	conn1, _ := connectClient(t, hub)
	conn2, _ := connectClient(t, hub)

	time.Sleep(50 * time.Millisecond)

	hub.Broadcast(Event{
		Type: "message:deleted",
		Data: map[string]any{"id": float64(1)},
	})

	for i, conn := range []*ws.Conn{conn1, conn2} {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("client %d ReadMessage: %v", i, err)
		}
		var event Event
		if err := json.Unmarshal(msg, &event); err != nil {
			t.Fatalf("client %d Unmarshal: %v", i, err)
		}
		if event.Type != "message:deleted" {
			t.Errorf("client %d Type = %q, want %q", i, event.Type, "message:deleted")
		}
	}
}

func TestHub_BroadcastNoClients(t *testing.T) {
	hub := testHub(t)
	// Should not panic
	hub.Broadcast(Event{Type: "test", Data: nil})
}
