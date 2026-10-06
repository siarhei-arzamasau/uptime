package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Monitor struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey" json:"id" swaggertype:"string" format:"uuid"`
	UserID          uuid.UUID `gorm:"type:uuid" json:"-"`
	URL             string    `json:"url"`
	IntervalSeconds int       `json:"interval_seconds" minimum:"5" maximum:"2147483647"`
	CreatedAt       time.Time `json:"created_at" format:"date-time"`
}

// CreateMonitor inserts an already-validated, owner-assigned monitor under ctx
// and its immediately due schedule atomically; constraint/database errors roll both back.
func (s *Store) CreateMonitor(ctx context.Context, monitor *Monitor) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(monitor).Error; err != nil {
			return err
		}
		return tx.Exec("INSERT INTO monitor_states(monitor_id) VALUES (?)", monitor.ID).Error
	})
}

const MonitorPageSize = 50

type MonitorCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// MonitorsByUser returns at most MonitorPageSize+1 rows for userID, newest first.
// after is an exclusive (created_at, id) cursor; the extra row signals another page.
// ctx controls the query; database errors propagate and an empty page is non-nil.
func (s *Store) MonitorsByUser(ctx context.Context, userID uuid.UUID, after *MonitorCursor) ([]Monitor, error) {
	monitors := make([]Monitor, 0)
	query := s.DB.WithContext(ctx).Where("user_id = ?", userID)
	if after != nil {
		// The UUID tie-breaker keeps equal timestamps stable when new rows are inserted.
		query = query.Where("(created_at, id) < (?, ?)", after.CreatedAt, after.ID)
	}
	// Fetch one extra row so callers can determine whether another page exists.
	err := query.Order("created_at DESC, id DESC").Limit(MonitorPageSize + 1).Find(&monitors).Error
	return monitors, err
}

// UpdateMonitor saves configuration, schedules a check, and clears history only when the URL changes.
// Ownership is checked under the row lock; missing/foreign IDs return gorm.ErrRecordNotFound.
// ctx controls database work; database errors propagate with context.
func (s *Store) UpdateMonitor(ctx context.Context, monitor *Monitor) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var previous Monitor
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ?", monitor.ID, monitor.UserID).First(&previous).Error; err != nil {
			return fmt.Errorf("lock monitor: %w", err)
		}
		if err := tx.Model(monitor).Clauses(clause.Returning{}).
			Updates(map[string]any{"url": monitor.URL, "interval_seconds": monitor.IntervalSeconds}).Error; err != nil {
			return fmt.Errorf("update monitor: %w", err)
		}
		changes := map[string]any{"version": gorm.Expr("version + 1"), "next_check_at": gorm.Expr("now()")}
		if previous.URL != monitor.URL {
			changes["last_started_at"], changes["last_finished_at"], changes["last_success"] = nil, nil, nil
			changes["http_status"], changes["error_kind"] = nil, ""
			if err := tx.Where("monitor_id = ?", monitor.ID).Delete(&MonitorMinute{}).Error; err != nil {
				return err
			}
		}
		// Keep an outstanding lease until the old request exits or expires.
		return tx.Model(&MonitorState{}).Where("monitor_id = ?", monitor.ID).Updates(changes).Error
	})
}

// DeleteMonitor permanently removes the owner's monitor under ctx.
// Missing/foreign IDs return gorm.ErrRecordNotFound; database errors propagate with context.
func (s *Store) DeleteMonitor(ctx context.Context, userID, id uuid.UUID) error {
	result := s.DB.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&Monitor{})
	if result.Error != nil {
		return fmt.Errorf("delete monitor: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
