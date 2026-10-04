package profile

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"uptime-app/backend/internal/auth"
	"uptime-app/backend/internal/httpx"
	"uptime-app/backend/internal/store"
)

type Controller struct {
	store     *store.Store
	avatarDir string
}

type updateRequest struct {
	// The 100-character limit applies after trimming, not to the raw JSON string.
	Name *string `json:"name" example:"Sergey"`
}

type profileResponse struct {
	ID        string    `json:"id" format:"uuid"`
	Email     string    `json:"email" format:"email"`
	Name      string    `json:"name"`
	AvatarURL string    `json:"avatar_url"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
}

// NewController binds profile persistence and the local avatar directory.
// The directory is created on upload, not during construction.
func NewController(s *store.Store, avatarDir string) *Controller {
	return &Controller{store: s, avatarDir: avatarDir}
}

// RegisterRoutes mounts authenticated profile reads/updates/uploads and public avatar reads.
// authenticate must supply the auth user ID; request contexts govern database work.
func (c *Controller) RegisterRoutes(mux *http.ServeMux, authenticate func(http.Handler) http.Handler) {
	mux.Handle("POST /api/v1/profile/avatar", authenticate(http.HandlerFunc(c.uploadAvatar)))
	mux.HandleFunc("GET /api/v1/avatars/{filename}", c.serveAvatar)
	mux.Handle("GET /api/v1/profile", authenticate(http.HandlerFunc(c.getProfile)))
	mux.Handle("PATCH /api/v1/profile", authenticate(http.HandlerFunc(c.updateProfile)))
}

// getProfile retrieves the account identified by the Bearer JWT.
// @Summary Get the profile
// @ID getProfile
// @Tags profile
// @Produce json
// @Security BearerAuth
// @Success 200 {object} profileResponse "Current profile"
// @Failure 401 {object} httpx.ErrorResponse "Invalid access token or deleted user"
// @Failure 403 {object} httpx.ErrorResponse "Origin rejected"
// @Failure 500 {object} httpx.ErrorResponse "Unable to load profile"
// @Router /profile [get]
func (c *Controller) getProfile(w http.ResponseWriter, r *http.Request) {
	c.profile(w, r)
}

// updateProfile changes only the name of the authenticated account.
// @Summary Update the profile name
// @ID updateProfile
// @Tags profile
// @Description Accepts only name. Surrounding whitespace is trimmed; at most 100 Unicode characters without controls are allowed. An empty string clears the name. JSON body is limited to 4096 bytes.
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Origin header string false "Allowed frontend origin; required for browser requests"
// @Param X-CSRF-Protection header string false "Must be 1 for mutating requests without Origin" enums(1)
// @Param profile body updateRequest true "New name; empty string clears it"
// @Success 200 {object} profileResponse "Updated profile"
// @Failure 400 {object} httpx.ErrorResponse "Invalid JSON or name"
// @Failure 401 {object} httpx.ErrorResponse "Invalid access token or deleted user"
// @Failure 403 {object} httpx.ErrorResponse "Origin or CSRF rejected"
// @Failure 500 {object} httpx.ErrorResponse "Unable to save profile"
// @Router /profile [patch]
func (c *Controller) updateProfile(w http.ResponseWriter, r *http.Request) {
	c.profile(w, r)
}

func validName(name string) bool {
	return utf8.ValidString(name) && utf8.RuneCountInString(name) <= 100 && !strings.ContainsFunc(name, unicode.IsControl)
}
func (c *Controller) profile(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.UserID(r.Context())
	if !ok {
		httpx.WriteError(w, 401, "unauthorized", "Please sign in to continue")
		return
	}
	var u store.User
	var err error
	if r.Method == http.MethodPatch {
		var body updateRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" || decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || body.Name == nil {
			httpx.WriteError(w, 400, "invalid_request", "Provide only the name field as a string")
			return
		}
		name := strings.TrimSpace(*body.Name)
		if !validName(name) {
			httpx.WriteError(w, 400, "invalid_name", "Name must be at most 100 characters without control characters")
			return
		}
		u, err = c.store.UpdateUserName(r.Context(), id, name)
	} else {
		u, err = c.store.UserByID(r.Context(), id)
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		httpx.WriteError(w, 401, "unauthorized", "User no longer exists")
		return
	}
	if err != nil {
		slog.Error("profile request failed")
		httpx.WriteError(w, 500, "internal_error", "Unable to load or save profile")
		return
	}
	respondProfile(w, u)
}

func respondProfile(w http.ResponseWriter, u store.User) {
	httpx.WriteJSON(w, 200, profileResponse{u.ID.String(), u.Email, u.Name, u.AvatarURL(), u.CreatedAt})
}
