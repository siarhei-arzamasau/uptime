package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"
)

const AccessTTL = 15 * time.Minute
const SessionTTL = 30 * 24 * time.Hour

// Credentials returns a trimmed, lowercase email when it and the UTF-8 password
// satisfy the credential policy; invalid input returns an empty email and ErrInvalid.
func Credentials(email, password string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > 254 || !strings.Contains(email, "@") || !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || utf8.RuneCountInString(password) > 128 {
		return "", ErrInvalid
	}
	return email, nil
}

// HashPassword returns a salted Argon2id encoding with the fixed parameters accepted
// by CheckPassword, or an error if salt generation fails. It does not validate input
// or support cancellation; request callers must limit concurrent work.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	// Argon2 memory is measured in KiB; 64 MiB matches the encoded parameters below.
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

// CheckPassword reports whether password matches a supported Argon2id encoding.
// Malformed encodings, unsupported parameters, and mismatches return false.
// The hash calculation cannot be cancelled; request callers must bound concurrency.
func CheckPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	// Accept only the fixed cost so stored hashes cannot bypass password-work limits.
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=1" {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

// NewRefresh returns a canonical base64url token containing 32 random bytes,
// or an error if the cryptographic random source fails.
func NewRefresh() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// RefreshHash returns the SHA-256 hex lookup key so persistence never needs the raw token.
func RefreshHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
func validRefresh(raw string) bool {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	return err == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == raw
}

type Tokens struct {
	secret           []byte
	issuer, audience string
}

// NewTokens creates an HS256 issuer/verifier with the supplied key and claim scope.
// It does not validate the key; configuration validation belongs to the caller.
func NewTokens(secret, issuer, audience string) *Tokens {
	return &Tokens{[]byte(secret), issuer, audience}
}

// Issue returns an HS256 access token for id, valid for AccessTTL from now,
// or a signing error.
func (t *Tokens) Issue(id uuid.UUID, now time.Time) (string, error) {
	claims := jwt.RegisteredClaims{Subject: id.String(), Issuer: t.issuer, Audience: jwt.ClaimStrings{t.audience}, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(AccessTTL))}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
}

// Verify returns the subject UUID after signature, issuer, audience, and time checks.
// Invalid tokens or subjects return uuid.Nil and ErrUnauthorized. Verification does
// not check database session revocation; access tokens remain valid until expiry.
func (t *Tokens) Verify(raw string) (uuid.UUID, error) {
	claims := new(jwt.RegisteredClaims)
	token, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return t.secret, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(t.issuer), jwt.WithAudience(t.audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !token.Valid || claims.IssuedAt == nil || claims.ExpiresAt == nil || !claims.ExpiresAt.After(claims.IssuedAt.Time) {
		return uuid.Nil, ErrUnauthorized
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, ErrUnauthorized
	}
	return id, nil
}
