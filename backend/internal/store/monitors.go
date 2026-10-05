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
	IntervalSeconds int       `json:"interval_seconds" minimum:"1" maximum:"2147483647"`
	CreatedAt       time.Time `json:"created_at" format:"date-time"`
}

// CreateMonitor inserts an already-validated, owner-assigned monitor under ctx
// and returns a constraint or database error; it does not run monitoring checks.
func (s *Store) CreateMonitor(ctx context.Context, monitor *Monitor) error {
	return s.DB.WithContext(ctx).Create(monitor).Error
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

// UpdateMonitor replaces only configuration and fills monitor with the saved row.
// The owner predicate is part of the update; missing/foreign IDs return gorm.ErrRecordNotFound.
// ctx controls database work; database errors propagate with context.
func (s *Store) UpdateMonitor(ctx context.Context, monitor *Monitor) error {
	result := s.DB.WithContext(ctx).Model(monitor).Clauses(clause.Returning{}).
		Where("id = ? AND user_id = ?", monitor.ID, monitor.UserID).
		Updates(map[string]any{"url": monitor.URL, "interval_seconds": monitor.IntervalSeconds})
	if result.Error != nil {
		return fmt.Errorf("update monitor: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
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
