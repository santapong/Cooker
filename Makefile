.PHONY: all build build-cli clean dev test lint uat-up uat-up-socketproxy uat-up-with-keycloak uat-down uat-logs uat-shell uat-reset test-e2e release release-snapshot

# Variables
BINARY_NAME=cooker
CLI_BINARY_NAME=cookerctl
BACKEND_DIR=backend
FRONTEND_DIR=frontend
DEPLOY_DIR=deploy

# Build metadata injected via -ldflags so `cooker --version` reports
# the real commit and date even for local builds.  GoReleaser overrides
# these same vars during an official release (VERSION is set from the
# git tag; COMMIT and BUILD_DATE come from GoReleaser's template vars).
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.1.0-dev")
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

GO_LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(BUILD_DATE)

all: build

# --- Backend ---
build-backend:
	cd $(BACKEND_DIR) && CGO_ENABLED=0 GOOS=linux go build \
		-ldflags "$(GO_LDFLAGS)" \
		-o ../bin/$(BINARY_NAME) ./cmd/cooker/

# Builds the cookerctl CLI — a second binary in the same Go module that
# talks to a Cooker server over the REST API with a ck_ token. Mirrors
# build-backend; clientVersion is injected so `cookerctl version` reports
# the real release. CGO disabled + static for the same portability the
# server binary gets.
build-cli:
	cd $(BACKEND_DIR) && CGO_ENABLED=0 go build \
		-ldflags "-s -w -X main.clientVersion=$(VERSION)" \
		-o ../bin/$(CLI_BINARY_NAME) ./cmd/cookerctl/

test-backend:
	cd $(BACKEND_DIR) && go test ./... -v -race

lint-backend:
	cd $(BACKEND_DIR) && golangci-lint run ./...

# --- Frontend ---
install-frontend:
	cd $(FRONTEND_DIR) && npm ci

build-frontend:
	cd $(FRONTEND_DIR) && npm run build

test-frontend:
	cd $(FRONTEND_DIR) && npm test

lint-frontend:
	cd $(FRONTEND_DIR) && npm run lint

# --- Combined ---
build: build-backend build-frontend

test: test-backend test-frontend

lint: lint-backend lint-frontend

dev:
	docker compose up --build

# --- Release (GoReleaser) ---
#
# release-snapshot: build + archive locally without pushing anything.
#   Produces dist/ with binaries, tarballs, and checksums.txt.
#   Does NOT require a git tag or GITHUB_TOKEN.
#   Useful for verifying the goreleaser config before tagging.
#
# release: publish a real release. Requires:
#   - The HEAD to be tagged with a semver tag (e.g. v0.1.0).
#   - GITHUB_TOKEN exported in the environment.
#   - docker login ghcr.io already performed (or run via the GH Actions
#     workflow which handles login automatically).
#   - cosign on PATH (installed by the release workflow via
#     sigstore/cosign-installer).
#
release-snapshot:
	goreleaser release --snapshot --clean

release:
	goreleaser release --clean

# --- Docker ---
docker-build:
	docker build -t cooker:latest -f $(DEPLOY_DIR)/docker/Dockerfile .

docker-push: docker-build
	docker push cooker:latest

# --- Database ---
migrate-up:
	cd $(BACKEND_DIR) && go run ./cmd/cooker/ migrate up

migrate-down:
	cd $(BACKEND_DIR) && go run ./cmd/cooker/ migrate down

# --- OpenAPI / Swagger ---
# `make swagger` regenerates docs/api/swagger.{json,yaml,go} from
# the swag annotations on cmd/cooker/main.go and the handlers.
# Requires `swag` on PATH (go install github.com/swaggo/swag/cmd/swag@latest).
swagger:
	cd $(BACKEND_DIR) && swag init -d ./cmd/cooker,./internal/handler -g main.go -o docs/api --parseInternal --parseDependency

