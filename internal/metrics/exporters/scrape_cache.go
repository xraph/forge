package exporters

import (
	"bytes"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/common/expfmt"
)

// Scrape handler limits. A scrape walks every series and encodes all of them,
// which on an app with a few hundred routes is milliseconds of CPU and
// megabytes of garbage, so an unbounded number of them running at once is a
// cheap way to load the process.
const (
	// ScrapeMaxRequestsInFlight caps the scrapes NewScrapeCache lets past it
	// uncached (the ones filtering by name). Further ones get a 503 and the
	// scraper retries on its next interval. Cached scrapes need no cap: they
	// render one at a time.
	ScrapeMaxRequestsInFlight = 4

	// ScrapeTimeout bounds one render. It matches Prometheus' default scrape
	// timeout, after which the scraper has given up anyway.
	ScrapeTimeout = 10 * time.Second
)

// scrapeKey identifies one rendering of the registry. A scrape's bytes depend
// only on the negotiated exposition format and on whether the body is
// gzipped, and both come from a small fixed set, so the cache cannot grow
// with whatever headers a client sends.
type scrapeKey struct {
	format expfmt.Format
	gzip   bool
}

type scrapeEntry struct {
	at     time.Time
	status int
	header http.Header
	body   []byte
}

// scrapeCache serves repeated scrapes from the last rendering for ttl.
type scrapeCache struct {
	next http.Handler
	ttl  time.Duration
	now  func() time.Time

	// mu guards entries and is held across a render. Holding it is what
	// makes a burst of scrapes cost one render: the first one renders and
	// the rest wait for it and then read its result instead of each walking
	// the registry again.
	mu      sync.Mutex
	entries map[scrapeKey]*scrapeEntry

	// bypass bounds the scrapes that skip the cache.
	bypass chan struct{}
}

// NewScrapeCache wraps a Prometheus scrape handler, such as the one
// PrometheusBridge.Handler returns, so that scrapes within ttl of each other
// share one rendering. Scrapes that filter by name (any query string) and
// methods other than GET and HEAD bypass the cache, at most
// ScrapeMaxRequestsInFlight at a time.
//
// A ttl of zero or less turns caching off; the concurrency cap still applies.
func NewScrapeCache(next http.Handler, ttl time.Duration) http.Handler {
	return &scrapeCache{
		next:    next,
		ttl:     ttl,
		now:     time.Now,
		entries: make(map[scrapeKey]*scrapeEntry, 2),
		bypass:  make(chan struct{}, ScrapeMaxRequestsInFlight),
	}
}

func (c *scrapeCache) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if c.ttl <= 0 || (r.Method != http.MethodGet && r.Method != http.MethodHead) || (r.URL != nil && r.URL.RawQuery != "") {
		select {
		case c.bypass <- struct{}{}:
			defer func() { <-c.bypass }()
		default:
			http.Error(w, "too many concurrent scrapes, try again later", http.StatusServiceUnavailable)

			return
		}

		c.next.ServeHTTP(w, r)

		return
	}

	key := scrapeKey{
		format: expfmt.NegotiateIncludingOpenMetrics(r.Header),
		gzip:   acceptsGzip(r.Header.Get("Accept-Encoding")),
	}

	c.mu.Lock()

	e := c.entries[key]
	if e == nil || c.now().Sub(e.at) >= c.ttl {
		e = c.render(r, key)

		// Only a good render is worth repeating. An error or a 503 from the
		// in-flight limit goes to this caller alone.
		if e.status == http.StatusOK {
			c.entries[key] = e
		} else {
			delete(c.entries, key)
		}
	}

	c.mu.Unlock()

	h := w.Header()
	for k, v := range e.header {
		h[k] = slices.Clone(v)
	}

	h.Set("Content-Length", strconv.Itoa(len(e.body)))
	w.WriteHeader(e.status)

	if r.Method != http.MethodHead {
		_, _ = w.Write(e.body)
	}
}

// render runs the wrapped handler into memory. The request is pinned to the
// encoding the key names, so what is stored matches the key whatever else
// the original Accept-Encoding listed.
func (c *scrapeCache) render(r *http.Request, key scrapeKey) *scrapeEntry {
	req := r.Clone(r.Context())
	req.Method = http.MethodGet

	if key.gzip {
		req.Header.Set("Accept-Encoding", "gzip")
	} else {
		req.Header.Set("Accept-Encoding", "identity")
	}

	rec := &bufferedResponse{header: make(http.Header)}
	c.next.ServeHTTP(rec, req)

	status := rec.status
	if status == 0 {
		status = http.StatusOK
	}

	// Drop anything hop-specific; Content-Length is recomputed per write.
	rec.header.Del("Content-Length")

	return &scrapeEntry{at: c.now(), status: status, header: rec.header, body: rec.body.Bytes()}
}

// acceptsGzip reports whether an Accept-Encoding value allows gzip, either by
// name or through a wildcard, without a zero quality.
func acceptsGzip(header string) bool {
	if header == "" {
		return false
	}

	for part := range strings.SplitSeq(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		name = strings.ToLower(strings.TrimSpace(name))

		if name != "gzip" && name != "*" {
			continue
		}

		if q, ok := strings.CutPrefix(strings.ReplaceAll(params, " ", ""), "q="); ok {
			if v, err := strconv.ParseFloat(q, 64); err == nil && v == 0 {
				continue
			}
		}

		return true
	}

	return false
}

// bufferedResponse captures a handler's response in memory.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }

func (b *bufferedResponse) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}

	return b.body.Write(p)
}
