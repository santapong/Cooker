-- Refuse silent loss of admitted native history or source-bound definitions.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pipelines WHERE source IS NOT NULL AND source<>'null'::jsonb)
 OR EXISTS(SELECT 1 FROM trigger_delivery_decisions WHERE target_kind='pipeline')
 OR EXISTS(SELECT 1 FROM trigger_rules WHERE body->>'targetKind'='pipeline')
 THEN RAISE EXCEPTION 'native source records require a data-preserving export before rollback'; END IF;
END $$;
DROP INDEX trigger_delivery_decisions_target;
ALTER TABLE trigger_delivery_decisions DROP CONSTRAINT trigger_delivery_decisions_target_kind_check;
ALTER TABLE trigger_delivery_decisions ADD CONSTRAINT trigger_delivery_decisions_target_kind_check CHECK(target_kind IN ('','app'));
ALTER TABLE pipelines DROP COLUMN source;
