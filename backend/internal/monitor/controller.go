package monitor

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"uptime-app/backend/internal/auth"
	"uptime-app/backend/internal/httpx"
	"uptime-app/backend/internal/store"
)

type Controller struct {
	store    *store.Store
	favicons *faviconFetcher
}

type monitorRequest struct {
	URL             string `json:"url" example:"https://example.com"`
	IntervalSeconds int    `json:"interval_seconds" minimum:"5" maximum:"2147483647" example:"60"`
}

type listResponse struct {
	Monitors   []monitorResponse `json:"monitors"`
	NextCursor string            `json:"next_cursor,omitempty" validate:"optional"`
}

// NewController binds monitor persistence without starting checks or doing I/O.
func NewController(s *store.Store) *Controller {
	return &Controller{store: s, favicons: newFaviconFetcher()}
}

// RegisterRoutes mounts authenticated monitor management and cursor-based listing.
// authenticate must supply the auth user ID; database work observes each request context.
func (c *Controller) RegisterRoutes(mux *http.ServeMux, authenticate func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/monitors/status", authenticate(http.HandlerFunc(c.statuses)))
	mux.Handle("GET /api/v1/monitors/{id}/history", authenticate(http.HandlerFunc(c.history)))
	mux.Handle("GET /api/v1/monitors", authenticate(http.HandlerFunc(c.list)))
	mux.Handle("POST /api/v1/monitors", authenticate(http.HandlerFunc(c.create)))
	mux.Handle("PUT /api/v1/monitors/{id}", authenticate(http.HandlerFunc(c.update)))
	mux.Handle("DELETE /api/v1/monitors/{id}", authenticate(http.HandlerFunc(c.delete)))
}

// create derives ownership from the JWT so request fields cannot select another user.
// @Summary Create a website monitor
// @ID createMonitor
// @Tags monitors
// @Description Saves configuration and discovers an optional PNG favicon; schedules an immediate background GET check. Only original HTTP 200 is successful; redirects are not followed. Owner is taken from the Bearer JWT. URL is trimmed and must be absolute HTTP/HTTPS without credentials, whitespace, or fragments, with an ASCII host and at most 2048 bytes. Interval must be a whole number of seconds, at least 5.
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Origin header string false "Allowed frontend origin; required for browser requests"
// @Param X-CSRF-Protection header string false "Must be 1 for mutating requests without Origin" enums(1)
// @Param monitor body monitorRequest true "Website URL and interval in seconds"
// @Success 201 {object} monitorResponse "Created monitor"
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
	body, valid := readMonitor(w, r)
	if !valid {
		return
	}
	m := store.Monitor{ID: uuid.New(), UserID: id, URL: body.URL, IntervalSeconds: body.IntervalSeconds, CreatedAt: time.Now().UTC()}
	if err := c.store.CreateMonitor(r.Context(), &m); err != nil {
		slog.Error("monitor creation failed")
		httpx.WriteError(w, 500, "internal_error", "Unable to create the monitor")
		return
	}
	c.writeMonitor(w, r, m, http.StatusCreated)
}

