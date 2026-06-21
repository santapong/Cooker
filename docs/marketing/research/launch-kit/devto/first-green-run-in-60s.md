<!-- DRAFT dev.to article -->

# From `docker compose up` to Your First Green Pipeline Run in 60 Seconds

*Tags: devops, cicd, tutorial, docker*

---

Cooker is an open-source, self-hosted CI/CD tool with a drag-drop graph editor for building OCI images (Kaniko, BuildKit, Buildah) and deploying to Kubernetes, ECS, Cloud Run, Fly.io, and Render — single Go binary, Apache-2.0-licensed, no SaaS, no agents.

This article is a literal walkthrough. The 60-second hero cast on the README shows it faster; this shows you every step with the context you'd want if something went wrong. Prerequisites are Docker and `git`.

---

## Step 0: Clone and Start

```bash
git clone https://github.com/santapong/Cooker.git
cd Cooker
docker compose up
```

The compose stack brings up three services: the Cooker backend (Go binary on port 8080), a Postgres instance for pipeline state, and a Redis instance for WebSocket ticket storage and rate limiting. The frontend Vite dev server runs on port 5173 in local dev mode.

Wait for the log line that reads something like:

```
cooker  | {"time":"...","level":"INFO","msg":"server started","addr":":8080"}
```

That is the signal that the API is ready.

Open `http://localhost:5173` in your browser. You should see the Cooker UI with an empty pipelines list. By default, OIDC authentication is off in local mode — the backend injects a dev admin user, so you land directly in the app without a sign-in step. This is intentional; wiring up an OIDC provider is a production concern, not a first-run concern.

---

## Step 1: Create a Pipeline

Click "New pipeline" (or the equivalent empty-state CTA on the pipelines page). Give it a name — "hello-pipeline" is fine.

You land on the graph editor. The canvas is empty. You'll see a panel on the right for pipeline settings; for now, ignore it.

---

## Step 2: Add Nodes

The node palette is on the left side of the canvas. Drag a **Build** node onto the canvas. Then drag a **Push** node. Draw an edge from Build to Push by hovering over the Build node's right handle until a connector appears, then dragging to the Push node's left handle.

What you have now is a two-stage pipeline: build an OCI image, then push it to a registry.

**Configure the Build node:** Click on it. A sidebar opens with the stage configuration. For a first run, you need at minimum:

- A Dockerfile path (use `.` for the current directory, or point at a Dockerfile in a repo you have access to)
- An image name (e.g. `my-registry.example.com/hello:latest`)

For the Push node, the image name is pre-populated from the Build node if you connected them correctly. You'll need registry credentials, but for a first smoke-test against a local registry, you can skip this.

If you just want to see a green run without a real build, drag a **Custom** node instead of Build and Push, and enter `echo "hello world"` as the command. This runs a shell command and turns green when it exits 0. It is the fastest path to a confirmed working install.

---

## Step 3: Validate and Run

Before clicking Run, click the **Validate** button in the pipeline toolbar. This calls `POST /api/v1/pipelines/:id/validate` on the backend. The validator runs the same DAG validation logic the executor uses: it checks for cycles, disconnected nodes, and missing required fields. If anything is wrong, the node with the problem gets highlighted.

When validation passes, click **Run**.

---

## Step 4: Watch It Execute

The graph updates in real time as the run progresses. Each node transitions:

- Grey: waiting (dependency not yet satisfied)
- Blue: running
- Green: succeeded
- Red: failed

The right panel shows the live log stream for whichever node is currently active. This is a WebSocket connection: the frontend calls `POST /api/v1/ws-tickets` to get a single-use 60-second ticket, then opens `wss://.../ws/stage-logs/:runId/:stageId?ticket=<value>`. If you see the log panel say "connecting..." for more than a few seconds, check the browser console — a WebSocket ticket timeout produces a visible error.

When the final node turns green, the run is complete. The run is persisted in Postgres; you can reload the page and see the same run state.

---

## What Just Happened Under the Hood

The run you triggered followed this path:

1. `POST /api/v1/pipelines/:id/run` created a Run record in Postgres with status `pending`.
2. The executor read the pipeline's DAG from the store, identified the root nodes (those with no incoming edges), and began executing them.
3. Each stage's status transition (pending → running → succeeded/failed) was written to Postgres and broadcast over the Redis pub/sub channel.
4. The React frontend received those status updates over the WebSocket and updated the graph in real time.
5. The orphan sweep (`COOKER_ORPHAN_SWEEP_INTERVAL`, default 60s) will periodically reap any run that gets stuck — useful if the process is killed mid-run.

---

## If It Didn't Work

**Nothing loads at `localhost:5173`:** The frontend dev server may not have started. Check `docker compose logs frontend`. In UAT mode (`make uat-up`), both the API and the React app are served from the Go binary at port 8080, not from a separate Vite server.

**The run is stuck in "running" for more than a minute:** Check `GET /health/ready`. A non-200 response from the health endpoint means Postgres or Redis is not reachable. The response body breaks down which check failed.

**The WebSocket log panel shows "disconnected" immediately:** WebSocket tickets are single-use and expire after 60 seconds. If the tab was idle when you opened the log panel, the frontend may have fetched a ticket and then waited too long before using it. Refreshing the run page fetches a new ticket.

**Build step fails with "docker refused":** The `docker` builder is refused in production mode (`COOKER_ENV=production`). In local dev mode it should work if the Docker socket is accessible. If you are on a machine where Docker Desktop uses a non-standard socket path, set `COOKER_DOCKER_SOCKET` to the correct path.

---

## Next Steps

That first green run confirms the install works. From here:

- Add a real Dockerfile and a registry credential to your Build and Push stages
- Wire a GitHub webhook (`POST /api/v1/webhooks/apps/:id`) to trigger runs on push
- Add a **Deploy** node to deploy the built image to a Kubernetes cluster or a cloud target

The full pipeline — build, push, deploy to Kubernetes — is the same process as above with one more node and one more edge. The Kubernetes deployer calls `kubectl apply` using the in-cluster ServiceAccount or a kubeconfig you provide.

Try it: `docker compose up` — repo at github.com/santapong/Cooker
