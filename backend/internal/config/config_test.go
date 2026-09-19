package config

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("JWT_SECRET", strings.Repeat("s", 32))
	t.Setenv("ALLOWED_ORIGIN", "http://localhost:3000")
	t.Setenv("COOKIE_SECURE", "true")
	if c, err := Load(); err != nil || !c.CookieSecure {
		t.Fatal("valid config", err)
	}
	for _, tc := range []struct{ key, value string }{{"JWT_SECRET", "short"}, {"JWT_SECRET", "replace-with-at-least-32-random-bytes"}, {"DATABASE_URL", ""}, {"COOKIE_SECURE", "bad"}, {"ALLOWED_ORIGIN", "*"}, {"ALLOWED_ORIGIN", "https://example.com/path"}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}
