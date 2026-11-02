# Immutable image admission for App deployments

Implementation checkpoint: 28 September 2026. This closes the image-only
Compose tag gap in the durable App path. It is not complete M1 or live private
registry acceptance.

Before acknowledging a new job, the server retrieves the exact accepted Git
commit, resolves the captured environment generation and loads the selected
Compose files/profiles. Each non-build, non-external service receives an image
resolution containing its original normalized reference and a digest reference
for the same repository. Image tags are resolved through the request's matching
Settings registry credential, or anonymously when none is configured. Host
Docker credentials are never consulted by this resolver. Registry failures fail
admission and leave no new runnable job.

Source-build plans also run the configured build/publish capability check at
admission. Unsupported buildpack/detected kinds fail before queuing.

Already pinned SHA-256 references are validated and normalized locally. They do
not need a registry lookup at admission; runtime pull/cache availability remains
an execution check. Multi-platform tags retain the registry's index digest, so
admission does not choose the control plane machine's architecture.

The resulting map is part of `AppDeployPlan`, covered by its canonical queue
payload digest. It is server-generated evidence, not an editable App field.
Only references are persisted; decrypted environment and registry credentials
stay ephemeral. The configuration generation is checked again after source and
registry requests to reject edits during admission.

At execution, Cooker loads the pinned source again and verifies every image-only
service against the accepted map. A missing service, extra service, changed
reference, different repository, mutable result, or substitution of an existing
digest fails before any runtime stage. Validated digests replace images in both
the displayed deployment DAG and the private runtime Compose project. Source
build services still receive their image digest from the verified Push result.
The worker does not resolve the original tags again.

The server requires image evidence even for plans with no image-only services.
Jobs accepted by an older version without that evidence fail with an instruction
to start a new deployment. Direct legacy service calls and explicitly constructed
test queues retain their compatibility behavior; the server always wires the
mandatory preparer. The live Compose acceptance fixture also enables that path
using its already pinned cached images.

## Verification

The local registry fixture publishes a real image index, queues a deployment
from a real local Git fixture, changes the registry tag to another manifest, then
executes through the App deployer with a recording runtime adapter. Assertions
check that the accepted original index digest reaches the runtime request,
private Compose file and saved DAG without a second resolution. A populated
host Docker configuration does not authenticate an anonymous request.

Other cases check immutable input mismatch, missing/extra service pins, wrong
repository, invalid mutable result, already pinned digest substitution,
registry failure, environment edit during admission, and refusal to execute an
older job with missing image evidence. These are real local Git/HTTP registry
and service contract tests. They do not launch a real workload from the fixture
image or establish hosted/private source or production registry acceptance.

The full service and server packages passed with `-race` from a frozen backend
copy after these changes: `/mnt/data/cooker-image-pins-tests-20260928.log`.
The corresponding changed-source hashes are recorded in
`/mnt/data/cooker-image-pins-source-sha256-20260928.json`.

## Limits

This is image identity pinning, not image signing, provenance verification,
vulnerability scanning, registry retention or pull permission provisioning.
Garbage collection or revoked runtime pull access can still make an accepted
digest unavailable; Cooker must report deployment failure rather than replace
it with the current tag. Existing target pull-authentication capabilities remain
separate from admission authentication. Registry credential authority is still
instance-scoped as described in [the build handoff contract](buildkit-handoff.md).

A new manual deployment can resolve newer image tags even at the same Git
commit. An existing accepted intent retains its original payload and image
map. Automated missed-change detection tracks source revisions; it does not
create new deployments merely because an external image tag changed.
