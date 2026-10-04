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
type credentialsRequest struct {
	// Validate the address after normalization so surrounding whitespace remains accepted.
	Email    string `json:"email" example:"demo@example.com"`
	Password string `json:"password" minLength:"12" maxLength:"128" example:"correct horse battery staple"`
}
type userResponse struct {
	ID        uuid.UUID `json:"id" swaggertype:"string" format:"uuid"`
	Email     string    `json:"email" format:"email"`
	Name      string    `json:"name"`
	AvatarURL string    `json:"avatar_url"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
}
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type" enums:"Bearer"`
	ExpiresIn   int    `json:"expires_in" example:"900"`
}
type authResponse struct {
	tokenResponse
	User userResponse `json:"user"`
}

func userDTO(u store.User) userResponse {
	return userResponse{u.ID, u.Email, u.Name, u.AvatarURL(), u.CreatedAt}
}

// NewController binds authentication dependencies and the refresh cookie's Secure policy.
// It performs no I/O or validation.
func NewController(service *Service, tokens *Tokens, cookieSecure bool) *Controller {
	return &Controller{service: service, tokens: tokens, cookieSecure: cookieSecure}
}

// RegisterRoutes mounts public authentication operations and JWT-protected /me on mux.
// Request contexts are forwarded to the service; middleware policies belong to the caller.
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
	var body credentialsRequest
	if err := dec.Decode(&body); err != nil {
		return "", "", ErrInvalid
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return "", "", ErrInvalid
	}
	return body.Email, body.Password, nil
}

// register returns tokens only after account and session creation commit together.
// @Summary Register an account
// @ID register
// @Tags auth
// @Description Accepts only email and password. Sets an HttpOnly refresh_token cookie. JSON body is limited to 4096 bytes. Email is trimmed and lowercased; password length is measured in Unicode characters.
// @Accept json
// @Produce json
// @Param Origin header string false "Allowed frontend origin; required for browser requests"
// @Param X-CSRF-Protection header string false "Must be 1 for mutating requests without Origin" enums(1)
// @Param credentials body credentialsRequest true "Email and password"
// @Success 201 {object} authResponse "Account and access token"
// @Failure 400 {object} httpx.ErrorResponse "Invalid JSON or credentials"
// @Failure 409 {object} httpx.ErrorResponse "Email already registered"
// @Failure 403 {object} httpx.ErrorResponse "Origin or CSRF rejected"
// @Failure 429 {object} httpx.ErrorResponse "Authentication capacity exhausted"
// @Failure 500 {object} httpx.ErrorResponse "Internal server error"
// @Header 201 {string} Set-Cookie "HttpOnly refresh_token; SameSite=Lax; Secure when configured"
// @Header 429 {string} Retry-After "1 second"
// @Router /auth/register [post]
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

// login starts an independent session after credentials pass the shared admission gate.
// @Summary Sign in
// @ID login
// @Tags auth
// @Description Accepts only email and password. Sets an HttpOnly refresh_token cookie. JSON body is limited to 4096 bytes. Email is trimmed and lowercased; password length is measured in Unicode characters.
// @Accept json
// @Produce json
// @Param Origin header string false "Allowed frontend origin; required for browser requests"
// @Param X-CSRF-Protection header string false "Must be 1 for mutating requests without Origin" enums(1)
// @Param credentials body credentialsRequest true "Email and password"
// @Success 200 {object} authResponse "Account and access token"
// @Failure 400 {object} httpx.ErrorResponse "Invalid JSON or credentials"
// @Failure 401 {object} httpx.ErrorResponse "Invalid credentials"
// @Failure 403 {object} httpx.ErrorResponse "Origin or CSRF rejected"
// @Failure 429 {object} httpx.ErrorResponse "Authentication capacity exhausted"
// @Failure 500 {object} httpx.ErrorResponse "Internal server error"
// @Header 200 {string} Set-Cookie "HttpOnly refresh_token; SameSite=Lax; Secure when configured"
// @Header 429 {string} Retry-After "1 second"
// @Router /auth/login [post]
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

