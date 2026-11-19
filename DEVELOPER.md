# Developer Guide

## Prerequisites

- Go 1.27+
- Node.js 24+
- [just](https://github.com/casey/just) command runner (recommended)
- [golangci-lint](https://golangci-lint.run/) (for linting)

## Getting Started

```bash
# Install frontend dependencies
just install-web

# Start backend (with auth disabled and plain text logs)
just dev

# In another terminal, start frontend dev server
just dev-web
```

The backend runs on `:1080` (SMTP on `:1025`). The frontend dev server runs on `:3000` and proxies `/api` and `/ws` to the backend.

## Justfile Commands

| Command | Description |
|---------|-------------|
| `just dev` | Run backend (set `IMAP_ENABLED=true` for IMAP) with `AUTH_DISABLED=true LOG_FORMAT=plain LOG_LEVEL=debug` |
| `just dev-web` | Run Vite dev server on `:3000` with API proxy to `:1080` |
| `just build` | Build frontend + backend |
| `just build-go` | Build Go binary to `bin/mailcatcher` |
| `just build-web` | Build frontend to `web/dist/` |
| `just install-web` | `npm install` in `web/` |
| `just test` | Run all Go tests |
| `just test-one <name>` | Run a specific Go test |
| `just lint` | Run golangci-lint |
| `just fmt` | Format Go code |
| `just vet` | Run go vet |
| `just check-web` | Type-check frontend (`vue-tsc --noEmit`) |
| `just docker-build` | Build Docker image |
| `just clean` | Remove `bin/` and `web/dist/` |

## Project Structure

```
cmd/mailcatcher/main.go          # Entrypoint — wires all components, graceful shutdown
internal/
  config/config.go               # Environment variable loading with defaults
  database/
    database.go                  # SQLite connection, migrations, repository methods
    models.go                    # Data models (Mailbox, Message, Attachment, etc.)
    migrations/                  # SQL migration files (embedded at build time)
  smtp/server.go                 # SMTP server, MIME parsing, attachment extraction
  api/
    router.go                    # Gin + Huma v2 setup, route registration, static serving
    mailboxes.go                 # Mailbox API handlers
    messages.go                  # Message API handlers (list, detail, raw, delete)
    attachments.go               # Attachment API handlers
    search.go                    # Search handler
    logging.go                   # slog request logging middleware
  websocket/hub.go               # WebSocket hub — connection management, broadcast
  retention/cleanup.go           # Background retention cleanup goroutine
web/
  src/
    services/api.ts              # Typed API client (fetch wrapper)
    stores/                      # Pinia stores (mailboxStore, messageStore)
    composables/useWebSocket.ts  # WebSocket client with auto-reconnect
    components/
      common/ConfirmDialog.vue   # Reusable confirmation dialog
      mailbox/MailboxList.vue    # Sidebar mailbox list
      message/MessageList.vue    # Message list with infinite scroll
      message/MessageDetail.vue  # Message detail (HTML/text tabs, attachments)
    views/
      MailboxView.vue            # Mailbox + message two-panel layout
      SearchView.vue             # Search results view
    plugins/vuetify.ts           # Vuetify theme configuration
    router/index.ts              # Vue Router setup
    types/api.ts                 # TypeScript interfaces matching backend models
```

## Architecture Notes

### Backend

- **Huma v2** handles all `/api/v1/` endpoints and auto-generates the OpenAPI 3.1 spec from Go struct tags. The interactive API browser at `/docs` is provided by Huma.
- **Plain Gin routes** handle everything else: WebSocket (`/ws`), Prometheus metrics (`/metrics`), health checks (`/healthz`, `/readyz`), and static file serving.
- **SQLite** is the sole datastore. Migrations are embedded in the binary via `//go:embed`. The database uses WAL mode with `MaxOpenConns(1)` for safe concurrent reads.
- **SMTP** uses `emersion/go-smtp`. On message receipt, the server parses MIME multipart bodies, extracts attachments, creates mailbox entries for each recipient, and broadcasts a WebSocket event.
- **Retention** runs as a background goroutine that wakes daily and deletes messages older than `RETENTION_DAYS`.

### Frontend

- **Vite** dev server on `:3000` proxies `/api` and `/ws` to the backend on `:1080`, enabling hot-reload development.
- **Pinia** stores manage mailbox and message state. WebSocket events update stores in real-time.
- **Infinite scroll** uses cursor-based pagination — the cursor is the last message ID, requesting the next page with `?cursor=<id>`.
- **HTML rendering** uses a sandboxed iframe (`sandbox="allow-same-origin"`). When no HTML body exists, plain text is rendered in a `<pre>` styled page inside the iframe.

### Static File Serving

In production, the Go binary serves the frontend from `web/dist/`. SPA routing is handled by falling back to `index.html` for any unmatched path. During development, the Vite dev server handles this instead.

## Sending Test Emails

With the backend running:

```python
import smtplib
from email.mime.text import MIMEText

msg = MIMEText('<h1>Hello!</h1>', 'html')
msg['Subject'] = 'Test Email'
msg['From'] = 'alice@example.com'
msg['To'] = 'bob@test.com'

with smtplib.SMTP('localhost', 1025) as smtp:
    smtp.send_message(msg)
```

Or with `swaks`:

```bash
swaks --to bob@test.com --from alice@example.com --server localhost:1025 --body "Hello"
```

## Database

SQLite database is ephemeral by default (`mailcatcher.db` in the working directory). In Docker/Kubernetes, mount a volume to `DATABASE_PATH` if persistence across restarts is needed.

Migrations are in `internal/database/migrations/` and run automatically on startup.

## Docker

Three-stage build:

1. **Node stage** — builds frontend (`web/dist/`)
2. **Go stage** — builds binary, copies in frontend assets
3. **Distroless stage** — final image with binary + static files

```bash
just docker-build
docker run -p 1025:1025 -p 1080:1080 mailcatcher
```

## IMAP and credentials

- `internal/imap` is the IMAP server (go-imap v2). UIDs are message ids, which SQLite never reuses; UIDVALIDITY is stored in the `meta` table so a recreated database invalidates client caches.
- `internal/credentials` stores logins. `Kubernetes` keeps one Secret per login (name derived from the username, so uniqueness is enforced by the API server); `Memory` is used outside a cluster. Tests use the fake clientset.
- Try it locally: `IMAP_ENABLED=true just dev`, create a login at http://localhost:8080/settings/imap, then connect an IMAP client to `localhost:8993` (TLS, self-signed certificate).
