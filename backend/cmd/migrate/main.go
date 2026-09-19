package main

import (
	"context"
	"fmt"
	"github.com/pressly/goose/v3"
	"os"
	"time"
	"uptime-app/backend/internal/store"
	"uptime-app/backend/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migration failed:", err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: go run ./cmd/migrate up|down|status")
	}
	command := os.Args[1]
	if command != "up" && command != "down" && command != "status" {
		return fmt.Errorf("unsupported command")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s, err := store.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("database connection failed")
	}
	defer s.Close()
	db, err := s.DB.DB()
	if err != nil {
		return err
	}
	goose.SetBaseFS(migrations.Files)
	if err = goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.RunContext(ctx, command, db, ".")
}
