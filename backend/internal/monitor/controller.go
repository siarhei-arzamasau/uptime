package monitor

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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

func validURL(value string) bool {
	if len(value) > 2048 || !utf8.ValidString(value) || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.Contains(value, "#") || strings.Contains(value, "\\") {
		return false
	}
	if strings.ContainsAny(u.Hostname(), "%[]") || (strings.HasPrefix(u.Host, "[") && net.ParseIP(u.Hostname()) == nil) {
		return false
	}
	if port := u.Port(); port != "" {
		if number, err := strconv.Atoi(port); err != nil || number > 65535 {
			return false
		}
	}
	return true
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
		httpx.WriteError(w, 400, "invalid_url", "Enter an HTTP or HTTPS URL up to 2048 bytes without credentials or a fragment")
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
	monitors, err := c.store.MonitorsByUser(r.Context(), id)
	if err != nil {
		slog.Error("monitor listing failed")
		httpx.WriteError(w, 500, "internal_error", "Unable to load monitors")
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"monitors": monitors})
}
