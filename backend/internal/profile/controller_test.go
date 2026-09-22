package profile

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestValidName(t *testing.T) {
	for _, tc := range []struct {
		name  string
		valid bool
	}{
		{"", true}, {"\u0421\u0435\u0440\u0433\u0435\u0439 O’Connor", true}, {strings.Repeat("界", 100), true},
		{strings.Repeat("界", 101), false}, {"line\nfeed", false}, {"null\x00byte", false},
	} {
		if validName(tc.name) != tc.valid {
			t.Errorf("name validation mismatch for %q", tc.name)
		}
	}
}

func TestAvatarDimensions(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 2049, 1))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeAvatar(data.Bytes()); err == nil {
		t.Fatal("oversized dimensions accepted")
	}
}
