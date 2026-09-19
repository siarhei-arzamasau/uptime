package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestCredentials(t *testing.T) {
	for _, tc := range []struct {
		email, password string
		valid           bool
	}{
		{" Alice@Example.com ", strings.Repeat("a", 12), true}, {"a@example.com", strings.Repeat("я", 128), true},
		{"bad", strings.Repeat("a", 12), false}, {"Name <a@example.com>", strings.Repeat("a", 12), false},
		{"a@example.com", strings.Repeat("a", 11), false}, {"a@example.com", strings.Repeat("я", 129), false}, {"a@example.com", "", false},
	} {
		email, err := Credentials(tc.email, tc.password)
		if (err == nil) != tc.valid {
			t.Fatalf("valid=%v error=%v", tc.valid, err)
		}
		if tc.valid && email != strings.ToLower(strings.TrimSpace(tc.email)) {
			t.Fatal("email normalization")
		}
	}
}
func TestPassword(t *testing.T) {
	h, err := HashPassword("correct password")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct password") || CheckPassword(h, "wrong password") || CheckPassword(h, " correct password") {
		t.Fatal("password verification")
	}
	h2, err := HashPassword("correct password")
	if err != nil {
		t.Fatal(err)
	}
	if h == h2 {
		t.Fatal("salt not random")
	}
	for _, bad := range []string{"", "$argon2id$v=19$m=999999999,t=3,p=1$x$x", strings.Replace(h, "v=19", "v=20", 1)} {
		if CheckPassword(bad, "correct password") {
			t.Fatal("invalid hash accepted")
		}
	}
}
func TestRefreshToken(t *testing.T) {
	a, err := NewRefresh()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewRefresh()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || !validRefresh(a) || len(RefreshHash(a)) != 64 || RefreshHash(a) == a {
		t.Fatal("bad refresh generation")
	}
	if validRefresh("") || validRefresh("garbage") {
		t.Fatal("bad refresh accepted")
	}
}
func TestJWT(t *testing.T) {
	secret := strings.Repeat("s", 32)
	tokens := NewTokens(secret, "issuer", "audience")
	id := uuid.New()
	now := time.Now()
	raw, err := tokens.Issue(id, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tokens.Verify(raw)
	if err != nil || got != id {
		t.Fatal("valid JWT rejected", err)
	}
	cases := []string{"signature", "algorithm", "issuer", "audience", "expired", "future", "sub", "iat", "exp", "iss", "aud"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			claims := jwt.MapClaims{"sub": id.String(), "iss": "issuer", "aud": "audience", "iat": now.Unix(), "exp": now.Add(time.Minute).Unix()}
			key := []byte(secret)
			method := jwt.SigningMethodHS256
			switch name {
			case "signature":
				key = []byte(strings.Repeat("z", 32))
			case "algorithm":
				method = jwt.SigningMethodHS384
			case "issuer":
				claims["iss"] = "other"
			case "audience":
				claims["aud"] = "other"
			case "expired":
				claims["exp"] = now.Add(-time.Minute).Unix()
			case "future":
				claims["iat"] = now.Add(time.Hour).Unix()
			default:
				delete(claims, name)
			}
			raw, err := jwt.NewWithClaims(method, claims).SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tokens.Verify(raw); err == nil {
				t.Fatal("invalid JWT accepted")
			}
		})
	}
}
