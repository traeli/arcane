-- +goose Up
ALTER TABLE image_builds ADD COLUMN source_update_mode TEXT NOT NULL DEFAULT '';
ALTER TABLE image_builds ADD COLUMN source_revision TEXT;
ALTER TABLE image_builds ADD COLUMN source_branch TEXT;
ALTER TABLE image_builds ADD COLUMN source_repository TEXT;

-- +goose Down
ALTER TABLE image_builds DROP COLUMN source_repository;
ALTER TABLE image_builds DROP COLUMN source_branch;
ALTER TABLE image_builds DROP COLUMN source_revision;
ALTER TABLE image_builds DROP COLUMN source_update_mode;
