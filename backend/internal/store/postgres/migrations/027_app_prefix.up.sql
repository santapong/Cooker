-- Only new, explicitly scoped deployments participate. Legacy rows are unchanged.
CREATE UNIQUE INDEX apps_deployment_prefix_unique ON apps (
  (deploy_target->>'prefix'),
  (deploy_target->>'kind'),
  (CASE WHEN deploy_target->>'kind' = 'kubernetes' THEN COALESCE(NULLIF(BTRIM(deploy_target->>'namespace'), ''), 'default') ELSE '' END),
  (COALESCE(deploy_target->>'hostId', ''))
) WHERE COALESCE(deploy_target->>'prefix', '') <> '';
