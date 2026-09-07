# Quickstart

Start by choosing whether you want to explore the interface or exercise real
builds and deployments. Both use port **8080** for the API and frontend. Build
time depends on your machine and image/dependency caches.

## Explore the interface

Requires Git and Docker with Compose. From a new checkout:

```sh
git clone --branch develop https://github.com/santapong/Cooker.git
cd Cooker
docker compose up --build
```

Open <http://localhost:8080>. The development stack includes Cooker, PostgreSQL
and Redis. It injects a dev admin because authentication is disabled and uses
no-op build/push/deploy backends. Use it in a trusted local environment. It cannot
complete real App deployments. No Vite server runs on port 5173 in this stack.

If ports 8080, 5432 or 6379 are already occupied, change the published ports in a
local Compose override. Do not stop unrelated services just to free those ports.
For frontend development with hot reload, run `npm ci` then `npm run dev` from
`frontend/` alongside a backend; the Vite URL is a separate development option.

Stop the development stack with `docker compose down`. Its database volume
survives unless you explicitly add `-v`.

## Exercise a real deployment in UAT

Read the [UAT prerequisites](../../guides/UAT.md#prerequisites) first. The UAT stack
uses the host Docker daemon, a local registry, PostgreSQL and k3s. It needs Docker
access, `make`, available ports, disk/RAM for builds, and the documented registry
configuration. Do not assume the exploration stack above has these capabilities.

```sh
make uat-up
```

This builds the image and creates `.env.uat` when absent, including a generated
secret key and the host Docker group ID. Open <http://localhost:8080> when the
stack is ready. Stop the exploration stack first if it occupies the same port.

### Import a Compose repository

![The five import steps](../../images/compose-workflow.svg)

1. Go to **Apps → New app**. Select an approved GitHub installation/repository or
   enter a public `owner/repo`, then a branch or tag.
2. Select the discovered base Compose file and any overrides in application
   order. Set profiles and non-secret interpolation inputs.
3. Inspect the preview: services, dependencies, build contexts and Dockerfiles.
4. Choose a prefix such as `shop-uat`, a configured target, image registry and
   optional Environment/database bindings. Resolve every blocking diagnostic.
5. Review the names and pinned commit, then **Save app**. Start execution with
   **Deploy** on the App page.

For a repository with a single Dockerfile, use **Use a single Dockerfile** on the
first step. For the exact GitHub App settings, ECS/Cloud SQL example and supported
Compose subset, follow [GitHub Compose deployment](../../guides/GITHUB-COMPOSE-DEPLOYMENT.md).

Source builds currently need Docker builder + Docker pusher. Production mode
rejects those settings; the reviewed source-build flow is a dev/UAT candidate.
Native Docker Compose also needs Compose v2 inside the backend environment; the
bundled image's Docker CLI alone is insufficient. Kubernetes UAT needs the
configured deployer and working k3s credentials.

### Verify the outcome

Inspect the App deployment graph and stage logs. For Kubernetes, use
`make uat-shell` and `kubectl get deployments,services -n <namespace>` to check
the **prefixed names shown in your review**. For cloud destinations, inspect the
provider's workload status and container logs as well.

Reachability requires your chosen exposure mechanism; the generated Service does
not create ingress or a public URL. If using `kubectl port-forward`, run it where
the client can reach its listening port. A loopback forward inside the Cooker
container is not automatically reachable from the host.

For an external database, verify a designated UAT read/write from the deployed
application. Runtime readiness alone does not prove the database connection.

## Stop or reset UAT

To stop while preserving UAT volumes and `.env.uat`:

```sh
docker compose -f docker-compose.uat.yml --env-file .env.uat stop
```

`make uat-down` removes the UAT containers **and volumes**, and deletes `.env.uat`.
`make uat-reset` performs that destructive reset and starts again. Use them only
when you intend to discard the test data. They do not remove workloads previously
deployed into an external cloud account.

## Next steps

- [Compose library and registries](../guides/compose-library.md): reopen saved stacks.
- [Apps, pipelines and environments](../concepts/apps.md): understand the model.
- [First pipeline](../guides/first-pipeline.md): author a custom DAG.
- [UAT acceptance](../../guides/GITHUB-COMPOSE-DEPLOYMENT.md#uat-acceptance): test a real account and target.
- [Troubleshooting](../operations/troubleshooting.md): diagnose setup failures.
