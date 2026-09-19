package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type User struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey"`
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}
type Session struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID    uuid.UUID `gorm:"type:uuid"`
	ExpiresAt time.Time
	RevokedAt *time.Time
}
type RefreshToken struct {
	Hash      string    `gorm:"primaryKey"`
	SessionID uuid.UUID `gorm:"type:uuid"`
	CreatedAt time.Time
	UsedAt    *time.Time
}

type Store struct{ DB *gorm.DB }

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	pool, err := db.DB()
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(5)
	pool.SetConnMaxLifetime(30 * time.Minute)
	if err = pool.PingContext(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{db}, nil
}
func (s *Store) Close() error {
	db, err := s.DB.DB()
	if err != nil {
		return err
	}
	return db.Close()
}
func (s *Store) Transaction(ctx context.Context, fn func(*Store) error) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(&Store{tx}) })
}
func (s *Store) CreateUser(ctx context.Context, u *User) error {
	return s.DB.WithContext(ctx).Create(u).Error
}
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.DB.WithContext(ctx).Where("email = ?", email).Take(&u).Error
	return u, err
}
func (s *Store) UserByID(ctx context.Context, id uuid.UUID) (User, error) {
	var u User
	err := s.DB.WithContext(ctx).Where("id = ?", id).Take(&u).Error
	return u, err
}
func (s *Store) CreateSession(ctx context.Context, v *Session) error {
	return s.DB.WithContext(ctx).Create(v).Error
}
func (s *Store) CreateToken(ctx context.Context, v *RefreshToken) error {
	return s.DB.WithContext(ctx).Create(v).Error
}
func (s *Store) Token(ctx context.Context, hash string) (RefreshToken, error) {
	var v RefreshToken
	err := s.DB.WithContext(ctx).Where("hash = ?", hash).Take(&v).Error
	return v, err
}
func (s *Store) LockSession(ctx context.Context, id uuid.UUID) (Session, error) {
	var v Session
	err := s.DB.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&v).Error
	return v, err
}
func (s *Store) Revoke(ctx context.Context, id uuid.UUID, now time.Time) error {
	return s.DB.WithContext(ctx).Model(&Session{}).Where("id = ? AND revoked_at IS NULL", id).Update("revoked_at", now).Error
}
func (s *Store) UseToken(ctx context.Context, hash string, now time.Time) error {
	return s.DB.WithContext(ctx).Model(&RefreshToken{}).Where("hash = ?", hash).Update("used_at", now).Error
}
