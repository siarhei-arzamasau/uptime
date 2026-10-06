package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"uptime-app/backend/internal/monitor"
	"uptime-app/backend/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func run() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	concurrency := 128
	if value := os.Getenv("MONITOR_CONCURRENCY"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("MONITOR_CONCURRENCY must be an integer")
		}
		concurrency = parsed
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	s, err := store.Open(connectCtx, dsn)
	cancel()
	if err != nil {
		return fmt.Errorf("worker database connection failed")
	}
	defer func() {
		if err := s.Close(); err != nil {
			slog.Error("worker database close failed")
		}
	}()
	worker, err := monitor.NewWorker(s, concurrency)
	if err != nil {
		return err
	}
	slog.Info("monitor worker starting", "concurrency", concurrency)
	worker.Run(ctx)
	return nil
}
