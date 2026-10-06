package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MonitorState holds durable scheduling and the last observation; it is not an API model.
type MonitorState struct {
	MonitorID      uuid.UUID `gorm:"primaryKey"`
	Version        int64
	NextCheckAt    time.Time
	JobID          *uuid.UUID
	LeaseUntil     *time.Time
	LastStartedAt  *time.Time
	LastFinishedAt *time.Time
	LastSuccess    *bool
	HTTPStatus     *int
	ErrorKind      string
}

// MonitorMinute counts observations by their UTC start minute.
type MonitorMinute struct {
	MonitorID   uuid.UUID `gorm:"primaryKey"`
	MinuteStart time.Time `gorm:"primaryKey"`
	Successes   int64
	Failures    int64
}

// CheckJob is a leased snapshot of a monitor's configuration.
type CheckJob struct {
	MonitorID       uuid.UUID
	URL             string
	IntervalSeconds int
	Version         int64
	JobID           uuid.UUID
	ScheduledAt     time.Time
}

// CheckResult is an observation; infrastructure cancellation must not be submitted here.
type CheckResult struct {
	StartedAt  time.Time
	FinishedAt time.Time
	HTTPStatus *int
	ErrorKind  string
	Success    bool
}

// ClaimChecks atomically leases at most limit due checks for 30 seconds under ctx.
// HTTP I/O must happen after a successful return; database errors discard the batch,
// including scanned jobs whose lease transaction failed to commit.
func (s *Store) ClaimChecks(ctx context.Context, limit int) ([]CheckJob, error) {
	jobs := make([]CheckJob, 0)
	if limit <= 0 {
		return jobs, nil
	}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Raw(`WITH due AS (
    SELECT monitor_id FROM monitor_states
    WHERE next_check_at <= now() AND (lease_until IS NULL OR lease_until <= now())
    ORDER BY next_check_at, monitor_id LIMIT ? FOR UPDATE SKIP LOCKED
   ), claimed AS (
    UPDATE monitor_states s SET job_id = gen_random_uuid(), lease_until = now() + interval '30 seconds'
    FROM due WHERE s.monitor_id = due.monitor_id
    RETURNING s.monitor_id, s.version, s.job_id, s.next_check_at
   )
   SELECT c.monitor_id, m.url, m.interval_seconds, c.version, c.job_id, c.next_check_at AS scheduled_at
   FROM claimed c JOIN monitors m ON m.id = c.monitor_id`, limit).Scan(&jobs).Error
	})
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

// NextCheck returns the first scheduled slot strictly after finished, skipping missed slots.
func NextCheck(scheduled, finished time.Time, intervalSeconds int) time.Time {
	interval := time.Duration(intervalSeconds) * time.Second
	if finished.Before(scheduled) {
		return scheduled.Add(interval)
	}
	// Preserve the original cadence without replaying intervals missed by slow requests or downtime.
	return scheduled.Add((finished.Sub(scheduled)/interval + 1) * interval)
}

