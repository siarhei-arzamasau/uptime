package store

import (
	"context"
	"time"

	"github.com/google/uuid"
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
