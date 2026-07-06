<!-- DRAFT dev.to article -->

# Why We Built a CI/CD Tool with a DAG Editor When YAML Works Fine

*Tags: devops, cicd, golang, opensource*

---

Cooker is an open-source, self-hosted CI/CD tool with a drag-drop graph editor for building OCI images (Kaniko, BuildKit, Buildah) and deploying to Kubernetes, ECS, Cloud Run, Fly.io, and Render — single Go binary, Apache-2.0-licensed, no SaaS, no agents.

That description will prompt the obvious question: YAML CI works. GitHub Actions works. Drone works. Woodpecker works. Why build something with a graph editor?

This article is the honest answer. Not the marketing answer — the design argument. If you disagree at the end, that's a legitimate position.

---

## The Typical Framing Is Wrong

Most articles about "visual CI/CD" frame it as a usability improvement — YAML is hard, a GUI is easier, drag-and-drop is friendlier. That's not why we built Cooker with a graph editor.

We built it because of where the data model lives.

In every YAML-based CI tool, the authoritative description of a pipeline is a text file. The UI — if there is one — reads that file, parses it, and renders something for you to look at. When you want to change the pipeline, you edit the YAML. The UI is a viewer.

In Cooker, the authoritative description of a pipeline is the graph. The JSONB blob that backs a pipeline node in Postgres is exactly what the React Flow editor produces and exactly what the Go DAG runner consumes. There is no YAML-to-internal-model translation. There is no "render the YAML into a view." The graph is the model.

That is the design decision. Everything else follows from it.

---

## What Follows from Making the Graph the Model

**The frontend and the executor share one data type.**

The DAG runner in `pkg/dagrunner` walks the same node-and-edge structure that the React Flow editor writes. When you draw an edge from Build to Push in the editor, the executor sees that edge as a dependency: Push does not start until Build succeeds. There is no intermediate format. The `validate` endpoint (`POST /api/v1/pipelines/:id/validate`) runs the same DAG validation logic the executor uses — if it's invalid in the editor, it's invalid at runtime, and the error message is the same.

**Serialisation is obvious.**

A YAML-first tool has a serialisation question: when does the YAML become the internal model? At parse time? At pipeline trigger? When a step starts? Different tools make different choices, and those choices produce subtle bugs when the YAML references a variable that hasn't been resolved yet, or when a conditional branch changes the effective graph structure.

Because Cooker's pipeline is already a validated DAG before it runs, the executor never encounters a malformed graph at runtime. `ValidateManifest` in `internal/oci` and `ValidateIndex` run at build time. `ValidateManifest` at push time confirms the output matches what the executor was told to produce. The validation chain is explicit and testable at every step.

**The UI can show you something real.**

When a pipeline is running, the graph editor updates in real time. Each node goes from grey to blue to green (or red). The edges light up as they activate. This is not a table of steps being polled for status — it is the live state of the graph, because the graph is the model and the WebSocket events carry node-level state transitions.

This matters for debugging. When a fan-out step — where multiple independent stages run in parallel — has one branch fail and two succeed, you see it as a graph with one red node and two green nodes. The structural cause of the failure is visible.

---

## The Trade-Offs We Made

This is not a free choice. Making the graph the authoritative model costs things.

**You can't diff a Cooker pipeline in a PR the way you diff a YAML file.**

Pipeline-as-code (the CKR-DSL, described in `docs/reference/protocols.md`) is on the roadmap. Until it ships, a Cooker pipeline lives in the database. You can export the JSON representation; you cannot currently maintain it in a git repo and have Cooker sync from it. If your workflow depends on CI config living in version control alongside your application code, Cooker is not the right fit today. We state this in the README.

**The graph editor is easier to modify at 3am than a YAML file checked into git.**

That is a feature for some teams and a risk for others. A YAML file in a repo has a PR process. The Cooker editor has RBAC (operator role can run pipelines; admin role can edit them), but there is no "PR to modify a pipeline." For teams where the approval process for CI config changes is important, this is a real gap. It is on the roadmap as an audit-log viewer and approval workflow; it does not ship today.

**Single-tenant today.**

The current version is single-tenant: every authenticated user can read every pipeline and environment. This is documented (the security audit is in `docs/audits/2026-05-security-review.md`, public on the repo). Multi-tenancy is a roadmap item. If you are a platform team running Cooker for multiple isolated teams, wait.

---

## Why Not Just Build a Viewer on Top of YAML?

We considered it. A "YAML CI with a nice graph view" is a smaller project. Several tools have built it.

The reason we didn't is the debugging story. When a pipeline fails mid-way through a complex fan-out and you need to understand why three concurrent stages produced two successes and one failure, a viewer that parses the YAML and renders a status grid is useful. A live graph where the failure is structurally visible — where you can see that stage C failed because stage B's output was invalid, and stage B depends on stages A1 and A2, both of which succeeded but produced an incompatible artifact — is better.

Building that debugging experience on top of YAML parsing is possible but forces you to maintain a mapping layer between the text representation and the graph you're drawing. We chose to skip that layer.

---

## The Honest Comparison

If your pipelines are YAML today and you like them there, Woodpecker CI is well-built, Apache-2.0-licensed, and actively maintained. We would recommend it. We don't compete on "YAML CI done better" because we are not doing YAML CI.

If you want to build and deploy containers from a visual pipeline that you can inspect live while it runs, and you want to run it on your own infrastructure without agents or SaaS control planes, Cooker is worth trying.

Try it: `docker compose up` — repo at github.com/santapong/Cooker
