# Multi-stage Dockerfile for Cooker
# Builds both frontend and backend into a single OCI-compliant image

# --- Frontend build ---
FROM node:20-alpine AS frontend-build
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --ignore-scripts
COPY frontend/ ./
# Vite reads VITE_* env vars at build time and bakes them into the
# bundle. The OIDC provider URL and client ID are public values
# (the client secret is never used in the browser PKCE flow), so it
# is safe to bake them into the JS bundle.
ARG VITE_OIDC_ENABLED=false
ARG VITE_OIDC_AUTHORITY=
ARG VITE_OIDC_CLIENT_ID=
ARG VITE_OIDC_REDIRECT_URI=
ARG VITE_OIDC_POST_LOGOUT_REDIRECT_URI=
ARG VITE_OIDC_SCOPE=
ENV VITE_OIDC_ENABLED=$VITE_OIDC_ENABLED \
    VITE_OIDC_AUTHORITY=$VITE_OIDC_AUTHORITY \
    VITE_OIDC_CLIENT_ID=$VITE_OIDC_CLIENT_ID \
    VITE_OIDC_REDIRECT_URI=$VITE_OIDC_REDIRECT_URI \
    VITE_OIDC_POST_LOGOUT_REDIRECT_URI=$VITE_OIDC_POST_LOGOUT_REDIRECT_URI \
    VITE_OIDC_SCOPE=$VITE_OIDC_SCOPE
RUN npm run build

# --- Backend build ---
FROM golang:1.25-alpine AS backend-build
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
# Runs as a non-root system user (UID/GID 65532 — the standard
# "nonroot" UID that aligns with distroless and K8s securityContext
# defaults). UAT compose passes the host's docker group GID via
# group_add so this user can still access the bind-mounted
# /var/run/docker.sock; see docker-compose.uat.yml and Makefile.
ARG KUBECTL_VERSION=v1.29.4
# Fetch kubectl and verify against the upstream-published SHA256 so a
# compromised mirror or in-flight tampering cannot inject a malicious
# binary that would run with docker.sock and kubeconfig access.
RUN apk add --no-cache ca-certificates git docker-cli \
 && wget -qO /usr/local/bin/kubectl \
      https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl \
 && wget -qO /tmp/kubectl.sha256 \
      https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl.sha256 \
 && echo "$(cat /tmp/kubectl.sha256)  /usr/local/bin/kubectl" | sha256sum -c - \
 && rm /tmp/kubectl.sha256 \
 && chmod +x /usr/local/bin/kubectl \
 && addgroup -g 65532 -S cooker \
 && adduser -u 65532 -S -G cooker -H -h /nonexistent cooker
COPY --from=backend-build --chown=65532:65532 /cooker /usr/local/bin/cooker
COPY --from=frontend-build --chown=65532:65532 /app/frontend/dist /usr/share/cooker/static

EXPOSE 8080

# Liveness signal for runtimes that don't define their own probe
# (Compose has its own healthcheck on the service; K8s uses probes
# from the Deployment manifest). The /health endpoint is unauthenticated.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:8080/health > /dev/null || exit 1

LABEL org.opencontainers.image.title="Cooker"
LABEL org.opencontainers.image.description="CI/CD Management Tool for Docker & Kubernetes"
LABEL org.opencontainers.image.source="https://github.com/cooker-ci/cooker"

USER 65532:65532
ENTRYPOINT ["cooker"]
