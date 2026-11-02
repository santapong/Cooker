-- Optional safe lineage. Legacy runs remain unknown, never backfilled from the
-- current pipeline/ref. Existing explicit queries remain compatible.
ALTER TABLE pipeline_runs ADD COLUMN source JSONB;
ALTER TABLE pipeline_runs ADD CONSTRAINT pipeline_runs_source_object
 CHECK(source IS NULL OR jsonb_typeof(source)='object');
