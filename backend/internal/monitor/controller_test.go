package monitor

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestValidURL(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"https://example.com", true}, {"http://localhost:8080/health?full=1", true}, {"https://[::1]/", true},
		{"HTTPS://example.com", true}, {"https://example.com:99999", false}, {"https://[invalid]/", false},
		{"", false}, {"example.com", false}, {"ftp://example.com", false}, {"https://", false},
		{"https://user:password@example.com", false}, {"https://example.com/#section", false},
		{"https://example.com/#", false}, {"https://example.com/a b", false}, {"https://example.com/\n", false},
		{"https://example.com/\\path", false}, {"https://example.com:" + "bad", false},
		{"https://example.com/" + strings.Repeat("a", 2048), false},
	} {
		if got := validURL(tc.value); got != tc.valid {
			t.Errorf("validURL(%q) = %v, want %v", tc.value, got, tc.valid)
		}
	}
}

func TestURLContract(t *testing.T) {
	data, err := os.ReadFile("../../../contracts/monitor-urls.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Value string `json:"value"`
		Valid bool   `json:"valid"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		if got := validURL(tc.Value); got != tc.Valid {
			t.Errorf("validURL(%q) = %v, want %v", tc.Value, got, tc.Valid)
		}
	}
}
