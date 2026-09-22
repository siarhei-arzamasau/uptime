package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"uptime-app/backend/internal/auth"
	"uptime-app/backend/internal/config"
	"uptime-app/backend/internal/httpx"
	"uptime-app/backend/internal/monitor"
	"uptime-app/backend/internal/profile"
	"uptime-app/backend/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}
func run() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	s, err := store.Open(connectCtx, c.DatabaseURL)
	cancel()
	if err != nil {
		return fmt.Errorf("database connection failed")
	}
	defer s.Close()
	tokens := auth.NewTokens(c.JWTSecret, c.Issuer, c.Audience)
	service, err := auth.NewService(s, tokens)
	if err != nil {
		return fmt.Errorf("authentication initialization failed")
	}
	mux := http.NewServeMux()
	authController := auth.NewController(service, tokens, c.CookieSecure)
	authController.RegisterRoutes(mux)
	profile.NewController(s, c.AvatarDir).RegisterRoutes(mux, authController.Authenticate)
	monitor.NewController(s).RegisterRoutes(mux, authController.Authenticate)
	server := &http.Server{Addr: c.HTTPAddr, Handler: httpx.Protect(mux, c.Origin), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	failures := make(chan error, 1)
	go func() { failures <- server.ListenAndServe() }()
	slog.Info("API starting", "address", c.HTTPAddr)
	select {
	case err := <-failures:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server failed: %w", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
	return nil
}
