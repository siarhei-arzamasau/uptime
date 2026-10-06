package monitor

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/sync/errgroup"
	"uptime-app/backend/internal/store"
)

const faviconLimit = 64 * 1024
const faviconFetchTimeout = time.Second

type monitorResponse struct {
	store.Monitor
	Check   store.MonitorStatus `json:"check"`
	Favicon string              `json:"favicon,omitempty" validate:"optional" example:"data:image/png;base64,..."`
}

type faviconEntry struct {
	image   string
	expires time.Time
}

type faviconCall struct {
	done     chan struct{}
	image    string
	retry    bool
	timedOut bool
}

type faviconFetcher struct {
	client   *http.Client
	slots    chan struct{}
	mu       sync.Mutex
	cache    map[string]faviconEntry
	inflight map[string]*faviconCall
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
		slots:    make(chan struct{}, 8),
		cache:    make(map[string]faviconEntry),
		inflight: make(map[string]*faviconCall),
	}
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
	for {
		if ctx.Err() != nil {
			return ""
		}
		f.mu.Lock()
		cached, ok := f.cache[rawURL]
		if ok && time.Now().Before(cached.expires) {
			f.mu.Unlock()
			return cached.image
		}
		if call, ok := f.inflight[rawURL]; ok {
			f.mu.Unlock()
			select {
			case <-ctx.Done():
				return ""
			case <-call.done:
				if ctx.Err() != nil {
					return ""
				}
				// A shorter-lived caller must not turn cancellation into a shared negative cache result.
				if call.retry {
					continue
				}
				return call.image
			}
		}
		call := &faviconCall{done: make(chan struct{})}
		f.inflight[rawURL] = call
		f.mu.Unlock()

		select {
		case f.slots <- struct{}{}:
			// Each admitted URL gets a smaller budget than the list, allowing later entries to run.
			fetchCtx, cancel := context.WithTimeout(ctx, faviconFetchTimeout)
			image := f.fetch(fetchCtx, rawURL)
			call.timedOut = errors.Is(fetchCtx.Err(), context.DeadlineExceeded)
			cancel()
			<-f.slots
			f.complete(ctx, rawURL, call, image)
			return image
		case <-ctx.Done():
			f.complete(ctx, rawURL, call, "")
			return ""
		}
	}
}

func (f *faviconFetcher) complete(ctx context.Context, rawURL string, call *faviconCall, image string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	call.image = image
	call.retry = ctx.Err() != nil
	if !call.retry {
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
		if call.timedOut {
			// Briefly skip slow sites so they cannot repeatedly monopolize the list's workers.
			ttl = 30 * time.Second
		}
		f.cache[rawURL] = faviconEntry{image: image, expires: time.Now().Add(ttl)}
	}
	delete(f.inflight, rawURL)
	close(call.done)
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
