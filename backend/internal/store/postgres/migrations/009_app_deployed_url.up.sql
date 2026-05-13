-- Cooker app deployed URL (W11 Indie step 6).
-- Stores the public ingress URL surfaced by the deploy target after a
-- successful deploy (e.g. <appId>.fly.dev for Fly.io, Render service URL).
-- Written by AppHealthChecker alongside health_status so the frontend
-- 30s polling pick-up the URL without a separate API call.
-- Targets that don't expose a URL (docker-host, plain kubernetes) leave
-- this column empty.

ALTER TABLE apps
    ADD COLUMN IF NOT EXISTS deployed_url TEXT NOT NULL DEFAULT '';
