# Registries

Cooker pushes built images to OCI-compliant registries. The pusher is selected by `COOKER_PUSHER`:

- `noop` — no push (default).
- `docker` — shells out to the local Docker daemon's push code.
- `crane` — uses `go-containerregistry`. Works against any OCI-compliant registry.

`crane` is the production-recommended pusher. `docker` is dev-host only.

## Configure a registry

Registries are stored as `RegistryConfig` records (`POST /api/v1/settings/registries`). Each record has:

- A name.
- A URL (e.g. `ghcr.io`, `us-docker.pkg.dev/my-project/my-repo`).
- Optional username + password / token (sealed via `Codec`).

> **Partial frontend.** The Settings / Registries page exists but is rudimentary. Most operators wire registries via API today. Roadmap `A4` polishes this.

### Add via API

```bash
curl -X POST https://cooker.example.com/api/v1/settings/registries \
     -H 'Authorization: Bearer <jwt>' \
     -H 'Content-Type: application/json' \
     -d '{
       "name":"ghcr",
       "url":"ghcr.io/your-org",
       "username":"your-username",
       "password":"<github-token>"
     }'
```

`POST` requires `admin` role.

## Per-registry setup

### Docker Hub (`docker.io`)

```json
{
  "name":   "dockerhub",
  "url":    "docker.io/your-namespace",
  "username":"your-dockerhub-username",
  "password":"<dockerhub access token>"
}
```

Use an access token, not your account password. Generate at https://hub.docker.com/settings/security.

### GitHub Container Registry (`ghcr.io`)

```json
{
  "name":   "ghcr",
  "url":    "ghcr.io/your-github-username-or-org",
  "username":"your-github-username",
  "password":"<github PAT with write:packages>"
}
```

GHCR uses GitHub PATs (classic or fine-grained). The classic PAT needs the `write:packages` scope; fine-grained needs the "Packages: read & write" permission on the target org.

### AWS Elastic Container Registry (ECR)

ECR uses short-lived tokens, not static credentials. The supported path today is **static IAM credentials with `aws ecr get-login-password` rotated externally**:

```json
{
  "name":   "ecr",
  "url":    "<account>.dkr.ecr.<region>.amazonaws.com/<repo>",
  "username":"AWS",
  "password":"<output of `aws ecr get-login-password`>"
}
```

> **Partial.** Native IRSA / instance-profile auth for ECR is not yet wired into the pusher. Roadmap `A5` tracks "ECR with IRSA". Today operators run a CronJob that calls `get-login-password` and updates the secret via `PUT /api/v1/settings/registries`. Acceptable but operationally painful.

### Google Artifact Registry / GCR

Same shape as ECR for now — use a service account key:

```json
{
  "name":   "gar",
  "url":    "<region>-docker.pkg.dev/<project>/<repo>",
  "username":"_json_key",
  "password":"<contents of service-account-key.json>"
}
```

> **Partial.** Native Workload Identity auth is roadmap `A6`.

### Quay.io

```json
{
  "name":   "quay",
  "url":    "quay.io/your-namespace",
  "username":"your-quay-username",
  "password":"<robot token>"
}
```

Use a robot account, not your user account.

### Harbor

```json
{
  "name":   "harbor",
  "url":    "harbor.example.com/<project>",
  "username":"harbor-robot",
  "password":"<robot token>"
}
```

Harbor must have its TLS cert trusted by the Cooker container's CA bundle. If you're using a private CA, mount it into the pod at `/etc/ssl/certs/ca-certificates.crt`.

### Self-hosted Distribution (`registry:5000`)

UAT compose uses this. With auth-less Distribution:

```json
{
  "name":"uat",
  "url":"registry:5000/cooker"
}
```

For production deployments of Distribution, configure HTPasswd or token auth and set `username` / `password` accordingly.

## How a Push stage uses the config

A Push stage (`StageTypePush`) resolves its `registry` field against the configured registries by **URL prefix match**. The first registry whose URL is a prefix of the stage's `registry` value wins. This means:

- Stage config `registry: ghcr.io/your-org` matches a registry config with `url: ghcr.io/your-org` exactly, or with `url: ghcr.io` (less specific).
- If no registry config matches, the push uses no credentials. For public registries (the bundled `registry:5000`), this works. For real registries, the push will fail with `denied: requested access to the resource is denied`.

## Pull-through (downstream of Cooker)

Cooker pushes; your K8s cluster pulls. The pull side is a Kubernetes concern, not a Cooker one:

- For public registries: nothing to configure.
- For private registries: create an `imagePullSecret` in the target namespace and reference it in your Deployment's `imagePullSecrets`. See [Kubernetes deploy](kubernetes-deploy.md).

## OCI conformance

Cooker pushes via `go-containerregistry`, which implements the OCI distribution-spec v1.1 natively. The push path is exercised against the upstream conformance suite weekly via `.github/workflows/oci-conformance.yml`. The referrers API is supported for SBOM / signature attachment.

## Cross-references

- **[Stages: Push](../concepts/stages.md#push-stagetypepush)** — what a Push stage configures.
- **[Kubernetes deploy](kubernetes-deploy.md)** — the pull side.
- **[Reference: env vars](../reference/env-vars.md#general)** — `COOKER_REGISTRY` default.
