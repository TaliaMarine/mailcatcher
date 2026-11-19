# MailCatcher

An SMTP server that captures outgoing mail from development and test environments, with a web UI and IMAP access. Point an application at it and read what it sent, without anything reaching a real inbox.

## Features

- **SMTP** on port 1025 accepts all mail with no authentication. STARTTLS and implicit TLS (SMTPS) are available when a certificate is configured.
- **Web UI** with a mailbox rail, message list and reader. New mail appears live over WebSocket. HTML mail is rendered in a sandboxed frame.
- **Single sign-on** through any OIDC provider, using Authorization Code + PKCE, so the client needs **no secret**.
- **IMAP over TLS.** Each signed-in user creates their own IMAP username and password in the UI; they are stored in Kubernetes Secrets (password as a bcrypt hash).
- **Search**, attachments, raw `.eml` download, retention cleanup, Prometheus metrics, and a REST API documented at `/docs`.

## Quick start

```bash
docker run -p 1025:1025 -p 1080:1080 -e AUTH_DISABLED=true ghcr.io/taliamarine/mailcatcher
```

Send mail to `localhost:1025` and open http://localhost:1080.

From source (Go 1.27+, Node.js 24+):

```bash
just build-web
just dev          # AUTH_DISABLED=true, SMTP on 8025, HTTP on 8080
```

## Configuration

All configuration is through environment variables.

| Variable | Description | Default |
|----------|-------------|---------|
| `SMTP_PORT` | SMTP listen port | `1025` |
| `SMTP_TLS_PORT` | Implicit-TLS SMTP (SMTPS) port, `0` disables | `0` |
| `HTTP_PORT` | HTTP listen port | `1080` |
| `DATABASE_PATH` | SQLite database file | `mailcatcher.db` |
| `RETENTION_DAYS` | Days to keep messages | `100` |
| `AUTH_DISABLED` | Run without authentication (development only) | `false` |
| `AUTH_TOKEN` | Static bearer token for app-to-app API access | — |
| `BASE_URL` | Public URL, e.g. `https://mail.example.com` (required with OIDC) | — |
| `OIDC_ISSUER` | OIDC issuer URL | — |
| `OIDC_CLIENT_ID` | OIDC client ID | — |
| `OIDC_CLIENT_SECRET` | Only for confidential clients; leave empty for PKCE | — |
| `SESSION_KEY` | 32+ character key signing session cookies | — |
| `SESSION_KEY_SECRET_NAME` | Kubernetes Secret holding the key; created with a random key if missing | — |
| `IMAP_ENABLED` | Serve IMAP | `false` |
| `IMAP_PORT` | IMAP port; clients must STARTTLS before logging in, `0` disables | `1143` |
| `IMAP_TLS_PORT` | Implicit-TLS IMAP (IMAPS) port, `0` disables | `1993` |
| `IMAP_PUBLIC_HOST` / `_PORT` / `_TLS_PORT` | What the UI tells users to connect to | host of `BASE_URL`, `143`, `993` |
| `CREDENTIAL_STORE` | `kubernetes` or `memory`; automatic by environment | — |
| `TLS_CERT_FILE` / `TLS_KEY_FILE` | Certificate for IMAPS, SMTPS and STARTTLS; reloaded when the file changes. Without them a self-signed certificate is generated | — |
| `POD_NAMESPACE` | Namespace for Secrets (read from the service account by default) | — |
| `LOG_LEVEL` / `LOG_FORMAT` | `debug`/`info`/`warn`/`error`, `json`/`plain` | `info`, `json` |

## Authentication

Browser sessions use OIDC Authorization Code with **PKCE (S256)** and a `nonce`. Create a **public** client in your provider (in authentik: client type *Public*) with the redirect URI `<BASE_URL>/auth/callback`; no client secret is involved. Sessions are 24-hour signed cookies. The signing key comes from `SESSION_KEY`, or from a Kubernetes Secret the app creates on first start, so no secret material has to live in Git.

Cross-origin state-changing requests and WebSocket upgrades are rejected, and rendered email is served with a sandboxing Content-Security-Policy.

`AUTH_TOKEN` additionally allows `Authorization: Bearer <token>` on `/api/` for scripts. A token has no user identity, so it cannot manage IMAP logins.

## IMAP

After signing in, open **Mail client access** and choose a username and password. The server stores them as one Kubernetes Secret per login in its own namespace: the username, a bcrypt hash of the password, and the owner's OIDC subject. The plaintext password is never stored. Changing the username replaces the Secret; usernames are unique.

Client settings: the server's `mail.hostname`, port **993** (SSL/TLS) or **143** (STARTTLS), your username and password. Plain-text logins are refused. Ten failed logins from one address block it for 15 minutes.

Folders: `INBOX` holds every message and each recipient address is also a folder. Everyone who can sign in sees the same mail. Flags such as *seen* last for the IMAP session only; marking a message `\Deleted` and expunging it deletes it for everyone. Uploading, copying and creating folders are refused.

## Kubernetes

The chart in [`chart/`](chart) is published to `oci://ghcr.io/taliamarine/charts/mailcatcher` on every release. A minimal install behind a Gateway API gateway:

```yaml
gateway:
  host: mail.example.com
  parentRefs:
    - {name: default-gateway, namespace: envoy-gateway-system, sectionName: https}
oidc:
  issuerUrl: https://authentik.example.com/application/o/mailcatcher/
  clientId: <public client id>
imap:
  enabled: true
mail:
  hostname: imap.example.com        # published by external-dns, used in the certificate
tls:
  certManager:
    enabled: true
    issuerRef: {name: letsencrypt-prod}
```

The chart refuses to render without OIDC unless `authDisabled: true`. See [`chart/values.yaml`](chart/values.yaml) for persistence, SMTPS, service annotations and resources. RBAC is limited to Secrets in the release namespace; give the release its own namespace, since the IMAP login Secrets live beside any others there.

## Development

See [DEVELOPER.md](DEVELOPER.md) and [API.md](API.md).
