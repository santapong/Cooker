.PHONY: all build clean dev test lint uat-up uat-up-socketproxy uat-down uat-logs uat-shell uat-reset

# Variables
BINARY_NAME=cooker
BACKEND_DIR=backend
FRONTEND_DIR=frontend
DEPLOY_DIR=deploy

all: build

# --- Backend ---
build-backend:
	cd $(BACKEND_DIR) && CGO_ENABLED=0 GOOS=linux go build -o ../bin/$(BINARY_NAME) ./cmd/cooker/

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
		go test -tags oci_conformance -v -run TestPushConformance ./internal/pusher/...
	@echo "Push OK; building upstream conformance binary..."
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

uat-down:
	-$(UAT_COMPOSE) down -v --remove-orphans
	rm -f .env.uat

uat-logs:
	$(UAT_COMPOSE) logs -f cooker

uat-shell:
	$(UAT_COMPOSE) exec cooker sh

uat-reset: uat-down uat-up

clean:
	rm -rf bin/ $(FRONTEND_DIR)/dist $(FRONTEND_DIR)/node_modules
