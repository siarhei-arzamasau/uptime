package monitor

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"uptime-app/backend/internal/auth"
	"uptime-app/backend/internal/httpx"
	"uptime-app/backend/internal/store"
)

type Controller struct{ store *store.Store }

func NewController(s *store.Store) *Controller { return &Controller{store: s} }

func (c *Controller) RegisterRoutes(mux *http.ServeMux, authenticate func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/monitors", authenticate(http.HandlerFunc(c.list)))
	mux.Handle("POST /api/v1/monitors", authenticate(http.HandlerFunc(c.create)))
}

func (c *Controller) create(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.UserID(r.Context())
	if !ok {
		httpx.WriteError(w, 401, "unauthorized", "Please sign in to continue")
		return
	}
	var body struct {
		URL             string `json:"url"`
		IntervalSeconds int    `json:"interval_seconds"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" || decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF {
		httpx.WriteError(w, 400, "invalid_request", "Provide a URL and check interval as JSON")
		return
	}
	body.URL = strings.TrimSpace(body.URL)
	if !validURL(body.URL) {
		httpx.WriteError(w, 400, "invalid_url", "Enter an HTTP or HTTPS URL up to 2048 bytes without credentials or a fragment; use punycode for international domains")
		return
	}
	if body.IntervalSeconds < 1 || body.IntervalSeconds > 2147483647 {
		httpx.WriteError(w, 400, "invalid_interval", "Check interval must be a whole number between 1 and 2147483647 seconds")
		return
	}
	m := store.Monitor{ID: uuid.New(), UserID: id, URL: body.URL, IntervalSeconds: body.IntervalSeconds, CreatedAt: time.Now().UTC()}
	if err := c.store.CreateMonitor(r.Context(), &m); err != nil {
		slog.Error("monitor creation failed")
		httpx.WriteError(w, 500, "internal_error", "Unable to create the monitor")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, m)
}

func (c *Controller) list(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.UserID(r.Context())
	if !ok {
		httpx.WriteError(w, 401, "unauthorized", "Please sign in to continue")
		return
	}
	cursor, err := parseCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		httpx.WriteError(w, 400, "invalid_cursor", "Invalid page cursor")
		return
	}
	monitors, err := c.store.MonitorsByUser(r.Context(), id, cursor)
	if err != nil {
		slog.Error("monitor listing failed")
		httpx.WriteError(w, 500, "internal_error", "Unable to load monitors")
		return
	}
	result := struct {
		Monitors   []store.Monitor `json:"monitors"`
		NextCursor string          `json:"next_cursor,omitempty"`
	}{Monitors: monitors}
	if len(monitors) > store.MonitorPageSize {
		result.Monitors = monitors[:store.MonitorPageSize]
		last := result.Monitors[len(result.Monitors)-1]
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID.String()))
	}
	httpx.WriteJSON(w, 200, result)
}

func parseCursor(value string) (*store.MonitorCursor, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > 128 {
		return nil, fmt.Errorf("invalid cursor length")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(decoded), "|")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid cursor")
	}
	created, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, err
	}
	return &store.MonitorCursor{CreatedAt: created, ID: id}, nil
}
