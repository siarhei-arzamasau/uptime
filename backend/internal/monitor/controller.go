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

type createRequest struct {
	URL             string `json:"url" example:"https://example.com"`
	IntervalSeconds int    `json:"interval_seconds" minimum:"1" maximum:"2147483647" example:"60"`
}

type listResponse struct {
	Monitors   []store.Monitor `json:"monitors"`
	NextCursor string          `json:"next_cursor,omitempty" validate:"optional"`
}

// NewController binds monitor persistence without starting checks or doing I/O.
func NewController(s *store.Store) *Controller { return &Controller{store: s} }

// RegisterRoutes mounts authenticated monitor creation and cursor-based listing.
// authenticate must supply the auth user ID; database work observes each request context.
func (c *Controller) RegisterRoutes(mux *http.ServeMux, authenticate func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/monitors", authenticate(http.HandlerFunc(c.list)))
	mux.Handle("POST /api/v1/monitors", authenticate(http.HandlerFunc(c.create)))
}

// create derives ownership from the JWT so request fields cannot select another user.
// @Summary Create a website monitor
// @ID createMonitor
// @Tags monitors
// @Description Saves configuration only; no outbound checks run. Owner is taken from the Bearer JWT. URL is trimmed and must be absolute HTTP/HTTPS without credentials, whitespace, or fragments, with an ASCII host and at most 2048 bytes. Interval must be a whole number of seconds.
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Origin header string false "Allowed frontend origin; required for browser requests"
// @Param X-CSRF-Protection header string false "Must be 1 for mutating requests without Origin" enums(1)
// @Param monitor body createRequest true "Website URL and interval in seconds"
// @Success 201 {object} store.Monitor "Created monitor"
// @Failure 400 {object} httpx.ErrorResponse "Invalid JSON, URL, or interval"
// @Failure 401 {object} httpx.ErrorResponse "Invalid access token"
// @Failure 403 {object} httpx.ErrorResponse "Origin or CSRF rejected"
// @Failure 500 {object} httpx.ErrorResponse "Unable to create monitor"
// @Router /monitors [post]
func (c *Controller) create(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.UserID(r.Context())
	if !ok {
		httpx.WriteError(w, 401, "unauthorized", "Please sign in to continue")
		return
	}
	var body createRequest
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

// list uses an exclusive cursor to preserve ordering across pages with equal timestamps.
// @Summary List website monitors
// @ID listMonitors
// @Tags monitors
// @Description Returns at most 50 monitors owned by the Bearer JWT identity, newest first by (created_at, id). Use next_cursor for the next page; it is omitted on the last page. Empty monitors is an array, not null.
// @Produce json
// @Security BearerAuth
// @Param cursor query string false "Opaque next_cursor from the preceding page" maxLength(128)
// @Success 200 {object} listResponse "Monitor page"
// @Failure 400 {object} httpx.ErrorResponse "Invalid page cursor"
// @Failure 401 {object} httpx.ErrorResponse "Invalid access token"
// @Failure 403 {object} httpx.ErrorResponse "Origin rejected"
// @Failure 500 {object} httpx.ErrorResponse "Unable to load monitors"
// @Router /monitors [get]
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
	result := listResponse{Monitors: monitors}
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
