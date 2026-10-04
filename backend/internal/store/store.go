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
	AvatarFile   string
	Name         string
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

// Store wraps a PostgreSQL handle. Methods accepting ctx use it for database work;
// callers remain responsible for authentication and input validation.
type Store struct{ DB *gorm.DB }

// Open creates a connection pool and returns it after an explicit ctx-bound ping.
// Connection/setup errors are returned; no migrations run. ctx bounds the explicit
// ping, not the initial GORM connection setup.
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

// Close closes the underlying connection pool and returns handle or close errors.
// It has no context deadline and should be called by the pool owner.
func (s *Store) Close() error {
	db, err := s.DB.DB()
	if err != nil {
		return err
	}
	return db.Close()
}

// Transaction runs fn with a transaction-scoped store and commits only on success.
// Callback, database, and context errors are returned; failed work is rolled back.
// ctx controls database operations, not arbitrary work performed by fn.
func (s *Store) Transaction(ctx context.Context, fn func(*Store) error) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(&Store{tx}) })
}

// CreateUser inserts u under ctx and returns a constraint or database error.
func (s *Store) CreateUser(ctx context.Context, u *User) error {
	return s.DB.WithContext(ctx).Create(u).Error
}

// UserByEmail returns the stored user for an already-normalized email under ctx.
// A missing user returns gorm.ErrRecordNotFound; other database errors propagate.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.DB.WithContext(ctx).Where("email = ?", email).Take(&u).Error
	return u, err
}

// UserByID returns the stored user under ctx, or gorm.ErrRecordNotFound if absent.
// Other database errors propagate.
func (s *Store) UserByID(ctx context.Context, id uuid.UUID) (User, error) {
	var u User
	err := s.DB.WithContext(ctx).Where("id = ?", id).Take(&u).Error
	return u, err
}

// CreateSession inserts v under ctx and returns a constraint or database error.
// Use the transaction store when the session and its token must commit together.
func (s *Store) CreateSession(ctx context.Context, v *Session) error {
	return s.DB.WithContext(ctx).Create(v).Error
}

// CreateToken inserts a refresh-token hash under ctx and returns a database error.
// It does not store raw tokens or consume an existing token.
func (s *Store) CreateToken(ctx context.Context, v *RefreshToken) error {
	return s.DB.WithContext(ctx).Create(v).Error
}

// Token looks up a refresh-token hash under ctx, including previously used tokens.
// It returns gorm.ErrRecordNotFound if absent, or another database error.
func (s *Store) Token(ctx context.Context, hash string) (RefreshToken, error) {
	var v RefreshToken
	err := s.DB.WithContext(ctx).Where("hash = ?", hash).Take(&v).Error
	return v, err
}

// LockSession returns a session with a FOR UPDATE row lock under ctx, or a lookup error.
// Call it inside Transaction so the lock spans token checks and writes, not just this query.
func (s *Store) LockSession(ctx context.Context, id uuid.UUID) (Session, error) {
	var v Session
	err := s.DB.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&v).Error
	return v, err
}

// Revoke sets the first revocation time under ctx and returns database errors.
// Already-revoked or missing sessions are a no-op; no tokens are deleted.
func (s *Store) Revoke(ctx context.Context, id uuid.UUID, now time.Time) error {
	return s.DB.WithContext(ctx).Model(&Session{}).Where("id = ? AND revoked_at IS NULL", id).Update("revoked_at", now).Error
}

// UseToken records the consumption time under ctx and returns database errors.
// It does not lock or verify prior consumption; callers must hold the session lock.
func (s *Store) UseToken(ctx context.Context, hash string, now time.Time) error {
	return s.DB.WithContext(ctx).Model(&RefreshToken{}).Where("hash = ?", hash).Update("used_at", now).Error
}

// UpdateUserName returns the user after changing only its name under ctx.
// Avatar and credentials are preserved. Missing users return gorm.ErrRecordNotFound;
// other database errors propagate.
func (s *Store) UpdateUserName(ctx context.Context, id uuid.UUID, name string) (User, error) {
	var u User
	result := s.DB.WithContext(ctx).Model(&u).Clauses(clause.Returning{}).Where("id = ?", id).Update("name", name)
	if result.Error != nil {
		return User{}, result.Error
	}
	if result.RowsAffected == 0 {
		return User{}, gorm.ErrRecordNotFound
	}
	return u, nil
}

// AvatarURL returns the public API path for the saved filename, or an empty string
// when no avatar is saved. It performs no filesystem check.
func (u User) AvatarURL() string {
	if u.AvatarFile == "" {
		return ""
	}
	return "/api/v1/avatars/" + u.AvatarFile
}

// UpdateUserAvatar atomically saves name and filename under ctx, serializing concurrent
// saves with a user row lock. On success it returns the updated user and previous filename
// for cleanup by the caller; this method does not remove files. Missing users return
// gorm.ErrRecordNotFound; discard the returned values on a transaction/database error.
func (s *Store) UpdateUserAvatar(ctx context.Context, id uuid.UUID, name, filename string) (User, string, error) {
	var u User
	var old string
	err := s.Transaction(ctx, func(tx *Store) error {
		if err := tx.DB.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&u).Error; err != nil {
			return err
		}
		old = u.AvatarFile
		if err := tx.DB.WithContext(ctx).Model(&u).Updates(map[string]any{"name": name, "avatar_file": filename}).Error; err != nil {
			return err
		}
		u.Name = name
		u.AvatarFile = filename
		return nil
	})
	return u, old, err
}