# --- OCI distribution-spec conformance ---
# Boots a local registry:2, pushes via Cooker's pusher, then runs the
# upstream conformance binary against that registry. Mirrors the CI
# workflow at .github/workflows/oci-conformance.yml. The upstream
# binary is a Ginkgo test suite (no main) — built with `go test -c`.
oci-conformance:
	docker rm -f cooker-conformance-registry 2>/dev/null || true
	docker run -d --rm --name cooker-conformance-registry -p 5000:5000 registry:2 \
		>/dev/null
	@echo "Waiting for registry..."
	@for i in 1 2 3 4 5 6 7 8 9 10; do \
		curl -fsS http://localhost:5000/v2/ >/dev/null 2>&1 && break; \
		sleep 1; \
	done
	cd $(BACKEND_DIR) && \
		OCI_NAMESPACE=cooker-conformance/test \
		COOKER_OCI_REGISTRY=localhost:5000 \
		go test -tags oci_conformance -v \
			-run 'TestPushConformance|TestManifestSpecConformance' \
			./internal/pusher/...
	@echo "Push + image-spec validation OK; building upstream conformance binary..."
	@tmp=$$(mktemp -d); \
	 git clone --depth 1 https://github.com/opencontainers/distribution-spec "$$tmp/dist-spec" >/dev/null 2>&1 && \
	 cd "$$tmp/dist-spec/conformance" && go test -c -o /tmp/cooker-conformance.test
	@OCI_ROOT_URL=http://localhost:5000 \
	 OCI_NAMESPACE=cooker-conformance/test \
	 OCI_USERNAME="" OCI_PASSWORD="" \
	 OCI_TEST_PULL=1 OCI_TEST_CONTENT_DISCOVERY=1 \
	 /tmp/cooker-conformance.test || (docker rm -f cooker-conformance-registry; exit 1)
	docker rm -f cooker-conformance-registry

# --- Helm ---
helm-install:
	helm install cooker $(DEPLOY_DIR)/helm/cooker/

helm-upgrade:
	helm upgrade cooker $(DEPLOY_DIR)/helm/cooker/

helm-uninstall:
	helm uninstall cooker

# Mirror the CI helm job's kubeconform gates locally: render the chart
# (default values) and validate it on stdin, then validate the raw
# deploy/kubernetes/ parity manifests. CRD instances (e.g. the
# monitoring.coreos.com ServiceMonitor/PrometheusRule that M0-T1 adds)
# resolve against the Datree CRDs-catalog; -ignore-missing-schemas is the
# fallback so an uncatalogued CRD is skipped, not failed. Requires `helm`
# and `kubeconform` on PATH.
KUBECONFORM_SCHEMAS=-schema-location default \
	-schema-location 'https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json'
helm-validate:
	helm template cooker $(DEPLOY_DIR)/helm/cooker | \
		kubeconform -strict -summary -ignore-missing-schemas $(KUBECONFORM_SCHEMAS) -
	kubeconform -strict -summary -ignore-missing-schemas $(KUBECONFORM_SCHEMAS) $(DEPLOY_DIR)/kubernetes/

# --- UAT (self-contained testers' stack) ---
# See docs/UAT.md for the full runbook. Brings up cooker + postgres
# + a local CNCF Distribution registry + a single-node k3s cluster.
# Teardown removes all volumes so state never survives across runs.
UAT_COMPOSE=docker compose -f docker-compose.uat.yml --env-file .env.uat

# Resolve the host's docker group GID so the non-root container
# user can access the bind-mounted /var/run/docker.sock. Tries
# `getent group docker` first (Linux), then the socket's GID via
# stat (works on macOS Docker Desktop's gRPC FUSE mount), then
# falls back to 999 (Debian/Ubuntu default).
DOCKER_GID := $(shell getent group docker 2>/dev/null | cut -d: -f3 \
                  || stat -c '%g' /var/run/docker.sock 2>/dev/null \
                  || echo 999)

uat-up:
	@if [ ! -f .env.uat ]; then \
	  cp .env.uat.example .env.uat; \
	  echo "COOKER_SECRET_KEY=$$(head -c 32 /dev/urandom | base64)" >> .env.uat; \
	  echo "DOCKER_GID=$(DOCKER_GID)" >> .env.uat; \
	  echo "Created .env.uat from .env.uat.example (DOCKER_GID=$(DOCKER_GID))."; \
	  echo "Edit .env.uat to enable OIDC."; \
	fi
	$(UAT_COMPOSE) up -d --build
	@echo
	@echo "Cooker UAT ready at http://localhost:8080"
	@echo "  logs:  make uat-logs"
	@echo "  shell: make uat-shell"
	@echo "  down:  make uat-down"

# Variant of uat-up that routes the cooker container through
# tecnativa/docker-socket-proxy instead of bind-mounting the host
# docker.sock. See docker-compose.uat.socketproxy.yml for the
# trade-offs.
uat-up-socketproxy:
	@if [ ! -f .env.uat ]; then \
	  cp .env.uat.example .env.uat; \
	  echo "COOKER_SECRET_KEY=$$(head -c 32 /dev/urandom | base64)" >> .env.uat; \
	  echo "Created .env.uat from .env.uat.example."; \
	  echo "Edit .env.uat to enable OIDC."; \
	fi
	docker compose -f docker-compose.uat.yml -f docker-compose.uat.socketproxy.yml \
	  --profile socketproxy --env-file .env.uat up -d --build
	@echo
	@echo "Cooker UAT (socket-proxy) ready at http://localhost:8080"

