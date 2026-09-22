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

func NewController(s *store.Store, avatarDir string) *Controller {
	return &Controller{store: s, avatarDir: avatarDir}
}
func (c *Controller) RegisterRoutes(mux *http.ServeMux, authenticate func(http.Handler) http.Handler) {
	mux.Handle("POST /api/v1/profile/avatar", authenticate(http.HandlerFunc(c.uploadAvatar)))
	mux.HandleFunc("GET /api/v1/avatars/{filename}", c.serveAvatar)
	mux.Handle("GET /api/v1/profile", authenticate(http.HandlerFunc(c.profile)))
	mux.Handle("PATCH /api/v1/profile", authenticate(http.HandlerFunc(c.profile)))
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
		var body struct {
			Name *string `json:"name"`
		}
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
	httpx.WriteJSON(w, 200, struct {
		ID        string    `json:"id"`
		Email     string    `json:"email"`
		Name      string    `json:"name"`
		AvatarURL string    `json:"avatar_url"`
		CreatedAt time.Time `json:"created_at"`
	}{u.ID.String(), u.Email, u.Name, u.AvatarURL(), u.CreatedAt})
}
