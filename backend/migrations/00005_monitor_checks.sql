-- +goose Up
UPDATE monitors SET interval_seconds = 5 WHERE interval_seconds < 5;
ALTER TABLE monitors DROP CONSTRAINT monitors_interval_seconds_check;
ALTER TABLE monitors ADD CONSTRAINT monitors_interval_seconds_check CHECK (interval_seconds >= 5);

CREATE TABLE monitor_states (
 monitor_id uuid PRIMARY KEY REFERENCES monitors(id) ON DELETE CASCADE,
 version bigint NOT NULL DEFAULT 1,
 next_check_at timestamptz NOT NULL DEFAULT now(),
 job_id uuid,
 lease_until timestamptz,
 last_started_at timestamptz,
 last_finished_at timestamptz,
 last_success boolean,
 http_status integer,
 error_kind text NOT NULL DEFAULT ''
);
CREATE INDEX monitor_states_due_idx ON monitor_states(next_check_at);
INSERT INTO monitor_states(monitor_id) SELECT id FROM monitors;

CREATE TABLE monitor_minutes (
 monitor_id uuid NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
 minute_start timestamptz NOT NULL,
 successes bigint NOT NULL DEFAULT 0 CHECK (successes >= 0),
 failures bigint NOT NULL DEFAULT 0 CHECK (failures >= 0),
 PRIMARY KEY (monitor_id, minute_start)
);
CREATE INDEX monitor_minutes_retention_idx ON monitor_minutes(minute_start);

-- +goose Down
DROP TABLE monitor_minutes;
DROP TABLE monitor_states;
ALTER TABLE monitors DROP CONSTRAINT monitors_interval_seconds_check;
ALTER TABLE monitors ADD CONSTRAINT monitors_interval_seconds_check CHECK (interval_seconds > 0);
