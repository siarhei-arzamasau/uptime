package monitor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
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

func TestFaviconDialAddressFallback(t *testing.T) {
	for _, tc := range []struct {
		name        string
		ips         []netip.Addr
		wantCalls   int
		wantSuccess bool
	}{
		{
			name:        "later public address succeeds",
			ips:         []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111"), netip.MustParseAddr("8.8.8.8")},
			wantCalls:   2,
			wantSuccess: true,
		},
		{
			name:      "all public addresses fail",
			ips:       []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("8.8.4.4")},
			wantCalls: 2,
		},
		{
			name: "mixed public and private rejected before dialing",
			ips:  []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			unavailable := errors.New("unavailable")
			conn, peer := net.Pipe()
			defer conn.Close()
			defer peer.Close()
			d := publicDialer{
				lookup: func(ctx context.Context, network, host string) ([]netip.Addr, error) {
					if network != "ip" || host != "example.com" {
						t.Fatalf("unexpected DNS lookup: %s %s", network, host)
					}
					return tc.ips, nil
				},
				dial: func(ctx context.Context, network, address string) (net.Conn, error) {
					if network != "tcp" || address != net.JoinHostPort(tc.ips[calls].String(), "443") {
						t.Fatalf("did not dial validated address: %s %s", network, address)
					}
					calls++
					if tc.wantSuccess && calls == 2 {
						return conn, nil
					}
					return nil, unavailable
				},
			}
			got, err := d.dialContext(t.Context(), "tcp", "example.com:443")
			if calls != tc.wantCalls || (err == nil) != tc.wantSuccess {
				t.Fatalf("calls=%d, error=%v; want calls=%d, success=%v", calls, err, tc.wantCalls, tc.wantSuccess)
			}
			if tc.wantSuccess && got != conn {
				t.Fatal("did not return successful connection")
			}
			if !tc.wantSuccess && calls > 0 && !errors.Is(err, unavailable) {
				t.Fatalf("lost dial error: %v", err)
			}
		})
	}
}

func TestFaviconDialReservesTimeForFallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		conn, peer := net.Pipe()
		defer conn.Close()
		defer peer.Close()
		calls := 0
		d := publicDialer{
			lookup: func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("8.8.8.8")}, nil
			},
			dial: func(attempt context.Context, network, address string) (net.Conn, error) {
				calls++
				if calls == 1 {
					<-attempt.Done()
					return nil, attempt.Err()
				}
				if attempt.Err() != nil {
					t.Fatalf("fallback context expired: %v", attempt.Err())
				}
				return conn, nil
			},
		}
		got, err := d.dialContext(ctx, "tcp", "example.com:443")
		if err != nil || got != conn || calls != 2 || ctx.Err() != nil {
			t.Fatalf("fallback failed within request deadline: calls=%d err=%v", calls, err)
		}
	})
}

func TestFaviconDialStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	d := publicDialer{
		lookup: func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("8.8.8.8")}, nil
		},
		dial: func(context.Context, string, string) (net.Conn, error) {
			calls++
			cancel()
			return nil, context.Canceled
		},
	}
	_, err := d.dialContext(ctx, "tcp", "example.com:443")
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("did not stop on cancellation: calls=%d err=%v", calls, err)
	}
}

func TestFaviconCoalescesConcurrentMisses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		found bool
	}{
		{name: "PNG", found: true},
		{name: "missing icon", found: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := testPNG(t)
				f := newFaviconFetcher()
				release := make(chan struct{})
				var pages, icons atomic.Int64
				f.client.Transport = iconTransport(func(r *http.Request) (*http.Response, error) {
					body := data
					status := http.StatusOK
					if r.URL.Host == "other.example" {
						if r.URL.Path == "/" {
							body = []byte(`<head><link rel="icon" href="/icon.png"></head>`)
						}
						return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
					}
					if r.URL.Path == "/" {
						pages.Add(1)
						<-release
						body = []byte(`<head><link rel="icon" href="/icon.png"></head>`)
					} else {
						icons.Add(1)
					}
					if !tc.found {
						status = http.StatusNotFound
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
				})
				var group sync.WaitGroup
				results := make([]string, 16)
				for i := range results {
					group.Go(func() { results[i] = f.get(t.Context(), "https://example.com/") })
				}
				synctest.Wait()
				other := make(chan string, 1)
				group.Go(func() { other <- f.get(t.Context(), "https://other.example/") })
				synctest.Wait()
				otherReady := len(other) == 1
				close(release)
				group.Wait()
				want := ""
				wantIcons := int64(2)
				if tc.found {
					want = pngData(data)
					wantIcons = 1
				}
				for _, got := range results {
					if got != want {
						t.Fatal("concurrent callers did not share the icon result")
					}
				}
				if pages.Load() != 1 || icons.Load() != wantIcons {
					t.Fatalf("duplicate discovery: pages=%d icons=%d", pages.Load(), icons.Load())
				}
				if !otherReady || <-other == "" {
					t.Fatal("duplicate URL waiters starved an unrelated icon")
				}
				if f.get(t.Context(), "https://example.com/") != want || pages.Load() != 1 {
					t.Fatal("completed result was not cached")
				}
			})
		})
	}
}

func TestFaviconCanceledWaiterDoesNotCancelSharedFetch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := testPNG(t)
		f := newFaviconFetcher()
		release := make(chan struct{})
		var pages atomic.Int64
		f.client.Transport = iconTransport(func(r *http.Request) (*http.Response, error) {
			body := data
			if r.URL.Path == "/" {
				pages.Add(1)
				select {
				case <-release:
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
				body = []byte(`<head><link rel="icon" href="/icon.png"></head>`)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
		})
		leader := make(chan string, 1)
		go func() { leader <- f.get(t.Context(), "https://example.com/") }()
		synctest.Wait()
		ctx, cancel := context.WithCancel(t.Context())
		waiter := make(chan string, 1)
		go func() { waiter <- f.get(ctx, "https://example.com/") }()
		synctest.Wait()
		cancel()
		if <-waiter != "" {
			t.Fatal("canceled waiter returned an icon")
		}
		close(release)
		if <-leader != pngData(data) || pages.Load() != 1 {
			t.Fatal("waiter duplicated or canceled the shared fetch")
		}
	})
}

func TestFaviconLiveWaiterRetriesCanceledLeader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := testPNG(t)
		f := newFaviconFetcher()
		var pages atomic.Int64
		f.client.Transport = iconTransport(func(r *http.Request) (*http.Response, error) {
			body := data
			if r.URL.Path == "/" {
				if pages.Add(1) == 1 {
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				body = []byte(`<head><link rel="icon" href="/icon.png"></head>`)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
		})
		ctx, cancel := context.WithCancel(t.Context())
		leader := make(chan string, 1)
		go func() { leader <- f.get(ctx, "https://example.com/") }()
		synctest.Wait()
		waiter := make(chan string, 1)
		go func() { waiter <- f.get(t.Context(), "https://example.com/") }()
		synctest.Wait()
		cancel()
		if <-leader != "" || <-waiter != pngData(data) || pages.Load() != 2 {
			t.Fatal("live waiter did not retry canceled discovery")
		}
		if f.get(t.Context(), "https://example.com/") != pngData(data) || pages.Load() != 2 {
			t.Fatal("canceled discovery poisoned the shared cache")
		}
	})
}
