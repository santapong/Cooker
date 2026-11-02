-- Prospective safe audit only. Do not backfill lost/compacted history or infer
-- historical App targets from a rule's current configuration.
CREATE TABLE trigger_audit_checkpoint (
 id TEXT PRIMARY KEY CHECK (id='v1'),
 started_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO trigger_audit_checkpoint(id) VALUES ('v1');

CREATE TABLE trigger_delivery_decisions (
 delivery_id TEXT NOT NULL REFERENCES webhook_deliveries(id) ON DELETE CASCADE,
 decision_index INTEGER NOT NULL CHECK (decision_index>0),
 rule_id TEXT NOT NULL DEFAULT '',
 rule_version INTEGER NOT NULL DEFAULT 0 CHECK(rule_version>=0),
 app_id TEXT NOT NULL DEFAULT '',
 environment_id TEXT NOT NULL DEFAULT '',
 target_kind TEXT NOT NULL DEFAULT '' CHECK(target_kind IN ('','app')),
 target_id TEXT NOT NULL DEFAULT '',
 intent_id TEXT NOT NULL DEFAULT '',
 outcome TEXT NOT NULL CHECK(outcome IN ('accepted','ignored','duplicate','stale','rejected','review_required','failed','unknown')),
 reason_code TEXT NOT NULL CHECK(reason_code IN ('accepted','ignored','duplicate','stale','rejected','review_required','failed','unknown','repository_mismatch','rule_disabled','ref_kind_mismatch','ref_excluded','ref_mismatch','no_rules','unsupported_event','ref_deleted','tag_moved','source_changed','source_unavailable','rule_changed','configuration_changed','source_outside_approval','actor_not_authorized','deployment_configuration_changed')),
 PRIMARY KEY(delivery_id,decision_index)
);
CREATE INDEX trigger_delivery_decisions_app ON trigger_delivery_decisions(app_id,delivery_id);
CREATE INDEX trigger_delivery_decisions_intent ON trigger_delivery_decisions(intent_id) WHERE intent_id<>'';

CREATE TABLE webhook_attempts (
 id TEXT PRIMARY KEY,
 provider TEXT NOT NULL CHECK(provider IN ('github','gitlab','gitea')),
 connection_id TEXT NOT NULL,
 provider_delivery_id TEXT NOT NULL DEFAULT '',
 delivery_id TEXT REFERENCES webhook_deliveries(id) ON DELETE SET NULL,
 status TEXT NOT NULL DEFAULT 'received' CHECK(status IN ('received','accepted','ignored','duplicate','rejected','failed','uncertain')),
 reason_code TEXT NOT NULL DEFAULT 'processing' CHECK(reason_code IN ('processing','admitted','already_recorded','unsupported_event','invalid_delivery_identity','invalid_json','invalid_event','repository_not_allowed','installation_mismatch','tag_source_unavailable','tag_identity_unverified','tag_revision_unavailable','tag_changed','delivery_identity_conflict','history_unavailable','admission_unconfirmed')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 finished_at TIMESTAMPTZ,
 CHECK ((status='received' AND finished_at IS NULL) OR (status<>'received' AND finished_at IS NOT NULL)),
 CHECK(length(id) BETWEEN 1 AND 128 AND length(connection_id) BETWEEN 1 AND 512 AND length(provider_delivery_id)<=256)
);
CREATE INDEX webhook_attempts_created ON webhook_attempts(created_at DESC,id);
CREATE INDEX webhook_attempts_delivery ON webhook_attempts(delivery_id) WHERE delivery_id IS NOT NULL;
