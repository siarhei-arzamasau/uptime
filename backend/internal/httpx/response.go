package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteJSON commits status and encodes body as JSON; encoding/write errors are ignored.
// Callers must supply JSON-encodable data because a committed status cannot be replaced.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// WriteError writes the shared JSON error envelope with the supplied HTTP status.
// Response-write errors are ignored, as in WriteJSON.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
