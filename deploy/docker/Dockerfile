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
RUN apk add --no-cache ca-certificates
COPY --from=backend-build /cooker /usr/local/bin/cooker
COPY --from=frontend-build /app/frontend/dist /usr/share/cooker/static

EXPOSE 8080

LABEL org.opencontainers.image.title="Cooker"
LABEL org.opencontainers.image.description="CI/CD Management Tool for Docker & Kubernetes"
LABEL org.opencontainers.image.source="https://github.com/cooker-ci/cooker"

ENTRYPOINT ["cooker"]
