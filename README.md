# Cooker

A web-based CI/CD management tool with a **graph-based UI** for visually building and executing pipelines that build **OCI-compliant** Docker images, push to registries, and deploy to **Kubernetes** across Dev, Staging, and Production environments.

## Architecture

```
Browser (React + React Flow)  ←→  Go API Server (Gin)  ←→  Docker / K8s / OCI Registries
```

- **Frontend**: React + TypeScript + React Flow (visual DAG editor) + Zustand
- **Backend**: Go + Gin + Docker SDK + client-go + go-containerregistry
- **Database**: PostgreSQL + Redis
- **Auth**: OIDC/OAuth 2.0 with PKCE (Keycloak, Okta, Azure AD, Google, GitHub)

## OCI Compliance

All operations follow OCI standards:

| Spec | Reference |
|------|-----------|
| **image-spec** v1.1 | [github.com/opencontainers/image-spec](https://github.com/opencontainers/image-spec) |
| **runtime-spec** v1.2 | [github.com/opencontainers/runtime-spec](https://github.com/opencontainers/runtime-spec) |
| **distribution-spec** v1.1 | [github.com/opencontainers/distribution-spec](https://github.com/opencontainers/distribution-spec) |

## Quick Start

```bash
# Local development
docker compose up

# Frontend: http://localhost:5173
# Backend:  http://localhost:8080
# API:      http://localhost:8080/api/v1
```

## Features

- **Visual Pipeline Builder** - Drag-and-drop graph editor with React Flow
- **Multi-Environment Deployment** - Dev → Staging → Production with configurable auto/manual promotion
- **Docker Management** - Build, list, inspect OCI images and manage containers
- **OCI Registry Integration** - Push/pull via distribution-spec, browse tags, inspect manifests, referrers API
- **Kubernetes Dashboard** - Manage workloads, scale, restart, view logs across clusters
- **SSO Authentication** - OIDC with RBAC (admin, operator, viewer roles)
- **Live Execution** - WebSocket-powered real-time pipeline status and log streaming

## Project Structure

```
├── frontend/           React + TypeScript + Vite
├── backend/            Go API server
│   ├── internal/       Server, handlers, services, models, OCI, auth, store
│   └── pkg/            Reusable DAG runner, OCI utilities
├── deploy/
│   ├── docker/         Dockerfiles (multi-stage, dev)
│   ├── kubernetes/     Raw K8s manifests
│   └── helm/cooker/    Helm chart
└── api/                OpenAPI spec
```

## Deployment

```bash
# Docker image
make docker-build

# Kubernetes (raw manifests)
kubectl apply -f deploy/kubernetes/

# Helm
helm install cooker deploy/helm/cooker/
```

## License

MIT
