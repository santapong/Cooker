-- Config generations cannot be reused after deletion/recreation. Existing
-- connections receive no grants: naming a saved credential is not authority.
-- A legacy-only schema reversal retains this sequence. Reapplication must
-- continue its high-water mark rather than reusing an accepted generation.
CREATE SEQUENCE IF NOT EXISTS registry_config_generation_seq AS bigint;
ALTER TABLE registry_configs
  ADD COLUMN version bigint NOT NULL DEFAULT nextval('registry_config_generation_seq'),
  ADD COLUMN allowed_app_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD COLUMN allowed_pipeline_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
  ADD CONSTRAINT registry_app_grants_array CHECK (jsonb_typeof(allowed_app_ids) = 'array'),
  ADD CONSTRAINT registry_pipeline_grants_array CHECK (jsonb_typeof(allowed_pipeline_ids) = 'array');
CREATE FUNCTION cooker_registry_generation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.version := nextval('registry_config_generation_seq');
  RETURN NEW;
END;
$$;
CREATE TRIGGER registry_config_generation BEFORE INSERT OR UPDATE ON registry_configs
FOR EACH ROW EXECUTE FUNCTION cooker_registry_generation();
