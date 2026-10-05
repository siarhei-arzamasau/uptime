package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMonitorMutationPreflight(t *testing.T) {
	for _, method := range []string{"PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			handler := Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("preflight reached handler") }), "http://localhost:3000")
			r := httptest.NewRequest("OPTIONS", "/api/v1/monitors/id", nil)
			r.Header.Set("Origin", "http://localhost:3000")
			r.Header.Set("Access-Control-Request-Method", method)
			r.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 204 || !strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), method) {
				t.Fatalf("preflight: %d %v", w.Code, w.Header())
			}
			r.Header.Set("Origin", "https://evil.example")
			w = httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatalf("foreign origin: %d", w.Code)
			}
		})
	}
}