# Variant of uat-up that adds Keycloak as an OIDC IdP and pre-seeds a
# realm with two users (alice/alice = admin, bob/bob = viewer). See
# docker-compose.uat.keycloak.yml for the topology and the Linux
# /etc/hosts requirement for host.docker.internal.
uat-up-with-keycloak:
	@if [ ! -f .env.uat ]; then \
	  cp .env.uat.example .env.uat; \
	  echo "COOKER_SECRET_KEY=$$(head -c 32 /dev/urandom | base64)" >> .env.uat; \
	  echo "DOCKER_GID=$(DOCKER_GID)" >> .env.uat; \
	  echo "Created .env.uat from .env.uat.example (DOCKER_GID=$(DOCKER_GID))."; \
	fi
	@# Append Keycloak OIDC config (idempotent — only added if absent).
	@if ! grep -q '^COOKER_OIDC_ENABLED=true' .env.uat; then \
	  echo "" >> .env.uat; \
	  echo "# Keycloak OIDC config injected by make uat-up-with-keycloak" >> .env.uat; \
	  echo "COOKER_OIDC_ENABLED=true" >> .env.uat; \
	  echo "COOKER_OIDC_ISSUER_URL=http://host.docker.internal:8081/realms/cooker" >> .env.uat; \
	  echo "COOKER_OIDC_CLIENT_ID=cooker" >> .env.uat; \
	  echo "COOKER_OIDC_CLIENT_SECRET=" >> .env.uat; \
	  echo "COOKER_OIDC_REDIRECT_URL=http://localhost:8080/callback" >> .env.uat; \
	  echo "COOKER_ALLOWED_ORIGINS=http://localhost:8080" >> .env.uat; \
	  echo "VITE_OIDC_ENABLED=true" >> .env.uat; \
	  echo "VITE_OIDC_AUTHORITY=http://host.docker.internal:8081/realms/cooker" >> .env.uat; \
	  echo "VITE_OIDC_CLIENT_ID=cooker" >> .env.uat; \
	  echo "VITE_OIDC_REDIRECT_URI=http://localhost:8080/callback" >> .env.uat; \
	  echo "VITE_OIDC_POST_LOGOUT_REDIRECT_URI=http://localhost:8080" >> .env.uat; \
	  echo "VITE_OIDC_SCOPE=openid profile email groups" >> .env.uat; \
	  echo "Appended Keycloak OIDC config to .env.uat."; \
	fi
	docker compose -f docker-compose.uat.yml -f docker-compose.uat.keycloak.yml \
	  --profile keycloak --env-file .env.uat up -d --build
	@echo
	@echo "Cooker UAT (Keycloak) ready at http://localhost:8080"
	@echo "  Keycloak admin:  http://localhost:8081  (admin / admin)"
	@echo "  Realm sign-in:   http://host.docker.internal:8081/realms/cooker/account"
	@echo "  Test users:      alice / alice  (admin)"
	@echo "                   bob   / bob    (viewer)"
	@echo
	@echo "Linux operators without Docker Desktop must add to /etc/hosts:"
	@echo "  127.0.0.1 host.docker.internal"
	@echo "(macOS/Windows Docker Desktop resolves this automatically.)"

uat-down:
	-$(UAT_COMPOSE) down -v --remove-orphans
	rm -f .env.uat

uat-logs:
	$(UAT_COMPOSE) logs -f cooker

uat-shell:
	$(UAT_COMPOSE) exec cooker sh

uat-reset: uat-down uat-up

# End-to-end smoke: boot UAT, drive a deterministic pipeline through
# the API, tear down. Uses the no-op "custom" stage so it doesn't
# depend on the build/push/deploy adapters working in the host
# environment. Requires curl + jq on PATH.
#
# Override the API host with COOKER_API=http://… or the timeouts with
# COOKER_E2E_READY_TIMEOUT / COOKER_E2E_RUN_TIMEOUT (seconds).
test-e2e:
	@command -v curl >/dev/null 2>&1 || { echo "missing required tool: curl"; exit 2; }
	@command -v jq   >/dev/null 2>&1 || { echo "missing required tool: jq"; exit 2; }
	@echo "==> booting UAT stack"
	$(MAKE) uat-up
	@trap '$(MAKE) uat-down >/dev/null 2>&1 || true' EXIT INT TERM; \
	  echo "==> running e2e harness" && \
	  bash scripts/e2e/run.sh

clean:
	rm -rf bin/ $(FRONTEND_DIR)/dist $(FRONTEND_DIR)/node_modules
