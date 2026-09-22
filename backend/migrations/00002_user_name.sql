-- +goose Up
ALTER TABLE users ADD COLUMN name varchar(100) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN name;
