-- +goose Up
ALTER TABLE container_registries ADD COLUMN consumer_aws_access_key_id TEXT NOT NULL DEFAULT '';
ALTER TABLE container_registries ADD COLUMN consumer_aws_secret_access_key TEXT NOT NULL DEFAULT '';
ALTER TABLE container_registries ADD COLUMN consumer_aws_region TEXT NOT NULL DEFAULT '';

CREATE TABLE container_registry_environment_statuses (
    id TEXT PRIMARY KEY,
    registry_id TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    desired_version TEXT NOT NULL DEFAULT '',
    applied_version TEXT NOT NULL DEFAULT '',
    sync_status TEXT NOT NULL DEFAULT 'pending',
    last_sync_at DATETIME,
    last_sync_error TEXT NOT NULL DEFAULT '',
    pull_test_status TEXT NOT NULL DEFAULT 'unknown',
    last_pull_test_at DATETIME,
    last_pull_test_error TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME,
    CONSTRAINT fk_registry_environment_status_registry FOREIGN KEY (registry_id) REFERENCES container_registries(id) ON DELETE CASCADE,
    CONSTRAINT fk_registry_environment_status_environment FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE,
    CONSTRAINT idx_registry_environment_status UNIQUE (registry_id, environment_id)
);

-- +goose Down
DROP TABLE IF EXISTS container_registry_environment_statuses;
ALTER TABLE container_registries DROP COLUMN consumer_aws_region;
ALTER TABLE container_registries DROP COLUMN consumer_aws_secret_access_key;
ALTER TABLE container_registries DROP COLUMN consumer_aws_access_key_id;
