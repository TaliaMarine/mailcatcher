# syntax=docker/dockerfile:1

# ---- Stage 1: build the frontend (runs on the build host's architecture) ----
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /web

# Lock files first so the install layer is cached until dependencies change.
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund

COPY web/ .
RUN npm run build

# ---- Stage 2: build the Go binary, cross-compiled for the target platform ----
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS backend
WORKDIR /app
ENV GOFLAGS=-mod=readonly

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download && go mod verify

COPY cmd/ cmd/
COPY internal/ internal/

ARG TARGETOS TARGETARCH
# Pure Go (modernc sqlite), so the binary runs on the distroless static image.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /mailcatcher ./cmd/mailcatcher

# ---- Stage 3: minimal runtime ----
# No shell or package manager. The nonroot tag runs as uid/gid 65532.
FROM gcr.io/distroless/static:nonroot

LABEL org.opencontainers.image.source="https://github.com/TaliaMarine/mailcatcher" \
      org.opencontainers.image.description="SMTP capture server with web UI, OIDC sign-in and IMAP access"

COPY --from=backend /mailcatcher /mailcatcher
# The binary serves the frontend from disk (web/dist relative to /), not via go:embed.
COPY --from=frontend /web/dist /web/dist

ENV DATABASE_PATH=/tmp/mailcatcher.db \
    SMTP_PORT=1025 \
    HTTP_PORT=1080

EXPOSE 1025 1080 1143 1993
WORKDIR /
USER 65532:65532
ENTRYPOINT ["/mailcatcher"]
