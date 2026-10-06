package monitor

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/sync/errgroup"
	"uptime-app/backend/internal/store"
)

const faviconLimit = 64 * 1024

type monitorResponse struct {
	store.Monitor
	Favicon string `json:"favicon,omitempty" validate:"optional" example:"data:image/png;base64,..."`
}

type faviconEntry struct {
	image   string
	expires time.Time
}

type faviconFetcher struct {
	client *http.Client
	slots  chan struct{}
	mu     sync.Mutex
	cache  map[string]faviconEntry
}

func newFaviconFetcher() *faviconFetcher {
	transport := &http.Transport{
		DialContext:            dialPublic,
		TLSHandshakeTimeout:    2 * time.Second,
		ResponseHeaderTimeout:  2 * time.Second,
		MaxResponseHeaderBytes: 32 * 1024,
		// Resolve and validate each connection, including redirects; never use environment proxies.
		DisableKeepAlives: true,
	}
	return &faviconFetcher{
		client: &http.Client{Transport: transport, CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 3 || !faviconURL(r.URL) {
				return fmt.Errorf("favicon redirect rejected")
			}
			return nil
		}},
		slots: make(chan struct{}, 8),
		cache: make(map[string]faviconEntry),
	}
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	// Exclude shared, documentation, benchmark, reserved and IPv6 transition ranges.
	for _, prefix := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"2001::/23", "2001:db8::/32", "2002::/16",
	} {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return false
		}
	}
	return ip.Is4() || netip.MustParsePrefix("2000::/3").Contains(ip)
}

func dialPublic(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("favicon address: %w", err)
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("favicon DNS: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("favicon host has no addresses")
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, fmt.Errorf("favicon address is not public")
		}
	}
	// Dial the checked IP directly to prevent DNS rebinding between validation and connection.
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

func faviconURL(u *url.URL) bool {
	if u == nil || u.User != nil || u.Hostname() == "" {
		return false
	}
	return (u.Scheme == "https" && (u.Port() == "" || u.Port() == "443")) ||
		(u.Scheme == "http" && (u.Port() == "" || u.Port() == "80"))
}

func (f *faviconFetcher) read(ctx context.Context, u *url.URL, limit int64) ([]byte, *url.URL, error) {
	if !faviconURL(u) {
		return nil, nil, fmt.Errorf("favicon URL rejected")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("favicon request: %w", err)
	}
	req.Header.Set("User-Agent", "Uptime-Favicon/1.0")
	response, err := f.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch favicon: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("favicon HTTP status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read favicon: %w", err)
	}
	if int64(len(data)) > limit {
		return data[:limit], response.Request.URL, fmt.Errorf("favicon response too large")
	}
	return data, response.Request.URL, nil
}

func pngData(data []byte) string {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width > 256 || config.Height > 256 {
		return ""
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	var clean bytes.Buffer
	if err := png.Encode(&clean, img); err != nil || clean.Len() > faviconLimit {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(clean.Bytes())
}

func iconLinks(data []byte, base *url.URL) []*url.URL {
	links := make([]*url.URL, 0)
	baseSeen := false
	tokenizer := html.NewTokenizer(bytes.NewReader(data))
	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			return links
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		token := tokenizer.Token()
		if token.Data == "body" {
			return links
		}
		if token.Data == "base" && !baseSeen {
			for _, attr := range token.Attr {
				if attr.Key != "href" {
					continue
				}
				baseSeen = true
				reference, err := url.Parse(attr.Val)
				if err == nil {
					candidate := base.ResolveReference(reference)
					if faviconURL(candidate) {
						base = candidate
					}
				}
				break
			}
		}
		if token.Data != "link" {
			continue
		}
		var rel, href string
		for _, attr := range token.Attr {
			switch attr.Key {
			case "rel":
				rel = strings.ToLower(attr.Val)
			case "href":
				href = attr.Val
			}
		}
		isIcon := false
		for _, value := range strings.Fields(rel) {
			if value == "icon" || value == "apple-touch-icon" {
				isIcon = true
			}
		}
		if !isIcon || href == "" {
			continue
		}
		reference, err := url.Parse(href)
		if err == nil && len(links) < 6 {
			candidate := base.ResolveReference(reference)
			if faviconURL(candidate) {
				links = append(links, candidate)
			}
		}
	}
}

func (f *faviconFetcher) fetch(ctx context.Context, rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || !faviconURL(u) {
		return ""
	}
	candidates := make([]*url.URL, 0)
	data, finalURL, readErr := f.read(ctx, u, 256*1024)
	// A bounded HTML prefix still contains the head on large pages. PNGs must be complete.
	if readErr == nil || len(data) > 0 {
		candidates = iconLinks(data, finalURL)
		u = finalURL
	}
	for _, path := range []string{"/favicon.png", "/favicon.ico"} {
		candidates = append(candidates, u.ResolveReference(&url.URL{Path: path}))
	}
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return ""
		}
		data, _, err := f.read(ctx, candidate, faviconLimit)
		if err == nil {
			if image := pngData(data); image != "" {
				return image
			}
		}
	}
	return ""
}

func (f *faviconFetcher) get(ctx context.Context, rawURL string) string {
	f.mu.Lock()
	cached, ok := f.cache[rawURL]
	f.mu.Unlock()
	if ok && time.Now().Before(cached.expires) {
		return cached.image
	}
	select {
	case f.slots <- struct{}{}:
		defer func() { <-f.slots }()
	case <-ctx.Done():
		return ""
	}
	image := f.fetch(ctx, rawURL)
	if ctx.Err() != nil {
		return image
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.cache) >= 256 {
		for key := range f.cache {
			delete(f.cache, key)
			break
		}
	}
	ttl := time.Hour
	if image == "" {
		ttl = 5 * time.Minute
	}
	f.cache[rawURL] = faviconEntry{image: image, expires: time.Now().Add(ttl)}
	return image
}

func (c *Controller) withFavicons(ctx context.Context, monitors []store.Monitor) []monitorResponse {
	// One shared deadline bounds list latency even when every website is unavailable.
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result := make([]monitorResponse, len(monitors))
	var group errgroup.Group
	group.SetLimit(8)
	for i, monitor := range monitors {
		group.Go(func() error {
			result[i] = monitorResponse{Monitor: monitor, Favicon: c.favicons.get(ctx, monitor.URL)}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return result
	}
	return result
}
