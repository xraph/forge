package exporters

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingScrape stands in for promhttp: it counts renders and honours the
// Accept-Encoding the cache pins.
type countingScrape struct {
	renders atomic.Int64
	delay   time.Duration
	block   chan struct{}
}

func (c *countingScrape) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.renders.Add(1)

	if c.delay > 0 {
		time.Sleep(c.delay)
	}

	if c.block != nil {
		<-c.block
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	body := "up 1\n"

	if r.Header.Get("Accept-Encoding") == "gzip" {
		w.Header().Set("Content-Encoding", "gzip")

		gz := gzip.NewWriter(w)
		_, _ = io.WriteString(gz, body)
		_ = gz.Close()

		return
	}

	_, _ = io.WriteString(w, body)
}

func TestScrapeCache_ConcurrentScrapesShareOneRender(t *testing.T) {
	next := &countingScrape{delay: 20 * time.Millisecond}
	h := NewScrapeCache(next, time.Second)

	var wg sync.WaitGroup

	for range 50 {
		wg.Go(func() {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_/metrics", nil))

			if rec.Code != http.StatusOK || rec.Body.String() != "up 1\n" {
				t.Errorf("got %d %q", rec.Code, rec.Body.String())
			}
		})
	}

	wg.Wait()

	if n := next.renders.Load(); n != 1 {
		t.Errorf("50 concurrent scrapes rendered %d times, want 1", n)
	}
}

func TestScrapeCache_ExpiresAndKeysByEncoding(t *testing.T) {
	next := &countingScrape{}
	c := NewScrapeCache(next, time.Second).(*scrapeCache)

	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }

	get := func(encoding string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/_/metrics", nil)
		if encoding != "" {
			req.Header.Set("Accept-Encoding", encoding)
		}

		rec := httptest.NewRecorder()
		c.ServeHTTP(rec, req)

		return rec
	}

	get("")
	get("")

	if n := next.renders.Load(); n != 1 {
		t.Fatalf("two scrapes in one window rendered %d times", n)
	}

	gz := get("gzip, deflate")
	if gz.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("gzip scrape got Content-Encoding %q", gz.Header().Get("Content-Encoding"))
	}

	zr, err := gzip.NewReader(gz.Body)
	if err != nil {
		t.Fatal(err)
	}

	if b, _ := io.ReadAll(zr); string(b) != "up 1\n" {
		t.Fatalf("gzip body = %q", b)
	}

	if plain := get("gzip;q=0"); plain.Header().Get("Content-Encoding") != "" {
		t.Fatal("q=0 must not get gzip")
	}

	if n := next.renders.Load(); n != 2 {
		t.Fatalf("plain and gzip should render once each, rendered %d", n)
	}

	now = now.Add(time.Second)
	get("")

	if n := next.renders.Load(); n != 3 {
		t.Fatalf("an expired entry should render again, rendered %d", n)
	}
}

func TestScrapeCache_UncachedScrapesAreCapped(t *testing.T) {
	next := &countingScrape{block: make(chan struct{})}
	h := NewScrapeCache(next, time.Second)

	var wg sync.WaitGroup

	for range ScrapeMaxRequestsInFlight {
		wg.Go(func() {
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/_/metrics?name[]=up", nil))
		})
	}

	deadline := time.Now().Add(5 * time.Second)
	for next.renders.Load() < ScrapeMaxRequestsInFlight && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_/metrics?name[]=up", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("scrape over the cap = %d, want 503", rec.Code)
	}

	close(next.block)
	wg.Wait()
}

func TestAcceptsGzip(t *testing.T) {
	for header, want := range map[string]bool{
		"":                  false,
		"gzip":              true,
		"deflate, gzip":     true,
		"GZIP;q=0.5":        true,
		"gzip;q=0":          false,
		"gzip; q=0":         false,
		"*":                 true,
		"identity":          false,
		"br, zstd":          false,
		"x-gzip-ish, other": false,
	} {
		if got := acceptsGzip(header); got != want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", header, got, want)
		}
	}
}