// FinishCheck atomically accounts for a still-owned observation and advances its schedule.
// Stale/deleted/already-finished jobs return false without changing counters. ctx bounds DB work.
func (s *Store) FinishCheck(ctx context.Context, job CheckJob, result CheckResult) (bool, error) {
	saved := false
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Match the edit/delete lock order before taking the state lock or inserting a foreign key.
		var m Monitor
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&m, "id = ?", job.MonitorID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		var state MonitorState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&state, "monitor_id = ?", job.MonitorID).Error; err != nil {
			return err
		}
		if state.JobID == nil || *state.JobID != job.JobID {
			return nil
		}
		if state.Version != job.Version {
			return tx.Model(&state).Updates(map[string]any{"job_id": nil, "lease_until": nil}).Error
		}
		var valid bool
		if err := tx.Raw("SELECT ?::timestamptz > clock_timestamp()", state.LeaseUntil).Scan(&valid).Error; err != nil {
			return err
		}
		if !valid {
			return nil
		}
		success, failure := int64(0), int64(1)
		if result.Success {
			success, failure = 1, 0
		}
		if err := tx.Exec(`INSERT INTO monitor_minutes(monitor_id, minute_start, successes, failures)
    VALUES (?, ?, ?, ?) ON CONFLICT (monitor_id, minute_start) DO UPDATE
    SET successes = monitor_minutes.successes + EXCLUDED.successes,
        failures = monitor_minutes.failures + EXCLUDED.failures`,
			job.MonitorID, result.StartedAt.UTC().Truncate(time.Minute), success, failure).Error; err != nil {
			return err
		}
		err = tx.Model(&state).Updates(map[string]any{
			"job_id": nil, "lease_until": nil,
			"next_check_at":   NextCheck(job.ScheduledAt, result.FinishedAt, job.IntervalSeconds),
			"last_started_at": result.StartedAt, "last_finished_at": result.FinishedAt,
			"last_success": result.Success, "http_status": result.HTTPStatus, "error_kind": result.ErrorKind,
		}).Error
		saved = err == nil
		return err
	})
	return saved && err == nil, err
}

// ReleaseCheck releases only the caller's lease without recording a failed site check.
func (s *Store) ReleaseCheck(ctx context.Context, job CheckJob) error {
	return s.DB.WithContext(ctx).Model(&MonitorState{}).
		Where("monitor_id = ? AND job_id = ?", job.MonitorID, job.JobID).
		Updates(map[string]any{"job_id": nil, "lease_until": nil}).Error
}

// PruneCheckHistory deletes one bounded batch; callers repeat while count is nonzero.
// ctx controls deletion and database failures are returned.
func (s *Store) PruneCheckHistory(ctx context.Context) (int64, error) {
	result := s.DB.WithContext(ctx).Exec(`DELETE FROM monitor_minutes WHERE (monitor_id, minute_start) IN (
  SELECT monitor_id, minute_start FROM monitor_minutes WHERE minute_start < now() - interval '30 days'
  ORDER BY minute_start LIMIT 5000 FOR UPDATE SKIP LOCKED
 )`)
	return result.RowsAffected, result.Error
}

// MonitorStatus is an owner-visible snapshot, independent of favicon discovery.
type MonitorStatus struct {
	ID             uuid.UUID  `json:"id" swaggertype:"string" format:"uuid"`
	Version        int64      `json:"version"`
	Status         string     `json:"status" enums:"pending,up,down,stale"`
	LastStartedAt  *time.Time `json:"last_started_at" format:"date-time" extensions:"x-nullable"`
	LastFinishedAt *time.Time `json:"last_finished_at" format:"date-time" extensions:"x-nullable"`
	LastSuccess    *bool      `json:"last_success" extensions:"x-nullable"`
	HTTPStatus     *int       `json:"http_status" extensions:"x-nullable"`
	ErrorKind      string     `json:"error_kind"`
}

// MonitorStatuses returns every requested owned ID, or ErrRecordNotFound for any missing/foreign ID.
// The single query uses database time to derive staleness; ctx bounds database work.
func (s *Store) MonitorStatuses(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) ([]MonitorStatus, error) {
	statuses := make([]MonitorStatus, 0, len(ids))
	if len(ids) == 0 {
		return statuses, nil
	}
	err := s.DB.WithContext(ctx).Raw(`SELECT m.id, s.version, s.last_started_at, s.last_finished_at,
  s.last_success, s.http_status, s.error_kind,
  CASE WHEN s.last_started_at IS NULL THEN 'pending'
       WHEN now() > s.last_started_at + (m.interval_seconds::bigint + 15) * interval '1 second' THEN 'stale'
       WHEN s.last_success THEN 'up' ELSE 'down' END AS status
  FROM monitors m JOIN monitor_states s ON s.monitor_id = m.id
  WHERE m.user_id = ? AND m.id IN ?`, userID, ids).Scan(&statuses).Error
	if err != nil {
		return nil, err
	}
	if len(statuses) != len(ids) {
		return nil, gorm.ErrRecordNotFound
	}
	return statuses, nil
}