// refresh rotates the cookie without extending the session's original expiry.
// @Summary Rotate the refresh token
// @ID refresh
// @Tags auth
// @Description Uses the HttpOnly refresh_token cookie and replaces it on success. Refresh calls must be sequential; replay revokes the session. Session expiry is fixed and invalid cookies are cleared.
// @Produce json
// @Security RefreshCookie
// @Param Origin header string false "Allowed frontend origin; required for browser requests"
// @Param X-CSRF-Protection header string false "Must be 1 for mutating requests without Origin" enums(1)
// @Success 200 {object} tokenResponse "New access token; no user field"
// @Failure 401 {object} httpx.ErrorResponse "Missing, expired, revoked, or replayed refresh token"
// @Failure 403 {object} httpx.ErrorResponse "Origin or CSRF rejected"
// @Failure 500 {object} httpx.ErrorResponse "Internal server error"
// @Header 200 {string} Set-Cookie "Replacement HttpOnly refresh_token"
// @Header 401 {string} Set-Cookie "Deletes the invalid refresh_token cookie"
// @Router /auth/refresh [post]
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

// logout clears the cookie even when its refresh session is already absent.
// @Summary Sign out
// @ID logout
// @Tags auth
// @Description Idempotently revokes the current refresh session and clears its cookie. The refresh_token cookie is optional. Existing access JWTs remain valid until expiry.
// @Produce json
// @Param Origin header string false "Allowed frontend origin; required for browser requests"
// @Param X-CSRF-Protection header string false "Must be 1 for mutating requests without Origin" enums(1)
// @Param Cookie header string false "Optional refresh_token=<token> cookie; browsers send it automatically"
// @Success 204 "Signed out; no response body"
// @Failure 403 {object} httpx.ErrorResponse "Origin or CSRF rejected"
// @Failure 500 {object} httpx.ErrorResponse "Internal server error"
// @Header 204 {string} Set-Cookie "Deletes refresh_token"
// @Router /auth/logout [post]
func (a *Controller) logout(w http.ResponseWriter, r *http.Request) {
	if err := a.service.Logout(r.Context(), refreshCookie(r)); err != nil {
		handleError(w, err)
		return
	}
	a.clearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// Authenticate returns middleware that verifies the bearer JWT and adds its user ID
// to the existing request context. Invalid credentials produce 401 without calling next.
// It does not query session state; next remains responsible for observing cancellation.
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

// me reads the JWT owner's current profile rather than embedding profile data in tokens.
// @Summary Get the authenticated account
// @ID me
// @Tags auth
// @Description Returns the account identified by the Bearer JWT, including a public avatar URL or an empty string.
// @Produce json
// @Security BearerAuth
// @Success 200 {object} userResponse "Authenticated account"
// @Failure 401 {object} httpx.ErrorResponse "Invalid access token or deleted user"
// @Failure 403 {object} httpx.ErrorResponse "Origin rejected"
// @Failure 500 {object} httpx.ErrorResponse "Internal server error"
// @Router /auth/me [get]
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
	// Zero means a session cookie and negative means deletion, so clamp rounded TTLs.
	c.MaxAge = max(1, int(time.Until(result.ExpiresAt).Seconds()))
	http.SetCookie(w, c)
	body := tokenResponse{AccessToken: result.Access, TokenType: "Bearer", ExpiresIn: int(AccessTTL.Seconds())}
	if includeUser {
		u := userDTO(result.User)
		httpx.WriteJSON(w, status, authResponse{tokenResponse: body, User: u})
		return
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
	case errors.Is(err, ErrBusy):
		w.Header().Set("Retry-After", "1")
		httpx.WriteError(w, http.StatusTooManyRequests, "auth_busy", "Too many sign-in attempts. Please wait a moment and try again")
	default:
		slog.Error("authentication request failed")
		httpx.WriteError(w, 500, "internal_error", "Internal server error")
	}
}

// UserID returns the identity inserted by Authenticate, or uuid.Nil and false if absent.
// It reads context values without checking cancellation or querying the database.
func UserID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(identityKey{}).(uuid.UUID)
	return id, ok
}