// list uses an exclusive cursor to preserve ordering across pages with equal timestamps.
// @Summary List website monitors
// @ID listMonitors
// @Tags monitors
// @Description Returns at most 50 monitors owned by the Bearer JWT identity, newest first by (created_at, id). Use next_cursor for the next page; it is omitted on the last page. Empty monitors is an array, not null. Includes optional PNG favicon data URIs, cached for up to one hour. Favicon discovery is best-effort, limited to public HTTP/HTTPS on standard ports, with a shared three-second request deadline and a one-second budget per URL; concurrent requests for the same URL share discovery, and failures omit favicon.
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
	result := listResponse{}
	if len(monitors) > store.MonitorPageSize {
		monitors = monitors[:store.MonitorPageSize]
		last := monitors[len(monitors)-1]
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID.String()))
	}
	result.Monitors = c.withFavicons(r.Context(), monitors)
	if !c.addStatuses(w, r, result.Monitors) {
		return
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

func readMonitor(w http.ResponseWriter, r *http.Request) (monitorRequest, bool) {
	var body monitorRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	isJSON := strings.Split(r.Header.Get("Content-Type"), ";")[0] == "application/json"
	if !isJSON {
		httpx.WriteError(w, 400, "invalid_request", "Provide a URL and check interval as JSON")
		return body, false
	}
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF {
		httpx.WriteError(w, 400, "invalid_request", "Provide a URL and check interval as JSON")
		return body, false
	}
	body.URL = strings.TrimSpace(body.URL)
	if !validURL(body.URL) {
		httpx.WriteError(
			w,
			400,
			"invalid_url",
			"Enter an HTTP or HTTPS URL up to 2048 bytes without credentials or a fragment; "+
				"use punycode for international domains",
		)
		return body, false
	}
	if body.IntervalSeconds < 5 || body.IntervalSeconds > 2147483647 {
		httpx.WriteError(w, 400, "invalid_interval", "Check interval must be a whole number between 5 and 2147483647 seconds")
		return body, false
	}
	return body, true
}

// update scopes the mutation to the authenticated owner to avoid exposing other users' records.
// @Summary Update a website monitor
// @ID updateMonitor
// @Tags monitors
// @Description Replaces URL and interval for a monitor owned by the Bearer JWT identity. Uses the same URL and interval validation as creation. ID, owner and creation time are preserved. Schedules an immediate check; changing the URL clears history and the last result, changing only the interval preserves history. Discovers an optional PNG favicon for the saved URL; icon failures do not fail the update.
// @Produce json
// @Security BearerAuth
// @Param id path string true "Monitor UUID" format(uuid)
// @Param Origin header string false "Allowed frontend origin; required for browser requests"
// @Param X-CSRF-Protection header string false "Must be 1 for mutating requests without Origin" enums(1)
// @Accept json
// @Param monitor body monitorRequest true "Website URL and interval in seconds"
// @Success 200 {object} monitorResponse "Updated monitor"
// @Failure 400 {object} httpx.ErrorResponse "Invalid monitor ID or JSON, URL, interval"
// @Failure 401 {object} httpx.ErrorResponse "Invalid access token"
// @Failure 403 {object} httpx.ErrorResponse "Origin or CSRF rejected"
// @Failure 404 {object} httpx.ErrorResponse "Monitor not found"
// @Failure 500 {object} httpx.ErrorResponse "Unable to update monitor"
// @Router /monitors/{id} [put]
func (c *Controller) update(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserID(r.Context())
	if !ok {
		httpx.WriteError(w, 401, "unauthorized", "Please sign in to continue")
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, 400, "invalid_id", "Invalid monitor ID")
		return
	}
	body, valid := readMonitor(w, r)
	if !valid {
		return
	}
	m := store.Monitor{ID: id, UserID: userID, URL: body.URL, IntervalSeconds: body.IntervalSeconds}
	err = c.store.UpdateMonitor(r.Context(), &m)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.WriteError(w, 404, "monitor_not_found", "Website not found")
		return
	}
	if err != nil {
		slog.Error("monitor update failed")
		httpx.WriteError(w, 500, "internal_error", "Unable to update the monitor")
		return
	}
	c.writeMonitor(w, r, m, http.StatusOK)
}

// delete scopes the mutation to the authenticated owner to avoid exposing other users' records.
// @Summary Delete a website monitor
// @ID deleteMonitor
// @Tags monitors
// @Description Permanently deletes a monitor owned by the Bearer JWT identity. Missing monitors and monitors belonging to another user both return 404.
// @Produce json
// @Security BearerAuth
// @Param id path string true "Monitor UUID" format(uuid)
// @Param Origin header string false "Allowed frontend origin; required for browser requests"
// @Param X-CSRF-Protection header string false "Must be 1 for mutating requests without Origin" enums(1)
// @Success 204 "Monitor deleted"
// @Failure 400 {object} httpx.ErrorResponse "Invalid monitor ID"
// @Failure 401 {object} httpx.ErrorResponse "Invalid access token"
// @Failure 403 {object} httpx.ErrorResponse "Origin or CSRF rejected"
// @Failure 404 {object} httpx.ErrorResponse "Monitor not found"
// @Failure 500 {object} httpx.ErrorResponse "Unable to delete monitor"
// @Router /monitors/{id} [delete]
func (c *Controller) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserID(r.Context())
	if !ok {
		httpx.WriteError(w, 401, "unauthorized", "Please sign in to continue")
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, 400, "invalid_id", "Invalid monitor ID")
		return
	}
	err = c.store.DeleteMonitor(r.Context(), userID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.WriteError(w, 404, "monitor_not_found", "Website not found")
		return
	}
	if err != nil {
		slog.Error("monitor delete failed")
		httpx.WriteError(w, 500, "internal_error", "Unable to delete the monitor")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
