package monitor

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"uptime-app/backend/internal/auth"
	"uptime-app/backend/internal/httpx"
	"uptime-app/backend/internal/store"
)

type statusResponse struct {
	Monitors []store.MonitorStatus `json:"monitors"`
}

func statisticsError(w http.ResponseWriter, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.WriteError(w, 404, "monitor_not_found", "Website not found")
		return
	}
	slog.Error("monitor statistics query failed")
	httpx.WriteError(w, 500, "internal_error", "Unable to load monitoring results")
}

func (c *Controller) addStatuses(w http.ResponseWriter, r *http.Request, monitors []monitorResponse) bool {
	userID, _ := auth.UserID(r.Context())
	ids := make([]uuid.UUID, 0, len(monitors))
	for _, m := range monitors {
		ids = append(ids, m.ID)
	}
	statuses, err := c.store.MonitorStatuses(r.Context(), userID, ids)
	if err != nil {
		statisticsError(w, err)
		return false
	}
	byID := make(map[uuid.UUID]store.MonitorStatus, len(statuses))
	for _, status := range statuses {
		byID[status.ID] = status
	}
	for i := range monitors {
		monitors[i].Check = byID[monitors[i].ID]
	}
	return true
}

func (c *Controller) writeMonitor(w http.ResponseWriter, r *http.Request, m store.Monitor, code int) {
	monitors := c.withFavicons(r.Context(), []store.Monitor{m})
	if !c.addStatuses(w, r, monitors) {
		return
	}
	httpx.WriteJSON(w, code, monitors[0])
}

// @Summary Read current monitor statuses without favicon discovery
// @ID monitorStatuses
// @Tags monitors
// @Description Returns each requested owned monitor. Any missing or foreign ID returns 404. Status is pending before the first result, stale after interval plus 15 seconds, otherwise up or down from the last check.
// @Produce json
// @Security BearerAuth
// @Param ids query string true "Comma-separated unique monitor UUIDs, 1 to 50"
// @Success 200 {object} statusResponse
// @Failure 400 {object} httpx.ErrorResponse "Invalid ID list"
// @Failure 401 {object} httpx.ErrorResponse "Invalid access token"
// @Failure 403 {object} httpx.ErrorResponse "Origin rejected"
// @Failure 404 {object} httpx.ErrorResponse "Monitor not found"
// @Failure 500 {object} httpx.ErrorResponse "Unable to load statuses"
// @Router /monitors/status [get]
func (c *Controller) statuses(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("ids")
	parts := strings.Split(raw, ",")
	if len(raw) > 1850 || len(parts) > 50 {
		httpx.WriteError(w, 400, "invalid_ids", "Provide 1 to 50 unique monitor IDs")
		return
	}
	ids := make([]uuid.UUID, 0, len(parts))
	seen := make(map[uuid.UUID]bool, len(parts))
	for _, part := range parts {
		id, err := uuid.Parse(part)
		if err != nil || seen[id] {
			httpx.WriteError(w, 400, "invalid_ids", "Provide 1 to 50 unique monitor IDs")
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	userID, _ := auth.UserID(r.Context())
	statuses, err := c.store.MonitorStatuses(r.Context(), userID, ids)
	if err != nil {
		statisticsError(w, err)
		return
	}
	httpx.WriteJSON(w, 200, statusResponse{Monitors: statuses})
}

// @Summary Read aggregated monitor availability
// @ID monitorHistory
// @Tags monitors
// @Description UTC minute counters retained for 30 days, grouped into graph buckets. Availability is the percentage of successful samples, not elapsed uptime. Empty buckets have null availability. The start rounds inward to a minute; the last bucket may be partial.
// @Produce json
// @Security BearerAuth
// @Param id path string true "Monitor UUID" format(uuid)
// @Param period query string false "History window" enums(1h,24h,7d,30d) default(24h)
// @Success 200 {object} store.MonitorHistory
// @Failure 400 {object} httpx.ErrorResponse "Invalid ID or period"
// @Failure 401 {object} httpx.ErrorResponse "Invalid access token"
// @Failure 403 {object} httpx.ErrorResponse "Origin rejected"
// @Failure 404 {object} httpx.ErrorResponse "Monitor not found"
// @Failure 500 {object} httpx.ErrorResponse "Unable to load history"
// @Router /monitors/{id}/history [get]
func (c *Controller) history(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "24h"
	}
	_, _, periodErr := store.HistoryWindow(period)
	if err != nil || periodErr != nil {
		httpx.WriteError(w, 400, "invalid_history", "Provide a valid ID and period")
		return
	}
	userID, _ := auth.UserID(r.Context())
	history, err := c.store.CheckHistory(r.Context(), userID, id, period)
	if err != nil {
		statisticsError(w, err)
		return
	}
	httpx.WriteJSON(w, 200, history)
}
