-- +goose Up
ALTER TABLE users ADD COLUMN avatar_file text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN avatar_file;
