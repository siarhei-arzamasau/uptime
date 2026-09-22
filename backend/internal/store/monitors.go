package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Monitor struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID          uuid.UUID `gorm:"type:uuid" json:"-"`
	URL             string    `json:"url"`
	IntervalSeconds int       `json:"interval_seconds"`
	CreatedAt       time.Time `json:"created_at"`
}

func (s *Store) CreateMonitor(ctx context.Context, monitor *Monitor) error {
	return s.DB.WithContext(ctx).Create(monitor).Error
}

const MonitorPageSize = 50

type MonitorCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

func (s *Store) MonitorsByUser(ctx context.Context, userID uuid.UUID, after *MonitorCursor) ([]Monitor, error) {
	monitors := make([]Monitor, 0)
	query := s.DB.WithContext(ctx).Where("user_id = ?", userID)
	if after != nil {
		query = query.Where("(created_at, id) < (?, ?)", after.CreatedAt, after.ID)
	}
	// Fetch one extra row so callers can determine whether another page exists.
	err := query.Order("created_at DESC, id DESC").Limit(MonitorPageSize + 1).Find(&monitors).Error
	return monitors, err
}
