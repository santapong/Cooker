-- Application rollback does not require reversing registry authority. Refuse
-- schema reversal while grants or accepted pins would be lost. Match admission
-- lock order (job before registry) so an enqueue cannot cross the check/drop.
DO $$
DECLARE has_pins boolean;
BEGIN
  IF to_regclass('jobs') IS NOT NULL THEN
    LOCK TABLE jobs IN ACCESS EXCLUSIVE MODE;
    SELECT EXISTS (
      SELECT 1 FROM jobs j
      CROSS JOIN LATERAL jsonb_each(
        CASE WHEN jsonb_typeof(j.payload) = 'object' THEN j.payload ELSE '{}'::jsonb END
      ) AS authority(key, value)
      -- Go JSON struct matching folds ASCII case plus long-s/Kelvin forms.
      -- Translate explicitly so the guard does not depend on database locale.
      -- Inspect every matching key; an empty alias cannot conceal another pin.
      WHERE translate(authority.key, 'ABCDEFGHIJKLMNOPQRSTUVWXYZſK', 'abcdefghijklmnopqrstuvwxyzsk') = 'registryauthority'
        AND authority.value <> 'null'::jsonb
        AND (
          jsonb_typeof(authority.value) <> 'object'
          OR EXISTS (
            SELECT 1 FROM jsonb_each(
              CASE WHEN jsonb_typeof(authority.value) = 'object' THEN authority.value ELSE '{}'::jsonb END
            ) AS pins(key, value)
            WHERE translate(pins.key, 'ABCDEFGHIJKLMNOPQRSTUVWXYZſK', 'abcdefghijklmnopqrstuvwxyzsk') = 'connections'
              AND pins.value <> '{}'::jsonb
          )
        )
    ) INTO has_pins;
    IF has_pins THEN
      RAISE EXCEPTION 'registry authority schema is still referenced by accepted jobs';
    END IF;
  END IF;
  LOCK TABLE registry_configs IN ACCESS EXCLUSIVE MODE;
  IF EXISTS (
    SELECT 1 FROM registry_configs
    WHERE allowed_app_ids <> '[]'::jsonb OR allowed_pipeline_ids <> '[]'::jsonb
  ) THEN
    RAISE EXCEPTION 'registry authority schema still contains explicit grants';
  END IF;
END;
$$;
DROP TRIGGER IF EXISTS registry_config_generation ON registry_configs;
DROP FUNCTION IF EXISTS cooker_registry_generation();
ALTER TABLE registry_configs DROP CONSTRAINT IF EXISTS registry_app_grants_array,
  DROP CONSTRAINT IF EXISTS registry_pipeline_grants_array,
  DROP COLUMN IF EXISTS version, DROP COLUMN IF EXISTS allowed_app_ids,
  DROP COLUMN IF EXISTS allowed_pipeline_ids;
-- Keep the sequence even in a legacy-only reversal. A later reapplication
-- cannot reuse previously issued registry generations after edits/deletions.
