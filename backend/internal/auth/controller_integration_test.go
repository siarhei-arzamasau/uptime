package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
	"uptime-app/backend/internal/auth"
	"uptime-app/backend/internal/httpx"
	"uptime-app/backend/internal/profile"
	"uptime-app/backend/internal/store"
	"uptime-app/backend/migrations"
)

const password = "correct horse battery staple"

type fixture struct {
	s         *store.Store
	svc       *auth.Service
	tokens    *auth.Tokens
	handler   http.Handler
	avatarDir string
	dsn       string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; PostgreSQL integration tests skipped")
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.HasSuffix(u.Path, "_test") {
		t.Fatal("TEST_DATABASE_URL must name a dedicated database ending in _test")
	}
	ctx := context.Background()
	admin, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err = admin.DB.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	dsn = u.String()
	s, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); admin.DB.Exec("DROP SCHEMA " + schema + " CASCADE"); admin.Close() })
	db, _ := s.DB.DB()
	goose.SetBaseFS(migrations.Files)
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err = goose.Up(db, "."); err != nil {
		t.Fatal(err)
	}
	tokens := auth.NewTokens(strings.Repeat("s", 32), "issuer", "audience")
	svc, err := auth.NewService(s, tokens)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	controller := auth.NewController(svc, tokens, true)
	controller.RegisterRoutes(mux)
	avatarDir := t.TempDir()
	profile.NewController(s, avatarDir).RegisterRoutes(mux, controller.Authenticate)
	return &fixture{s, svc, tokens, httpx.Protect(mux, "http://localhost:3000"), avatarDir, dsn}
}
func (f *fixture) register(t *testing.T, email string) auth.Result {
	t.Helper()
	r, err := f.svc.Register(context.Background(), email, password)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func request(h http.Handler, method, path, body, token string, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func checkStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status=%d want=%d body=%s", w.Code, status, w.Body.String())
	}
}
func cookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	cs := w.Result().Cookies()
	if len(cs) != 1 {
		t.Fatal("expected refresh cookie")
	}
	return cs[0]
}
func access(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var b struct {
		Access string `json:"access_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil || b.Access == "" {
		t.Fatal("missing access token", err)
	}
	return b.Access
}
func TestHTTPAuthentication(t *testing.T) {
	f := setup(t)
	h := f.handler
	csrf := map[string]string{"X-CSRF-Protection": "1"}
	body := `{"email":" Alice@Example.com ","password":"` + password + `"}`
	w := request(h, "POST", "/api/v1/auth/register", body, "", nil, csrf)
	checkStatus(t, w, 201)
	first := cookie(t, w)
	jwt := access(t, w)
	if !first.HttpOnly || !first.Secure || first.SameSite != http.SameSiteLaxMode || first.Path != "/api/v1/auth" || first.Domain != "" || first.MaxAge > int(auth.SessionTTL.Seconds()) || first.MaxAge <= 0 {
		t.Fatal("bad cookie attributes", first)
	}
	if bytes.Contains(w.Body.Bytes(), []byte(first.Value)) || bytes.Contains(w.Body.Bytes(), []byte("password")) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("secret exposed or caching enabled")
	}
	checkStatus(t, request(h, "POST", "/api/v1/auth/register", body, "", nil, csrf), 409)
	me := request(h, "GET", "/api/v1/auth/me", "", jwt, nil, nil)
	checkStatus(t, me, 200)
	if !strings.Contains(me.Body.String(), "alice@example.com") {
		t.Fatal("email not normalized")
	}
	checkStatus(t, request(h, "GET", "/api/v1/auth/me", "", "bad", nil, nil), 401)
	checkStatus(t, request(h, "GET", "/api/v1/auth/me", "", "", nil, nil), 401)
	login := request(h, "POST", "/api/v1/auth/login", body, "", nil, csrf)
	checkStatus(t, login, 200)
	other := cookie(t, login)
	wrong := request(h, "POST", "/api/v1/auth/login", strings.Replace(body, password, "wrong password", 1), "", nil, csrf)
	absent := request(h, "POST", "/api/v1/auth/login", strings.Replace(body, "Alice@Example.com", "missing@example.com", 1), "", nil, csrf)
	checkStatus(t, wrong, 401)
	checkStatus(t, absent, 401)
	if wrong.Body.String() != absent.Body.String() {
		t.Fatal("login reveals existence")
	}
	for _, b := range []string{`{`, `{"email":"bad","password":"short"}`, body + body, strings.Replace(body, "}", `,"unexpected":1}`, 1), strings.Repeat("x", 5000)} {
		checkStatus(t, request(h, "POST", "/api/v1/auth/register", b, "", nil, csrf), 400)
	}
	refreshed := request(h, "POST", "/api/v1/auth/refresh", "", "", first, csrf)
	checkStatus(t, refreshed, 200)
	next := cookie(t, refreshed)
	if next.Value == first.Value || bytes.Contains(refreshed.Body.Bytes(), []byte(`"user"`)) {
		t.Fatal("rotation or DTO failed")
	}
	checkStatus(t, request(h, "POST", "/api/v1/auth/refresh", "", "", first, csrf), 401)
	checkStatus(t, request(h, "POST", "/api/v1/auth/refresh", "", "", next, csrf), 401)
	checkStatus(t, request(h, "POST", "/api/v1/auth/refresh", "", "", nil, csrf), 401)
	// Other device remains valid after replay revokes the first session.
	otherRefresh := request(h, "POST", "/api/v1/auth/refresh", "", "", other, csrf)
	checkStatus(t, otherRefresh, 200)
	other = cookie(t, otherRefresh)
	out := request(h, "POST", "/api/v1/auth/logout", "", "", other, csrf)
	checkStatus(t, out, 204)
	if cookie(t, out).MaxAge != -1 {
		t.Fatal("cookie not cleared")
	}
	checkStatus(t, request(h, "POST", "/api/v1/auth/logout", "", "", other, csrf), 204)
	checkStatus(t, request(h, "POST", "/api/v1/auth/logout", "", "", nil, csrf), 204)
	checkStatus(t, request(h, "POST", "/api/v1/auth/refresh", "", "", other, csrf), 401)
	checkStatus(t, request(h, "GET", "/api/v1/auth/me", "", access(t, otherRefresh), nil, nil), 200)
}
func TestCORSAndCSRF(t *testing.T) {
	f := setup(t)
	for _, tc := range []struct {
		method  string
		headers map[string]string
		status  int
	}{
		{"POST", nil, 403}, {"POST", map[string]string{"Origin": "https://evil.example"}, 403},
		{"POST", map[string]string{"Origin": "http://localhost:3000"}, 204},
		{"POST", map[string]string{"X-CSRF-Protection": "1"}, 204},
		{"OPTIONS", map[string]string{"Origin": "http://localhost:3000", "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "content-type,x-csrf-protection"}, 204},
		{"OPTIONS", map[string]string{"Origin": "https://evil.example", "Access-Control-Request-Method": "POST"}, 403},
		{"OPTIONS", map[string]string{"Origin": "http://localhost:3000", "Access-Control-Request-Method": "DELETE"}, 403},
		{"OPTIONS", map[string]string{"Origin": "http://localhost:3000", "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "x-untrusted"}, 403},
	} {
		w := request(f.handler, tc.method, "/api/v1/auth/logout", "", "", nil, tc.headers)
		checkStatus(t, w, tc.status)
		if tc.headers["Origin"] == "http://localhost:3000" && w.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Fatal("missing credentials header")
		}
	}
}
func TestConcurrentRegistration(t *testing.T) {
	f := setup(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.svc.Register(context.Background(), "race@example.com", password)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, auth.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("registration race", success, conflict)
	}
}
func TestConcurrentRefresh(t *testing.T) {
	f := setup(t)
	r := f.register(t, "race@example.com")
	var wg sync.WaitGroup
	results := make(chan auth.Result, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := f.svc.Refresh(context.Background(), r.Refresh)
			results <- v
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	success, denied := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, auth.ErrUnauthorized) {
			denied++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || denied != 1 {
		t.Fatal("refresh race", success, denied)
	}
	for v := range results {
		if v.Refresh != "" {
			if _, err := f.svc.Refresh(context.Background(), v.Refresh); !errors.Is(err, auth.ErrUnauthorized) {
				t.Fatal("revocation not committed", err)
			}
		}
	}
}
func TestExpiryAndPersistence(t *testing.T) {
	f := setup(t)
	r := f.register(t, "persist@example.com")
	ctx := context.Background()
	reopened, err := store.Open(ctx, f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	svc, err := auth.NewService(reopened, f.tokens)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Me(ctx, r.User.ID); err != nil {
		t.Fatal(err)
	}
	v, err := svc.Refresh(ctx, r.Refresh)
	if err != nil {
		t.Fatal("session lost after reconnect", err)
	}
	token, err := f.s.Token(ctx, auth.RefreshHash(v.Refresh))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.DB.Model(&store.Session{}).Where("id = ?", token.SessionID).Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Refresh(ctx, v.Refresh); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatal("expired session accepted", err)
	}
	unknown, _ := auth.NewRefresh()
	if _, err = svc.Refresh(ctx, unknown); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatal("unknown token accepted", err)
	}
}
func TestRollbackOnTokenInsertFailure(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	existing := f.register(t, "existing@example.com")
	// A database constraint simulates a write failure after earlier transaction writes.
	if err := f.s.DB.Exec("ALTER TABLE refresh_tokens ADD CONSTRAINT reject_new_tokens CHECK (false) NOT VALID").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Register(ctx, "rollback@example.com", password); err == nil {
		t.Fatal("expected registration failure")
	}
	if _, err := f.s.UserByEmail(ctx, "rollback@example.com"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("user not rolled back", err)
	}
	var count int64
	f.s.DB.Model(&store.Session{}).Count(&count)
	if count != 1 {
		t.Fatal("session not rolled back")
	}
	if _, err := f.svc.Refresh(ctx, existing.Refresh); err == nil {
		t.Fatal("expected refresh failure")
	}
	old, err := f.s.Token(ctx, auth.RefreshHash(existing.Refresh))
	if err != nil || old.UsedAt != nil {
		t.Fatal("token consumption not rolled back", err)
	}
	w := request(f.handler, "POST", "/api/v1/auth/refresh", "", "", &http.Cookie{Name: "refresh_token", Value: existing.Refresh}, map[string]string{"X-CSRF-Protection": "1"})
	checkStatus(t, w, 500)
	if strings.Contains(w.Body.String(), "reject_new_tokens") || len(w.Result().Cookies()) != 0 {
		t.Fatal("database error leaked or invalid cookie issued")
	}
	if err = f.s.DB.Exec("ALTER TABLE refresh_tokens DROP CONSTRAINT reject_new_tokens").Error; err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.Refresh(ctx, existing.Refresh); err != nil {
		t.Fatal("refresh not retryable after rollback", err)
	}
}
func TestMigrations(t *testing.T) {
	f := setup(t)
	db, _ := f.s.DB.DB()
	if err := goose.Up(db, "."); err != nil {
		t.Fatal(err)
	}
	if err := goose.DownTo(db, ".", 0); err != nil {
		t.Fatal(err)
	}
	if f.s.DB.Migrator().HasTable("users") {
		t.Fatal("rollback did not remove schema")
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatal(err)
	}
	f.register(t, "after-migration@example.com")
}

// Verify the JSON response is consumable by a normal HTTP client as well.
func TestResponseJSON(t *testing.T) {
	f := setup(t)
	w := request(f.handler, "POST", "/api/v1/auth/register", `{"email":"json@example.com","password":"`+password+`"}`, "", nil, map[string]string{"Origin": "http://localhost:3000"})
	checkStatus(t, w, 201)
	var body map[string]any
	data, _ := io.ReadAll(w.Result().Body)
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	if body["token_type"] != "Bearer" || body["expires_in"] != float64(900) {
		t.Fatal("incorrect token response")
	}
}

func TestProfile(t *testing.T) {
	f := setup(t)
	alice := f.register(t, "profile@example.com")
	bob := f.register(t, "other@example.com")
	csrf := map[string]string{"X-CSRF-Protection": "1"}
	endpoint := "/api/v1/profile"
	checkStatus(t, request(f.handler, "GET", endpoint, "", "", nil, nil), 401)
	checkStatus(t, request(f.handler, "PATCH", endpoint, `{"name":"Intruder"}`, "", nil, csrf), 401)
	checkStatus(t, request(f.handler, "PATCH", endpoint, `{"name":"Alice"}`, alice.Access, nil, nil), 403)
	initial := request(f.handler, "GET", endpoint, "", alice.Access, nil, nil)
	checkStatus(t, initial, 200)
	if !strings.Contains(initial.Body.String(), `"name":""`) {
		t.Fatal("existing account should have an empty name")
	}
	for _, body := range []string{`{}`, `null`, `{"name":null}`, `{"name":123}`, `{"name":"Alice","email":"hijack@example.com"}`, `{"name":"Alice","id":"` + bob.User.ID.String() + `"}`, `{"name":"x"}{}`, `{"name":"line\nfeed"}`, `{"name":"` + strings.Repeat("界", 101) + `"}`} {
		checkStatus(t, request(f.handler, "PATCH", endpoint, body, alice.Access, nil, csrf), 400)
	}
	updated := request(f.handler, "PATCH", endpoint, `{"name":"  \u0421\u0435\u0440\u0433\u0435\u0439 Smith  "}`, alice.Access, nil, csrf)
	checkStatus(t, updated, 200)
	if strings.Contains(updated.Body.String(), "password") || strings.Contains(updated.Body.String(), "access_token") {
		t.Fatal("profile leaked secrets")
	}
	reopened, err := store.Open(context.Background(), f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	saved, err := reopened.UserByID(context.Background(), alice.User.ID)
	if err != nil || saved.Name != "\u0421\u0435\u0440\u0433\u0435\u0439 Smith" || saved.Email != alice.User.Email || saved.PasswordHash != alice.User.PasswordHash {
		t.Fatal("name was not persisted independently", err)
	}
	other, err := reopened.UserByID(context.Background(), bob.User.ID)
	if err != nil || other.Name != "" {
		t.Fatal("another user was changed", err)
	}
	for _, path := range []string{endpoint, "/api/v1/auth/me"} {
		w := request(f.handler, "GET", path, "", alice.Access, nil, nil)
		checkStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), saved.Name) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("profile read is stale or cacheable")
		}
	}
	checkStatus(t, request(f.handler, "PATCH", endpoint, `{"name":""}`, alice.Access, nil, csrf), 200)
}

func uploadAvatar(t *testing.T, f *fixture, token string, data []byte, name string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("name", name); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("avatar", "../../untrusted.svg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return request(f.handler, "POST", "/api/v1/profile/avatar", body.String(), token, nil, map[string]string{"Content-Type": writer.FormDataContentType(), "X-CSRF-Protection": "1"})
}
func TestAvatarUpload(t *testing.T) {
	f := setup(t)
	alice := f.register(t, "avatar@example.com")
	bob := f.register(t, "other-avatar@example.com")
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	checkStatus(t, uploadAvatar(t, f, "", pngData.Bytes(), "Alice"), 401)
	for _, data := range [][]byte{[]byte("<svg onload='evil()'></svg>"), []byte("not an image"), pngData.Bytes()[:20]} {
		checkStatus(t, uploadAvatar(t, f, alice.Access, data, "Alice"), 400)
	}
	checkStatus(t, uploadAvatar(t, f, alice.Access, make([]byte, (5<<20)+1), "Alice"), 413)
	checkStatus(t, uploadAvatar(t, f, alice.Access, pngData.Bytes(), strings.Repeat("x", 101)), 400)
	checkStatus(t, request(f.handler, "POST", "/api/v1/profile/avatar", "", alice.Access, nil, nil), 403)
	w := uploadAvatar(t, f, alice.Access, pngData.Bytes(), " Alice ")
	checkStatus(t, w, 200)
	var result struct {
		Name, Email string
		AvatarURL   string `json:"avatar_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Name != "Alice" || result.Email != alice.User.Email || !strings.HasPrefix(result.AvatarURL, "/api/v1/avatars/") {
		t.Fatal("invalid avatar profile", w.Body.String())
	}
	imageResponse := request(f.handler, "GET", result.AvatarURL, "", "", nil, nil)
	checkStatus(t, imageResponse, 200)
	if imageResponse.Header().Get("Content-Type") != "image/png" || imageResponse.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("unsafe image headers")
	}
	if _, err := png.Decode(imageResponse.Body); err != nil {
		t.Fatal(err)
	}
	saved, err := f.s.UserByID(context.Background(), alice.User.ID)
	if err != nil || saved.AvatarURL() != result.AvatarURL {
		t.Fatal("avatar not persisted", err)
	}
	other, err := f.s.UserByID(context.Background(), bob.User.ID)
	if err != nil || other.AvatarFile != "" {
		t.Fatal("another user changed", err)
	}
	first := saved.AvatarFile
	checkStatus(t, uploadAvatar(t, f, alice.Access, []byte("invalid"), "Changed"), 400)
	saved, _ = f.s.UserByID(context.Background(), alice.User.ID)
	if saved.Name != "Alice" || saved.AvatarFile != first {
		t.Fatal("invalid upload changed the profile")
	}
	// A failed DB update must remove its new file and preserve the previous avatar.
	if err := f.s.DB.Exec("ALTER TABLE users ADD CONSTRAINT reject_avatar CHECK (name <> 'Reject') NOT VALID").Error; err != nil {
		t.Fatal(err)
	}
	checkStatus(t, uploadAvatar(t, f, alice.Access, pngData.Bytes(), "Reject"), 500)
	files, err := os.ReadDir(f.avatarDir)
	if err != nil || len(files) != 1 || files[0].Name() != first {
		t.Fatal("failed upload left an orphan or removed old avatar", err)
	}
	var jpegData bytes.Buffer
	if err := jpeg.Encode(&jpegData, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	checkStatus(t, uploadAvatar(t, f, alice.Access, jpegData.Bytes(), "Alice New"), 200)
	if _, err := os.Stat(filepath.Join(f.avatarDir, first)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old avatar not removed", err)
	}
	saved, _ = f.s.UserByID(context.Background(), alice.User.ID)
	if saved.AvatarFile == first || !strings.HasSuffix(saved.AvatarFile, ".jpg") {
		t.Fatal("replacement failed")
	}
	// Ordinary name updates preserve the saved avatar.
	checkStatus(t, request(f.handler, "PATCH", "/api/v1/profile", `{"name":"Renamed"}`, alice.Access, nil, map[string]string{"X-CSRF-Protection": "1"}), 200)
	after, _ := f.s.UserByID(context.Background(), alice.User.ID)
	if after.AvatarFile != saved.AvatarFile {
		t.Fatal("name update discarded avatar")
	}
	checkStatus(t, request(f.handler, "GET", "/api/v1/avatars/not-an-image.svg", "", "", nil, nil), 404)
}
