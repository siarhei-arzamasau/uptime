package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL, HTTPAddr, JWTSecret, Issuer, Audience, Origin string
	AvatarDir                                                  string
	CookieSecure                                               bool
}

func Load() (Config, error) {
	c := Config{AvatarDir: env("AVATAR_DIR", "var/avatars"), DatabaseURL: os.Getenv("DATABASE_URL"), HTTPAddr: env("HTTP_ADDR", "127.0.0.1:8080"), JWTSecret: os.Getenv("JWT_SECRET"), Issuer: env("JWT_ISSUER", "uptime-api"), Audience: env("JWT_AUDIENCE", "uptime-web"), Origin: env("ALLOWED_ORIGIN", "http://localhost:3000")}
	var err error
	c.CookieSecure, err = strconv.ParseBool(env("COOKIE_SECURE", "true"))
	if err != nil {
		return c, fmt.Errorf("COOKIE_SECURE must be boolean")
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	if len(c.JWTSecret) < 32 || strings.HasPrefix(c.JWTSecret, "replace-") {
		return c, fmt.Errorf("JWT_SECRET must contain at least 32 random bytes and not a placeholder")
	}
	u, err := url.Parse(c.Origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return c, fmt.Errorf("ALLOWED_ORIGIN must be an http(s) origin without path")
	}
	return c, nil
}
func env(key, fallback string) string {
	if s := os.Getenv(key); s != "" {
		return s
	}
	return fallback
}
