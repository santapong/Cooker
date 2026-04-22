.PHONY: all build clean dev test lint uat-up uat-down uat-logs uat-shell uat-reset

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

uat-up:
	@[ -f .env.uat ] || echo "COOKER_SECRET_KEY=$$(head -c 32 /dev/urandom | base64)" > .env.uat
	$(UAT_COMPOSE) up -d --build
	@echo
	@echo "Cooker UAT ready at http://localhost:8080"
	@echo "  logs:  make uat-logs"
	@echo "  shell: make uat-shell"
	@echo "  down:  make uat-down"

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
