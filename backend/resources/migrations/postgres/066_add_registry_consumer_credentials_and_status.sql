-- +goose Up
ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS consumer_aws_access_key_id TEXT NOT NULL DEFAULT '';
ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS consumer_aws_secret_access_key TEXT NOT NULL DEFAULT '';
ALTER TABLE container_registries ADD COLUMN IF NOT EXISTS consumer_aws_region TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS container_registry_environment_statuses (
    id TEXT PRIMARY KEY,
    registry_id TEXT NOT NULL REFERENCES container_registries(id) ON DELETE CASCADE,
    environment_id TEXT NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    desired_version TEXT NOT NULL DEFAULT '',
    applied_version TEXT NOT NULL DEFAULT '',
    sync_status TEXT NOT NULL DEFAULT 'pending',
    last_sync_at TIMESTAMPTZ,
    last_sync_error TEXT NOT NULL DEFAULT '',
    pull_test_status TEXT NOT NULL DEFAULT 'unknown',
    last_pull_test_at TIMESTAMPTZ,
    last_pull_test_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ,
    CONSTRAINT idx_registry_environment_status UNIQUE (registry_id, environment_id)
);

-- +goose Down
DROP TABLE IF EXISTS container_registry_environment_statuses;
ALTER TABLE container_registries DROP COLUMN IF EXISTS consumer_aws_region;
ALTER TABLE container_registries DROP COLUMN IF EXISTS consumer_aws_secret_access_key;
ALTER TABLE container_registries DROP COLUMN IF EXISTS consumer_aws_access_key_id;
