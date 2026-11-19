# MailCatcher Next API

Base URL: `http://localhost:1080`

## Authentication

### Browser (OIDC)

Interactive users authenticate via OIDC. Unauthenticated browser requests are redirected to `/auth/login`. Sessions are stored in an `mc_session` cookie (24h TTL).

### App-to-App (Bearer Token)

External applications authenticate using a static Bearer token set via the `AUTH_TOKEN` environment variable. Include it in the `Authorization` header:

```
Authorization: Bearer <AUTH_TOKEN>
```

Token authentication is accepted on all `/api/v1/` endpoints. Returns `401 Unauthorized` if the token is missing or invalid.

### Public Endpoints (No Auth Required)

| Endpoint | Description |
|---|---|
| `GET /healthz` | Health check |
| `GET /readyz` | Readiness probe (DB ping) |
| `GET /metrics` | Prometheus metrics |
| `GET /docs` | Interactive API documentation (Huma) |
| `GET /openapi.json` | OpenAPI 3.1 specification |
| `GET /auth/login` | OIDC login redirect |
| `GET /auth/callback` | OIDC callback |
| `POST /auth/logout` | End the local session |

---

## Endpoints

### External Messages (App-to-App)

#### `GET /api/v1/message`

Returns all captured messages in a flat format designed for app-to-app integration.

**Response** `200 OK`

```json
[
  {
    "id": 1,
    "sender": "<sender@example.com> size=554",
    "recipients": ["<recipient@example.com>"],
    "subject": "Test email",
    "size": "554",
    "created_at": "2026-04-09T05:53:00+00:00"
  },
  {
    "id": 2,
    "sender": "<sender2@example.com> size=562",
    "recipients": ["<recipient2@example.com>"],
    "subject": "Test email 2",
    "size": "562",
    "created_at": "2026-04-09T05:53:38+00:00"
  }
]
```

**Example:**

```bash
curl -H "Authorization: Bearer $AUTH_TOKEN" http://localhost:1080/api/v1/message
```

---

### Mailboxes

#### `GET /api/v1/mailboxes`

List all mailboxes with message counts.

| Query Param | Type | Default | Description |
|---|---|---|---|
| `sort_by` | string | `last_message_at` | Sort field: `address`, `name`, `message_count`, `last_message_at` |
| `sort_order` | string | `desc` | Sort direction: `asc`, `desc` |

#### `GET /api/v1/mailboxes/{id}`

Get a single mailbox with stats.

#### `DELETE /api/v1/mailboxes/{id}`

Delete a mailbox and its exclusive messages.

#### `DELETE /api/v1/mailboxes`

Delete all mailboxes and messages.

---

### Messages

#### `GET /api/v1/messages`

List all messages across all mailboxes (paginated).

| Query Param | Type | Default | Description |
|---|---|---|---|
| `cursor` | int | `0` | Cursor for pagination (message ID) |
| `limit` | int | `50` | Number of messages (1-100) |

#### `GET /api/v1/mailboxes/{id}/messages`

List messages in a specific mailbox (paginated). Same query params as above.

#### `GET /api/v1/messages/{id}`

Get full message detail including body, attachments, and recipients.

#### `GET /api/v1/messages/{id}/raw`

Download the raw `.eml` file.

#### `DELETE /api/v1/messages/{id}`

Delete a single message.

#### `DELETE /api/v1/messages`

Bulk delete messages.

**Request Body:**

```json
{
  "ids": [1, 2, 3]
}
```

---

### Attachments

#### `GET /api/v1/messages/{id}/attachments`

List attachments for a message.

#### `GET /api/v1/attachments/{id}`

Download an attachment.

---

### Search

#### `GET /api/v1/search`

Search messages by subject, sender, or body text.

| Query Param | Type | Required | Description |
|---|---|---|---|
| `q` | string | yes | Search query |
| `cursor` | int | no | Cursor for pagination |
| `limit` | int | no | Number of results (1-100, default 50) |

---

### User

#### `GET /api/v1/me`

Get current authenticated user info (OIDC sessions only).

### IMAP access (OIDC sessions only)

`GET /api/v1/imap/credentials` returns `{enabled, configured, username, host, tls_port, starttls_port}`.
`PUT /api/v1/imap/credentials` with `{"username", "password"}` creates or replaces the caller's login (422 on invalid input, 409 if the username is taken).
`DELETE /api/v1/imap/credentials` removes it. Bearer-token clients receive 403.

### Rendered HTML

`GET /api/v1/messages/{id}/html` returns the HTML body with a sandboxing Content-Security-Policy, for embedding in an iframe.

---

### WebSocket

#### `GET /ws`

WebSocket connection for real-time events. Requires OIDC session.

**Events:**

| Event | Data | Description |
|---|---|---|
| `message:new` | `{message, mailbox_ids}` | New email captured |
| `message:deleted` | `{message_ids, mailbox_ids}` | Messages deleted |
| `mailbox:deleted` | `{mailbox_id}` | Mailbox deleted |

---

## Configuration

| Environment Variable | Default | Description |
|---|---|---|
| `HTTP_PORT` | `1080` | HTTP server port |
| `SMTP_PORT` | `1025` | SMTP server port |
| `DATABASE_PATH` | `mailcatcher.db` | SQLite database path |
| `RETENTION_DAYS` | `100` | Days to retain messages |
| `AUTH_DISABLED` | `false` | Disable all authentication |
| `AUTH_TOKEN` | _(empty)_ | Bearer token for app-to-app API access |
| `OIDC_ISSUER` | _(empty)_ | OIDC provider issuer URL |
| `OIDC_CLIENT_ID` | _(empty)_ | OIDC client ID |
| `OIDC_CLIENT_SECRET` | _(empty)_ | Only for confidential clients; PKCE is always used |
| `BASE_URL` | _(empty)_ | Externally-reachable URL for OIDC redirects |
| `LOG_LEVEL` | `info` | Log level: debug, info, warn, error |
| `LOG_FORMAT` | `json` | Log format: json, plain |

## OpenAPI Spec

The full OpenAPI 3.1 specification is available at `/openapi.json` and interactive docs at `/docs` -- both accessible without authentication.