// HistoryBucket includes null availability when no observations exist.
type HistoryBucket struct {
	Start        time.Time `json:"start" format:"date-time"`
	End          time.Time `json:"end" format:"date-time"`
	Successes    int64     `json:"successes"`
	Failures     int64     `json:"failures"`
	Availability *float64  `json:"availability" extensions:"x-nullable"`
}

// MonitorHistory contains aligned UTC buckets and sample-weighted totals.
type MonitorHistory struct {
	From         time.Time       `json:"from" format:"date-time"`
	To           time.Time       `json:"to" format:"date-time"`
	StepSeconds  int             `json:"step_seconds"`
	Version      int64           `json:"version"`
	Successes    int64           `json:"successes"`
	Failures     int64           `json:"failures"`
	Availability *float64        `json:"availability" extensions:"x-nullable"`
	Buckets      []HistoryBucket `json:"buckets"`
}

// HistoryWindow validates a named period and returns its duration and graph step.
func HistoryWindow(period string) (time.Duration, time.Duration, error) {
	switch period {
	case "1h":
		return time.Hour, time.Minute, nil
	case "24h":
		return 24 * time.Hour, 5 * time.Minute, nil
	case "7d":
		return 7 * 24 * time.Hour, 30 * time.Minute, nil
	case "30d":
		return 30 * 24 * time.Hour, 2 * time.Hour, nil
	default:
		return 0, 0, fmt.Errorf("invalid history period")
	}
}

func availability(successes, failures int64) *float64 {
	if successes+failures == 0 {
		return nil
	}
	value := float64(successes) * 100 / float64(successes+failures)
	return &value
}

// CheckHistory returns owned history or ErrRecordNotFound; ctx controls a consistent read snapshot.
// Boundaries round inward to complete minute starts so expired observations never leak into totals.
func (s *Store) CheckHistory(ctx context.Context, userID, id uuid.UUID, period string) (MonitorHistory, error) {
	duration, step, err := HistoryWindow(period)
	if err != nil {
		return MonitorHistory{}, err
	}
	history := MonitorHistory{StepSeconds: int(step.Seconds()), Buckets: make([]HistoryBucket, 0)}
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state struct {
			Version int64
			Now     time.Time
		}
		result := tx.Raw(`SELECT s.version, now() AS now FROM monitors m JOIN monitor_states s ON m.id = s.monitor_id
   WHERE m.id = ? AND m.user_id = ?`, id, userID).Scan(&state)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		history.Version, history.To = state.Version, state.Now.UTC()
		cutoff := history.To.Add(-duration)
		history.From = cutoff.Truncate(time.Minute)
		if history.From.Before(cutoff) {
			history.From = history.From.Add(time.Minute)
		}
		rows := make([]HistoryBucket, 0)
		if err := tx.Raw(`SELECT date_bin(? * interval '1 second', minute_start, ?::timestamptz) AS start,
    sum(successes) AS successes, sum(failures) AS failures FROM monitor_minutes
    WHERE monitor_id = ? AND minute_start >= ? AND minute_start <= ? GROUP BY 1 ORDER BY 1`,
			int(step.Seconds()), history.From, id, history.From, history.To).Scan(&rows).Error; err != nil {
			return err
		}
		index := 0
		for start := history.From; start.Before(history.To); start = start.Add(step) {
			bucket := HistoryBucket{Start: start, End: start.Add(step)}
			if bucket.End.After(history.To) {
				bucket.End = history.To
			}
			if index < len(rows) && rows[index].Start.Equal(start) {
				bucket.Successes, bucket.Failures = rows[index].Successes, rows[index].Failures
				index++
			}
			bucket.Availability = availability(bucket.Successes, bucket.Failures)
			history.Successes += bucket.Successes
			history.Failures += bucket.Failures
			history.Buckets = append(history.Buckets, bucket)
		}
		history.Availability = availability(history.Successes, history.Failures)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return history, err
}
