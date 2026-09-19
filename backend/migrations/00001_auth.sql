-- +goose Up
CREATE TABLE users (
 id uuid PRIMARY KEY,
 email text NOT NULL UNIQUE,
 password_hash text NOT NULL,
 created_at timestamptz NOT NULL,
 CONSTRAINT normalized_email CHECK (email = lower(btrim(email)))
);
CREATE TABLE sessions (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES users(id),
 expires_at timestamptz NOT NULL,
 revoked_at timestamptz
);
CREATE INDEX sessions_user_id_idx ON sessions(user_id);
CREATE TABLE refresh_tokens (
 hash text PRIMARY KEY CHECK (length(hash) = 64),
 session_id uuid NOT NULL REFERENCES sessions(id),
 created_at timestamptz NOT NULL,
 used_at timestamptz
);
CREATE INDEX refresh_tokens_session_id_idx ON refresh_tokens(session_id);
-- +goose Down
DROP TABLE refresh_tokens;
DROP TABLE sessions;
DROP TABLE users;
