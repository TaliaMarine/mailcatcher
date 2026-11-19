# Default recipe
default:
    @just --list

# Run backend in development mode
dev:
    SMTP_PORT=8025 HTTP_PORT=8080 AUTH_DISABLED=true LOG_FORMAT=plain LOG_LEVEL=debug go run ./cmd/mailcatcher

# Run backend in development mode with token auth enabled
dev-token:
    SMTP_PORT=8025 HTTP_PORT=8080 AUTH_DISABLED=false AUTH_TOKEN=dev-secret-token LOG_FORMAT=plain LOG_LEVEL=debug go run ./cmd/mailcatcher

# Run frontend dev server (with proxy to backend)
dev-web:
    cd web && npm run dev

# Build everything
build target:
    just build-{{target}}

# Build Go binary
build-go:
    go build -o bin/mailcatcher ./cmd/mailcatcher

# Build frontend
build-web:
    cd web && npm i && npm run build

build-frontend: build-web

build-docker:
    docker build -t mailcatcher .

# Install frontend dependencies
install-web:
    cd web && npm install

# Run all Go tests
test:
    go test ./...

# Run a specific test
test-one TEST:
    go test -run {{TEST}} ./...

# Run Go linter
lint:
    golangci-lint run ./...

# Format Go code
fmt:
    gofmt -w .

# Run go vet
vet:
    go vet ./...

# Type-check frontend
check-web:
    cd web && npx vue-tsc --noEmit

# Build Docker image
docker-build:
    docker build -t mailcatcher .

# Clean build artifacts
clean:
    rm -rf bin/ web/dist/


upgrade-all: upgrade-backend upgrade-frontend

upgrade-backend:
    go get -u ./... && go mod tidy

upgrade-frontend:
    #!/bin/bash
    cd web
    echo "Checking for outdated packages..."
    npx npm-check-updates
    echo "Upgrading all dependencies to latest versions (typescript stays on ~6, see web/.ncurc.json)..."
    npx npm-check-updates -u
    echo "Installing updated packages..."
    npm install
    echo "Frontend dependencies successfully upgraded!"

upgrade target:
    just upgrade-{{target}}

# Check the Helm chart renders in its main configurations
chart-check:
    helm lint chart --set authDisabled=true --set gateway.enabled=false
    helm template mc chart --set authDisabled=true --set gateway.enabled=false >/dev/null

# Check every GitHub Action is pinned to a commit SHA (pin with: pinact run)
pinact:
    pinact run --check .github/workflows/*.yml
