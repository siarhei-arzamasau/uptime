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

func (s *Store) MonitorsByUser(ctx context.Context, userID uuid.UUID) ([]Monitor, error) {
	monitors := make([]Monitor, 0)
	err := s.DB.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC, id DESC").Find(&monitors).Error
	return monitors, err
}
