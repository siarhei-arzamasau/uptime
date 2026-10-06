package monitor

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"uptime-app/backend/internal/store"
)

type iconTransport func(*http.Request) (*http.Response, error)

func (f iconTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFaviconDiscoveryAndCache(t *testing.T) {
	data := testPNG(t)
	f := newFaviconFetcher()
	calls := 0
	f.client.Transport = iconTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := []byte(`<head><link rel="shortcut ICON" href="../assets/brand.png?x=1&amp;y=2"></head>`)
		switch r.URL.Path {
		case "/start":
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://cdn.example/pages/home"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		case "/assets/brand.png":
			if r.URL.Host != "cdn.example" || r.URL.RawQuery != "x=1&y=2" {
				t.Fatalf("wrong icon URL: %s", r.URL)
			}
			body = data
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got := f.get(ctx, "https://example.com/start")
	if got != pngData(data) || got == "" {
		t.Fatal("PNG icon not discovered")
	}
	before := calls
	if f.get(ctx, "https://example.com/start") != got || calls != before {
		t.Fatal("cache missed")
	}
	f.get(ctx, "https://other.example/start")
	if calls == before {
		t.Fatal("changed URL reused old cache")
	}
}

func TestFaviconFallbackAndInvalidImages(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content []byte
		want    bool
	}{
		{name: "png", content: testPNG(t), want: true},
		{name: "html", content: []byte("<html>not an icon</html>")},
		{name: "svg", content: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)},
		{name: "oversized", content: bytes.Repeat([]byte("x"), faviconLimit+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFaviconFetcher()
			f.client.Transport = iconTransport(func(r *http.Request) (*http.Response, error) {
				status := 404
				if r.URL.Path == "/favicon.png" {
					status = 200
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(tc.content)), Request: r}, nil
			})
			got := f.get(context.Background(), "https://example.com/path")
			if (got != "") != tc.want {
				t.Fatalf("icon presence = %v, want %v", got != "", tc.want)
			}
		})
	}
	var oversized bytes.Buffer
	if err := png.Encode(&oversized, image.NewRGBA(image.Rect(0, 0, 257, 1))); err != nil {
		t.Fatal(err)
	}
	if pngData(oversized.Bytes()) != "" {
		t.Fatal("oversized dimensions accepted")
	}
}

func TestFaviconRejectsNonpublicAddresses(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.0.0.0", "198.18.0.1", "224.0.0.1", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1", "64:ff9b::7f00:1", "2002:7f00:1::"} {
		if publicIP(netip.MustParseAddr(raw)) {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(raw)) {
			t.Errorf("rejected public %s", raw)
		}
	}
	conn, err := dialPublic(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", "80"))
	if err == nil {
		conn.Close()
		t.Fatal("dialed loopback")
	}
}

func TestFaviconURLRestrictionsAndRedirect(t *testing.T) {
	f := newFaviconFetcher()
	f.client.Transport = iconTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"http://user:pass@example.com/icon.png"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if got := f.get(context.Background(), "https://example.com"); got != "" {
		t.Fatal("credential redirect accepted")
	}
	f.client.Transport = iconTransport(func(r *http.Request) (*http.Response, error) {
		t.Errorf("unexpected fetch %s", r.URL)
		return nil, fmt.Errorf("unexpected fetch")
	})
	for _, raw := range []string{"file:///etc/passwd", "http://u:p@example.com", "https://example.com:8443", "data:image/png;base64,aA==", "http://[invalid"} {
		if f.get(context.Background(), raw) != "" {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestFaviconCancellationPreservesMonitor(t *testing.T) {
	f := newFaviconFetcher()
	f.client.Transport = iconTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	c := &Controller{favicons: f}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	monitors := []store.Monitor{{URL: "https://example.com", IntervalSeconds: 60}}
	got := c.withFavicons(ctx, monitors)
	if len(got) != 1 || got[0].URL != monitors[0].URL || got[0].Favicon != "" {
		t.Fatalf("lost monitor on cancellation: %+v", got)
	}
	if len(f.cache) != 0 {
		t.Fatal("cached cancellation as missing icon")
	}
}

func TestFaviconBaseAndLargeHTML(t *testing.T) {
	data := testPNG(t)
	f := newFaviconFetcher()
	f.client.Transport = iconTransport(func(r *http.Request) (*http.Response, error) {
		body := []byte(`<head><base href="https://cdn.example/icons/"><link rel="apple-touch-icon" href="brand.png"></head>` + strings.Repeat("x", 300*1024))
		if r.URL.Host == "cdn.example" && r.URL.Path == "/icons/brand.png" {
			body = data
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})
	if got := f.get(context.Background(), "https://example.com"); got != pngData(data) {
		t.Fatal("missed icon in large page with base URL")
	}
}
