# Your first pipeline

This walks through building a pipeline from scratch in the graph editor: connect a repo, build an image, push it, deploy to Kubernetes. By the end you'll have a green run.

Prerequisites: Cooker running (see [Quickstart](../getting-started/quickstart.md) or [Helm install](../getting-started/helm-install.md)) and at least one Environment configured.

If all you want is "deploy on push", an [App](../concepts/apps.md) is faster. Pipelines are for when you need control over the DAG shape.

## Step 1 — Create the Environment

You need somewhere to deploy. Go to **Environments -> New** and fill:

- **Name:** `dev`
- **Order:** `1`
- **Target type:** `namespace` (we'll target a namespace in the running cluster)
- **Namespace:** `default`
- **Promotion strategy:** `auto` (for now)

Save. You can come back later to add secrets and plain vars.

<!-- SCREENSHOT: the New Environment form filled with the fields above -->

## Step 2 — Configure a registry

Builds need somewhere to push to. Go to **Settings -> Registries -> Add registry** and configure your destination:

| Field | UAT compose | Production (GHCR example) |
|---|---|---|
| URL | `registry:5000/cooker` | `ghcr.io/your-org` |
| Username | *(blank)* | `your-github-username` |
| Password | *(blank)* | A GHCR PAT with `write:packages` |

For UAT, the bundled registry is on `registry:5000` (no auth). For real registries, see [Registries](registries.md).

## Step 3 — Create the Pipeline

Go to **Pipelines -> New** and name it (e.g. `hello-world`). The editor opens with a blank canvas.

Drag a **Build** node from the toolbar onto the canvas. The slide-out config panel opens.

Fill the Build config:

```text
Name:       build
Dockerfile: Dockerfile
Context:    .
Tags:       hello-world:${COMMIT_SHA}
```

`${COMMIT_SHA}` is one of the built-in variables (resolved at run time). Pipeline.Variables and Environment.PlainVars can also be referenced this way.

## Step 4 — Add a Push stage

Drag a **Push** node onto the canvas, to the right of the Build node. Connect them: hover over Build's right-side handle, drag to Push's left-side handle.

Push config:

```text
Name:       push
Registry:   registry:5000/cooker        (or your prod registry)
Repository: hello-world
Tags:       ${COMMIT_SHA}
```

## Step 5 — Add a Deploy stage

Drag a **Deploy** node. Connect Push -> Deploy.

Drop the Deploy node inside the `dev` swimlane — this assigns its `environmentId` to your dev environment.

Deploy config:

```text
Name:         deploy-dev
Namespace:    default
ManifestPath: deploy/k8s/deployment.yaml
```

The manifestPath is a path inside the repo Cooker cloned at the start of the Build stage. The path you put here must point at a real `.yaml` file in your repo. If it doesn't exist, the stage will fail with a readable "file not found" error.

> **Tip.** For testing, a minimal manifest that pulls the just-pushed image works:
>
> ```yaml
> apiVersion: apps/v1
> kind: Deployment
> metadata:
>   name: hello-world
> spec:
>   replicas: 1
>   selector: { matchLabels: { app: hello-world } }
>   template:
>     metadata: { labels: { app: hello-world } }
>     spec:
>       containers:
>         - name: hello-world
>           image: registry:5000/cooker/hello-world:${COMMIT_SHA}
> ```

## Step 6 — Validate

Click **Validate** in the toolbar. The backend runs `validateDAG`:

- No cycles.
- Every edge has both endpoints in the stages list.
- Every stage has a non-empty `name` and `type`.

Errors show as a banner with line citations to the offending stage.

## Step 7 — Save and run

Click **Save** (creates the pipeline; idempotent on re-save). Then **Run**.

The run starts. The DAG colours change:

- Grey = pending
- Blue = running
- Green = success
- Red = failed

Per-stage logs stream live into the side panel. The whole thing should complete in 30-60 seconds for a small repo.

## Step 8 — Verify the deploy

```bash
kubectl get deploy/hello-world -n default
# NAME           READY   UP-TO-DATE   AVAILABLE   AGE
# hello-world    1/1     1            1           30s
```

## Things to add next

- **A Test stage** between Build and Push so unit tests gate the push.
- **A second Deploy stage** in a `staging` swimlane with `Approval` between them.
- **A Notify** stage — but [notifications are partial today](notifications.md); use a Custom stage that `curl`s your Slack webhook as the workaround.

## Common first-pipeline mistakes

| Symptom | Likely cause |
|---|---|
| Build fails with "no such file or directory: Dockerfile" | Your `context` doesn't include the Dockerfile, or `dockerfile` path is wrong. |
| Push fails with `denied: requested access to the resource is denied` | Registry credentials missing or wrong. See [Registries](registries.md). |
| Deploy fails with `the server doesn't have a resource type "Deployment"` | Wrong kubeconfig context or namespace doesn't exist. See [Kubernetes deploy](kubernetes-deploy.md). |
| Stage hangs in `running` forever | Run deadline not reached yet; or your container's entrypoint never exits. Check the live logs. See [builds stuck](../troubleshooting/builds-stuck.md). |
| `403 mfa_required` on run start | Step-up MFA is configured and your last sign-in didn't carry an MFA `acr`. Sign in again with the IdP's MFA flow. |

## Cross-references

- **[Stages](../concepts/stages.md)** — every type with its full config schema.
- **[Promotions](promotions.md)** — adding Staging -> Prod with approval gates.
- **[GitHub webhooks](github-webhooks.md)** — auto-run on push to `main`.
- **[Pipelines concept](../concepts/pipelines.md)** — DAG semantics if something behaves unexpectedly.
