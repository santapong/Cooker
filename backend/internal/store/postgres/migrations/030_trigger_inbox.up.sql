CREATE TABLE trigger_rules (
 id TEXT PRIMARY KEY,
 version INTEGER NOT NULL CHECK(version > 0),
 connection_id TEXT NOT NULL,
 body JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX trigger_rules_connection ON trigger_rules(connection_id,id);
CREATE TABLE webhook_deliveries (
 id TEXT PRIMARY KEY,
 provider TEXT NOT NULL,
 connection_id TEXT NOT NULL,
 delivery_id TEXT NOT NULL,
 payload_hash TEXT NOT NULL,
 event JSONB NOT NULL,
 decisions JSONB NOT NULL DEFAULT '[]',
 archived BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(provider,connection_id,delivery_id)
);
CREATE INDEX webhook_deliveries_created ON webhook_deliveries(created_at);
CREATE TABLE trigger_intents (
 id TEXT PRIMARY KEY,
 version INTEGER NOT NULL DEFAULT 1 CHECK(version > 0),
 logical_key TEXT NOT NULL UNIQUE,
 delivery_id TEXT NOT NULL REFERENCES webhook_deliveries(id) ON DELETE CASCADE,
 rule_id TEXT NOT NULL REFERENCES trigger_rules(id),
 status TEXT NOT NULL CHECK(status IN ('pending','awaiting_review','queued','superseded','failed_preparation')),
 reason TEXT NOT NULL,
 spec JSONB NOT NULL,
 receipt JSONB,
 archived BOOLEAN NOT NULL DEFAULT FALSE,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX trigger_intents_pending ON trigger_intents(created_at,id) WHERE status='pending';
CREATE TABLE trigger_tag_observations (
 provider TEXT NOT NULL,
 host TEXT NOT NULL,
 connection_id TEXT NOT NULL,
 repository_id TEXT NOT NULL,
 full_ref TEXT NOT NULL,
 object_sha TEXT NOT NULL,
 commit_sha TEXT NOT NULL,
 observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY(provider,host,connection_id,repository_id,full_ref)
);
