# Multi-stage Dockerfile for Cooker
# Builds both frontend and backend into a single OCI-compliant image

# --- Frontend build ---
FROM node:20-alpine AS frontend-build
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --ignore-scripts
COPY frontend/ ./
RUN npm run build

# --- Backend build ---
FROM golang:1.22-alpine AS backend-build
WORKDIR /app/backend
COPY backend/go.mod backend/go.sum* ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /cooker ./cmd/cooker/

# --- Final image ---
FROM alpine:3.19
# git:        used by internal/source/github to clone App repos
# docker-cli: Cooker shells `docker build/push` via a bind-mounted sock
# kubectl:    pinned static binary; internal/deployer/kubectl.go shells to it
#
# UAT runs the container as root so the bind-mounted docker.sock
# (host-owned GID) and the read-only kubeconfig mount Just Work on
# Linux, macOS, and WSL. Production Helm/K8s deploys override the
# user — see deploy/helm/cooker.
ARG KUBECTL_VERSION=v1.29.4
RUN apk add --no-cache ca-certificates git docker-cli \
 && wget -qO /usr/local/bin/kubectl \
      https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl \
 && chmod +x /usr/local/bin/kubectl
COPY --from=backend-build /cooker /usr/local/bin/cooker
COPY --from=frontend-build /app/frontend/dist /usr/share/cooker/static

EXPOSE 8080

LABEL org.opencontainers.image.title="Cooker"
LABEL org.opencontainers.image.description="CI/CD Management Tool for Docker & Kubernetes"
LABEL org.opencontainers.image.source="https://github.com/cooker-ci/cooker"

ENTRYPOINT ["cooker"]
