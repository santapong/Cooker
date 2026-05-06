-- Optimistic-concurrency version columns.
-- Each Update() does WHERE id=$1 AND version=$N; on RowsAffected=0
-- the store returns ErrConflict so the handler can map to HTTP 409.
-- DEFAULT 1 means existing rows are considered "version 1" and the
-- next update from a client that didn't see this column will succeed
-- on first attempt and fail on the second concurrent edit.

ALTER TABLE pipelines    ADD COLUMN IF NOT EXISTS version INT NOT NULL DEFAULT 1;
ALTER TABLE environments ADD COLUMN IF NOT EXISTS version INT NOT NULL DEFAULT 1;
ALTER TABLE apps         ADD COLUMN IF NOT EXISTS version INT NOT NULL DEFAULT 1;
ALTER TABLE hosts        ADD COLUMN IF NOT EXISTS version INT NOT NULL DEFAULT 1;
