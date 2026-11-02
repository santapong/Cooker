-- Prospective authored native sources. Existing pipelines remain source-unbound.
ALTER TABLE pipelines ADD COLUMN source JSONB;
ALTER TABLE trigger_delivery_decisions DROP CONSTRAINT trigger_delivery_decisions_target_kind_check;
ALTER TABLE trigger_delivery_decisions ADD CONSTRAINT trigger_delivery_decisions_target_kind_check CHECK(target_kind IN ('','app','pipeline'));
CREATE INDEX trigger_delivery_decisions_target ON trigger_delivery_decisions(target_kind,target_id,delivery_id) WHERE target_id<>'';
-- Rule/review/source metadata use existing bounded JSONB snapshots. Lost legacy
-- reviews or targets are not inferred from current definitions.
