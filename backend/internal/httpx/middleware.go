package httpx

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// Protect applies shared CORS, CSRF, cache and request-timeout policies.
func Protect(next http.Handler, allowedOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if origin != "" && origin != allowedOrigin {
			WriteError(w, http.StatusForbidden, "forbidden", "Origin is not allowed")
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if r.Method == http.MethodOptions {
			if origin == "" {
				WriteError(w, 403, "forbidden", "Origin is required")
				return
			}
			method := r.Header.Get("Access-Control-Request-Method")
			if method != "GET" && method != "POST" {
				WriteError(w, 403, "forbidden", "Method is not allowed")
				return
			}
			for _, h := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
				h = strings.ToLower(strings.TrimSpace(h))
				if h != "" && h != "content-type" && h != "authorization" && h != "x-csrf-protection" {
					WriteError(w, 403, "forbidden", "Header is not allowed")
					return
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Protection")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && origin == "" && r.Header.Get("X-CSRF-Protection") != "1" {
			WriteError(w, 403, "forbidden", "CSRF protection header is required")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
