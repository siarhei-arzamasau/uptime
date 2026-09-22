package monitor

import (
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
