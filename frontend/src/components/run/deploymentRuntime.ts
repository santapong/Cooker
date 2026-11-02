import type { Stage } from '../../types/pipeline';

/** Match recorded deploy services: Compose name first, otherwise the stage ID. */
export function deploymentRuntimeService(stage: Stage | null | undefined) {
  if (stage?.type !== 'deploy' || !stage.config.runtimeName) return null;
  const runtime = stage.config.deployRuntime;
  if (!runtime || !['docker', 'compose', 'kubernetes', 'ecs', 'cloud-run'].includes(runtime)) return null;
  const id = stage.config.composeServiceName || stage.id;
  if (!id) return null;
  // Immutable recorded live-log sessions currently support Docker hosts only.
  return { id, canStreamLogs: runtime === 'docker' || runtime === 'compose' };
}
