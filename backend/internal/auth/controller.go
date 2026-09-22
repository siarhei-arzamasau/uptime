package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"uptime-app/backend/internal/httpx"
	"uptime-app/backend/internal/store"
)

const cookieName = "refresh_token"
const cookiePath = "/api/v1/auth"

type Controller struct {
	service      *Service
	tokens       *Tokens
	cookieSecure bool
}
type identityKey struct{}
type userResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	AvatarURL string    `json:"avatar_url"`
	CreatedAt time.Time `json:"created_at"`
}
type tokenResponse struct {
	User        *userResponse `json:"user,omitempty"`
	AccessToken string        `json:"access_token"`
	TokenType   string        `json:"token_type"`
	ExpiresIn   int           `json:"expires_in"`
}

func userDTO(u store.User) userResponse {
	return userResponse{u.ID, u.Email, u.Name, u.AvatarURL(), u.CreatedAt}
}

// NewController creates the authentication module's HTTP controller.
func NewController(service *Service, tokens *Tokens, cookieSecure bool) *Controller {
	return &Controller{service: service, tokens: tokens, cookieSecure: cookieSecure}
}

// RegisterRoutes mounts authentication endpoints on the application's shared mux.
func (a *Controller) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/register", a.register)
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("POST /api/v1/auth/refresh", a.refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", a.logout)
	mux.Handle("GET /api/v1/auth/me", a.Authenticate(http.HandlerFunc(a.me)))
}
func decodeCredentials(w http.ResponseWriter, r *http.Request) (string, string, error) {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return "", "", ErrInvalid
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := dec.Decode(&body); err != nil {
		return "", "", ErrInvalid
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return "", "", ErrInvalid
	}
	return body.Email, body.Password, nil
}
func (a *Controller) register(w http.ResponseWriter, r *http.Request) {
	email, password, err := decodeCredentials(w, r)
	if err != nil {
		handleError(w, err)
		return
	}
	result, err := a.service.Register(r.Context(), email, password)
	if err != nil {
		handleError(w, err)
		return
	}
	a.respondTokens(w, result, http.StatusCreated, true)
}
func (a *Controller) login(w http.ResponseWriter, r *http.Request) {
	email, password, err := decodeCredentials(w, r)
	if err != nil {
		handleError(w, err)
		return
	}
	result, err := a.service.Login(r.Context(), email, password)
	if err != nil {
		handleError(w, err)
		return
	}
	a.respondTokens(w, result, http.StatusOK, true)
}
func refreshCookie(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
func (a *Controller) refresh(w http.ResponseWriter, r *http.Request) {
	result, err := a.service.Refresh(r.Context(), refreshCookie(r))
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			a.clearCookie(w)
		}
		handleError(w, err)
		return
	}
	a.respondTokens(w, result, http.StatusOK, false)
}
func (a *Controller) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.service.Logout(r.Context(), refreshCookie(r)); err != nil {
		handleError(w, err)
		return
	}
	a.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// Authenticate verifies an access token for protected feature routes.
func (a *Controller) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			handleError(w, ErrUnauthorized)
			return
		}
		id, err := a.tokens.Verify(parts[1])
		if err != nil {
			handleError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, id)))
	})
}
func (a *Controller) me(w http.ResponseWriter, r *http.Request) {
	id, ok := r.Context().Value(identityKey{}).(uuid.UUID)
	if !ok {
		handleError(w, ErrUnauthorized)
		return
	}
	u, err := a.service.Me(r.Context(), id)
	if err != nil {
		handleError(w, err)
		return
	}
	httpx.WriteJSON(w, 200, userDTO(u))
}
func (a *Controller) cookie(value string) *http.Cookie {
	return &http.Cookie{Name: cookieName, Value: value, Path: cookiePath, HttpOnly: true, Secure: a.cookieSecure, SameSite: http.SameSiteLaxMode}
}
func (a *Controller) clearCookie(w http.ResponseWriter) {
	c := a.cookie("")
	c.MaxAge = -1
	c.Expires = time.Unix(1, 0)
	http.SetCookie(w, c)
}
func (a *Controller) respondTokens(w http.ResponseWriter, result Result, status int, includeUser bool) {
	c := a.cookie(result.Refresh)
	c.Expires = result.ExpiresAt
	c.MaxAge = max(1, int(time.Until(result.ExpiresAt).Seconds()))
	http.SetCookie(w, c)
	body := tokenResponse{AccessToken: result.Access, TokenType: "Bearer", ExpiresIn: int(AccessTTL.Seconds())}
	if includeUser {
		u := userDTO(result.User)
		body.User = &u
	}
	httpx.WriteJSON(w, status, body)
}
func handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(w, 400, "invalid_request", "Valid email and a password of 12–128 characters are required")
	case errors.Is(err, ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		httpx.WriteError(w, 401, "unauthorized", "Invalid credentials or token")
	case errors.Is(err, ErrConflict):
		httpx.WriteError(w, 409, "email_exists", "Email already registered")
	default:
		slog.Error("authentication request failed")
		httpx.WriteError(w, 500, "internal_error", "Internal server error")
	}
}

// UserID returns the identity established by Authenticate.
func UserID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(identityKey{}).(uuid.UUID)
	return id, ok
}
