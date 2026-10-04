package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"uptime-app/backend/internal/store"
)

var (
	ErrInvalid      = errors.New("invalid credentials format")
	ErrUnauthorized = errors.New("invalid credentials or token")
	ErrConflict     = errors.New("email already registered")
)

type Result struct {
	User            store.User
	Access, Refresh string
	ExpiresAt       time.Time
}
type Service struct {
	store     *store.Store
	tokens    *Tokens
	dummyHash string
	passwords *passwordGate
}

// NewService creates a service with a shared password-work gate and a dummy hash
// for unknown-account checks. It returns an error if the random salt cannot be generated.
func NewService(s *store.Store, t *Tokens) (*Service, error) {
	dummy, err := HashPassword("dummy-password-for-timing")
	if err != nil {
		return nil, err
	}
	return &Service{store: s, tokens: t, dummyHash: dummy, passwords: newPasswordGate()}, nil
}

// Register atomically creates a user and session and returns their token pair.
// It returns ErrInvalid, ErrBusy, ErrConflict, or an underlying creation error.
// ctx controls admission and database work; an admitted Argon2 hash runs to completion.
// Discard the result if the transaction fails.
func (s *Service) Register(ctx context.Context, email, password string) (Result, error) {
	email, err := Credentials(email, password)
	if err != nil {
		return Result{}, err
	}
	release, err := s.passwords.acquire(ctx)
	if err != nil {
		return Result{}, err
	}
	defer release()
	hash, err := HashPassword(password)
	if err != nil {
		return Result{}, err
	}
	u := store.User{ID: uuid.New(), Email: email, PasswordHash: hash, CreatedAt: time.Now().UTC()}
	var result Result
	err = s.store.Transaction(ctx, func(tx *store.Store) error {
		if err := tx.CreateUser(ctx, &u); err != nil {
			return err
		}
		var err error
		result, err = s.newSession(ctx, tx, u)
		return err
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		err = ErrConflict
	}
	return result, err
}

// Login returns a new session and token pair for valid credentials.
// Invalid or unknown credentials return ErrUnauthorized; capacity exhaustion returns
// ErrBusy. ctx controls admission and database work, but cannot interrupt Argon2.
// Discard the result on a database or session-creation error.
func (s *Service) Login(ctx context.Context, email, password string) (Result, error) {
	email, err := Credentials(email, password)
	if err != nil {
		return Result{}, ErrUnauthorized
	}
	release, err := s.passwords.acquire(ctx)
	if err != nil {
		return Result{}, err
	}
	defer release()
	u, err := s.store.UserByEmail(ctx, email)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Spend the same password-check work for unknown accounts to reduce timing disclosure.
		CheckPassword(s.dummyHash, password)
		return Result{}, ErrUnauthorized
	}
	if err != nil {
		return Result{}, err
	}
	if !CheckPassword(u.PasswordHash, password) {
		return Result{}, ErrUnauthorized
	}
	var result Result
	err = s.store.Transaction(ctx, func(tx *store.Store) error { var err error; result, err = s.newSession(ctx, tx, u); return err })
	return result, err
}
func (s *Service) newSession(ctx context.Context, tx *store.Store, u store.User) (Result, error) {
	now := time.Now().UTC()
	session := store.Session{ID: uuid.New(), UserID: u.ID, ExpiresAt: now.Add(SessionTTL)}
	if err := tx.CreateSession(ctx, &session); err != nil {
		return Result{}, err
	}
	return s.issue(ctx, tx, session, u, now)
}
func (s *Service) issue(ctx context.Context, tx *store.Store, session store.Session, u store.User, now time.Time) (Result, error) {
	refresh, err := NewRefresh()
	if err != nil {
		return Result{}, err
	}
	access, err := s.tokens.Issue(session.UserID, now)
	if err != nil {
		return Result{}, err
	}
	token := store.RefreshToken{Hash: RefreshHash(refresh), SessionID: session.ID, CreatedAt: now}
	if err = tx.CreateToken(ctx, &token); err != nil {
		return Result{}, err
	}
	return Result{User: u, Access: access, Refresh: refresh, ExpiresAt: session.ExpiresAt}, nil
}

// Refresh atomically consumes a refresh token and returns its replacement pair
// with the original session expiry; the result does not include a user profile.
// Replay revokes that session before ErrUnauthorized is returned. Invalid, missing,
// expired, or revoked tokens also return ErrUnauthorized. ctx controls database work;
// transaction failures return an error and the result must be discarded.
func (s *Service) Refresh(ctx context.Context, raw string) (Result, error) {
	if !validRefresh(raw) {
		return Result{}, ErrUnauthorized
	}
	var result Result
	denied := false
	err := s.store.Transaction(ctx, func(tx *store.Store) error {
		token, err := tx.Token(ctx, RefreshHash(raw))
		if err != nil {
			return err
		}
		session, err := tx.LockSession(ctx, token.SessionID)
		if err != nil {
			return err
		}
		// Read again after acquiring the session lock: another request may have consumed it.
		token, err = tx.Token(ctx, token.Hash)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if session.RevokedAt != nil || !now.Before(session.ExpiresAt) {
			denied = true
			return nil
		}
		if token.UsedAt != nil {
			denied = true
			return tx.Revoke(ctx, session.ID, now)
		}
		if err = tx.UseToken(ctx, token.Hash, now); err != nil {
			return err
		}
		result, err = s.issue(ctx, tx, session, store.User{}, now)
		return err
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Result{}, ErrUnauthorized
	}
	if err != nil {
		return Result{}, err
	}
	// Returning ErrUnauthorized inside the transaction would roll back replay revocation.
	if denied {
		return Result{}, ErrUnauthorized
	}
	return result, nil
}

// Logout revokes the session associated with raw; invalid or missing tokens are a no-op.
// ctx controls database work, and lookup or transaction failures are returned.
func (s *Service) Logout(ctx context.Context, raw string) error {
	if !validRefresh(raw) {
		return nil
	}
	err := s.store.Transaction(ctx, func(tx *store.Store) error {
		token, err := tx.Token(ctx, RefreshHash(raw))
		if err != nil {
			return err
		}
		session, err := tx.LockSession(ctx, token.SessionID)
		if err != nil {
			return err
		}
		return tx.Revoke(ctx, session.ID, time.Now().UTC())
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

// Me returns the stored user or ErrUnauthorized if the user no longer exists.
// ctx controls the lookup; other database errors are returned unchanged.
func (s *Service) Me(ctx context.Context, id uuid.UUID) (store.User, error) {
	u, err := s.store.UserByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrUnauthorized
	}
	return u, err
}
