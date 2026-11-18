package middleware

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"

	forge "github.com/xraph/forge"
	forge_http "github.com/xraph/go-utils/http"
)

// DefaultCompressMinSize is the smallest response body, in bytes, that Compress
// gzips. Below about a kilobyte the gzip header, trailer and CPU time cost more
// than the bytes saved.
const DefaultCompressMinSize = 1024

// CompressConfig configures CompressWithConfig.
type CompressConfig struct {
	// Level is a compress/gzip level, from gzip.HuffmanOnly to
	// gzip.BestCompression. An invalid level disables compression.
	Level int

	// MinSize is the smallest body, in bytes, worth compressing; smaller
	// bodies are sent as they are. Zero means DefaultCompressMinSize and a
	// negative value compresses every body.
	MinSize int
}

// gzipWriterPools holds one pool of gzip.Writers per compression level. A
// gzip.Writer allocates about a megabyte of compressor state, so creating one
// per request cost far more than compressing a typical response did.
var gzipWriterPools = newGzipWriterPools()

func newGzipWriterPools() *[gzip.BestCompression - gzip.HuffmanOnly + 1]sync.Pool {
	var pools [gzip.BestCompression - gzip.HuffmanOnly + 1]sync.Pool

	for i := range pools {
		level := i + gzip.HuffmanOnly
		pools[i].New = func() any {
			// The level is in range, so this cannot fail.
			w, _ := gzip.NewWriterLevel(io.Discard, level)

			return w
		}
	}

	return &pools
}

// gzipPool returns the writer pool for level, or nil if level is invalid.
func gzipPool(level int) *sync.Pool {
	if level < gzip.HuffmanOnly || level > gzip.BestCompression {
		return nil
	}

	return &gzipWriterPools[level-gzip.HuffmanOnly]
}

// compressBufPool recycles the buffers that hold a response until it is big
// enough to be worth compressing.
var compressBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, DefaultCompressMinSize)

		return &b
	},
}

// Compress middleware compresses HTTP responses using gzip.
//
// A response is compressed only when the client accepts gzip, the request is
// not HEAD, the status can carry a body (not 1xx, 204 or 304) and is not a
// 206 range response, the handler has not set its own Content-Encoding, and
// the body reaches DefaultCompressMinSize. Smaller responses are sent
// unchanged. Every response that passes through gets Vary: Accept-Encoding so
// caches keep the two forms apart.
//
// This is the new forge middleware pattern using forge.Handler.
func Compress(level int) forge.Middleware {
	return CompressWithConfig(CompressConfig{Level: level})
}

// CompressWithConfig is Compress with a configurable minimum body size.
func CompressWithConfig(cfg CompressConfig) forge.Middleware {
	pool := gzipPool(cfg.Level)

	minSize := cfg.MinSize
	if minSize == 0 {
		minSize = DefaultCompressMinSize
	}

	return func(next forge.Handler) forge.Handler {
		if pool == nil {
			// Invalid level: nothing will ever be compressed.
			return next
		}

		return func(ctx forge.Context) error {
			addVaryAcceptEncoding(ctx.Response().Header())

			r := ctx.Request()
			if r.Method == http.MethodHead || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
				return next(ctx)
			}

			gzw := &gzipResponseWriter{
				ResponseWriter: ctx.Response(),
				pool:           pool,
				minSize:        minSize,
				code:           http.StatusOK,
			}
			defer gzw.close()

			// Create a new context with the wrapped response writer
			// We need to preserve the container and context values
			container := ctx.Container()

			newCtx := forge_http.NewContext(gzw, r, container)
			defer newCtx.(forge_http.ContextWithClean).Cleanup()

			// The new Context starts with an empty values map, so without
			// this the handler could not read anything earlier middleware
			// stored, such as the caller's identity. Sharing the map (not
			// copying it) also lets outer middleware see what the handler
			// stores. Cleanup releases the borrow before newCtx is pooled.
			if src, ok := ctx.(interface{ Values() map[string]any }); ok {
				if dst, ok := newCtx.(interface{ ShareValues(values map[string]any) }); ok {
					dst.ShareValues(src.Values())
				}
			}

			// Execute the next handler with the new context
			return next(newCtx)
		}
	}
}

// CompressDefault returns Compress middleware with default compression level.
func CompressDefault() forge.Middleware {
	return Compress(gzip.DefaultCompression)
}

// acceptsGzip reports whether an Accept-Encoding value lists gzip (or its
// legacy alias x-gzip) with a non-zero quality. A plain substring match also
// fired on "gzip;q=0", which is the client saying it does not want gzip.
func acceptsGzip(header string) bool {
	for header != "" {
		var part string

		part, header, _ = strings.Cut(header, ",")
		name, params, _ := strings.Cut(part, ";")
		name = strings.TrimSpace(name)

		if !strings.EqualFold(name, "gzip") && !strings.EqualFold(name, "x-gzip") {
			continue
		}

		return qualityAboveZero(params)
	}

	return false
}

func qualityAboveZero(params string) bool {
	for params != "" {
		var p string

		p, params, _ = strings.Cut(params, ";")

		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "q") {
			continue
		}

		q, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return true
		}

		return q > 0
	}

	return true
}

// addVaryAcceptEncoding adds Accept-Encoding to Vary unless it is already
// listed or Vary is "*".
func addVaryAcceptEncoding(h http.Header) {
	for _, v := range h.Values("Vary") {
		for f := range strings.SplitSeq(v, ",") {
			f = strings.TrimSpace(f)
			if f == "*" || strings.EqualFold(f, "Accept-Encoding") {
				return
			}
		}
	}

	h.Add("Vary", "Accept-Encoding")
}

// errWriteAfterClose is returned by writes that arrive after the handler has
// returned and the gzip stream has been finished.
var errWriteAfterClose = errors.New("middleware: write to compressed response after handler returned")

type gzipState uint8

const (
	// gzipPending: nothing sent yet. The body so far is held in buf until it
	// is big enough to compress or the handler returns.
	gzipPending gzipState = iota
	// gzipCompressing: headers sent with Content-Encoding: gzip.
	gzipCompressing
	// gzipPassthrough: headers sent unchanged, writes go straight through.
	gzipPassthrough
	// gzipClosed: the handler has returned.
	gzipClosed
)

// gzipResponseWriter compresses the response. It decides when the body
// reaches minSize (or on Flush) rather than up front, so small and
// uncompressible responses are left alone.
type gzipResponseWriter struct {
	http.ResponseWriter

	pool        *sync.Pool
	gz          *gzip.Writer
	buf         []byte
	bufp        *[]byte
	minSize     int
	code        int
	wroteHeader bool
	state       gzipState
}

// WriteHeader records the status code. Nothing is sent until the body shows
// whether it will be compressed, because that decides the headers.
func (gzw *gzipResponseWriter) WriteHeader(statusCode int) {
	if gzw.state != gzipPending {
		if gzw.state != gzipClosed {
			// Let the underlying writer report the superfluous call.
			gzw.ResponseWriter.WriteHeader(statusCode)
		}

		return
	}

	// Informational responses go out at once and the real status follows.
	if statusCode >= 100 && statusCode < 200 && statusCode != http.StatusSwitchingProtocols {
		gzw.ResponseWriter.WriteHeader(statusCode)

		return
	}

	if gzw.wroteHeader {
		return
	}

	gzw.wroteHeader = true
	gzw.code = statusCode

	if !gzw.compressible() {
		gzw.startPlain()

		return
	}

	// A declared length settles the question without waiting for the body.
	if n := gzw.declaredLength(); n >= 0 {
		if n >= int64(gzw.minSize) {
			gzw.startGzip(nil)
		} else {
			gzw.startPlain()
		}
	}
}

// compressible reports whether the status and headers allow a gzip body.
func (gzw *gzipResponseWriter) compressible() bool {
	switch gzw.code {
	case http.StatusNoContent, http.StatusNotModified, http.StatusPartialContent:
		return false
	}

	if gzw.code < http.StatusOK {
		return false
	}

	h := gzw.Header()

	return h.Get("Content-Encoding") == "" && h.Get("Content-Range") == ""
}

// declaredLength is the handler's Content-Length, or -1 if it set none.
func (gzw *gzipResponseWriter) declaredLength() int64 {
	cl := gzw.Header().Get("Content-Length")
	if cl == "" {
		return -1
	}

	n, err := strconv.ParseInt(cl, 10, 64)
	if err != nil || n < 0 {
		return -1
	}

	return n
}

// Write holds the body until it reaches minSize, then compresses.
func (gzw *gzipResponseWriter) Write(b []byte) (int, error) {
	if gzw.state == gzipPending && !gzw.wroteHeader {
		gzw.WriteHeader(http.StatusOK)
	}

	switch gzw.state {
	case gzipCompressing:
		return gzw.gz.Write(b)
	case gzipPassthrough:
		return gzw.ResponseWriter.Write(b)
	case gzipClosed:
		return 0, errWriteAfterClose
	case gzipPending:
	}

	if len(gzw.buf)+len(b) < gzw.minSize {
		if gzw.bufp == nil {
			gzw.bufp = compressBufPool.Get().(*[]byte)
			gzw.buf = (*gzw.bufp)[:0]
		}

		gzw.buf = append(gzw.buf, b...)

		return len(b), nil
	}

	gzw.startGzip(b)

	return gzw.gz.Write(b)
}

// startGzip sends the headers for a compressed response and feeds the held
// body into the gzip stream. next is the write about to follow, used only to
// sniff the content type.
func (gzw *gzipResponseWriter) startGzip(next []byte) {
	h := gzw.Header()

	// Without a Content-Type, net/http sniffs the first bytes it is given,
	// which here would be gzip and come out as application/x-gzip. Sniff the
	// uncompressed body instead.
	if h.Get("Content-Type") == "" {
		if sniff := gzw.sniffBytes(next); len(sniff) > 0 {
			h.Set("Content-Type", http.DetectContentType(sniff))
		}
	}

	// The handler's Content-Length describes the uncompressed body.
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")

	gzw.gz = gzw.pool.Get().(*gzip.Writer)
	gzw.gz.Reset(gzw.ResponseWriter)
	gzw.state = gzipCompressing

	gzw.ResponseWriter.WriteHeader(gzw.code)

	if len(gzw.buf) > 0 {
		_, _ = gzw.gz.Write(gzw.buf)
	}

	gzw.releaseBuf()
}

func (gzw *gzipResponseWriter) sniffBytes(next []byte) []byte {
	const sniffLen = 512

	if len(gzw.buf) == 0 {
		return next[:min(len(next), sniffLen)]
	}

	if len(gzw.buf) >= sniffLen || len(next) == 0 {
		return gzw.buf[:min(len(gzw.buf), sniffLen)]
	}

	sniff := make([]byte, 0, sniffLen)
	sniff = append(sniff, gzw.buf...)

	return append(sniff, next[:min(len(next), sniffLen-len(sniff))]...)
}

// startPlain sends the headers unchanged along with any held body.
func (gzw *gzipResponseWriter) startPlain() {
	gzw.state = gzipPassthrough
	gzw.ResponseWriter.WriteHeader(gzw.code)

	if len(gzw.buf) > 0 {
		_, _ = gzw.ResponseWriter.Write(gzw.buf)
	}

	gzw.releaseBuf()
}

func (gzw *gzipResponseWriter) releaseBuf() {
	if gzw.bufp != nil {
		*gzw.bufp = gzw.buf[:0]
		compressBufPool.Put(gzw.bufp)
	}

	gzw.bufp = nil
	gzw.buf = nil
}

// Flush implements http.Flusher. A handler that flushes is streaming and its
// final size is unknown, so a pending response is committed to gzip, as it
// always was, unless the status or headers rule compression out.
func (gzw *gzipResponseWriter) Flush() {
	if gzw.state == gzipPending {
		if !gzw.wroteHeader {
			gzw.WriteHeader(http.StatusOK)
		}

		if gzw.state == gzipPending {
			gzw.startGzip(nil)
		}
	}

	switch gzw.state {
	case gzipCompressing:
		_ = gzw.gz.Flush()
	case gzipClosed:
		return
	case gzipPending, gzipPassthrough:
	}

	if flusher, ok := gzw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// close finishes the response once the handler has returned. A body still
// held below the threshold goes out uncompressed. A handler that wrote
// nothing leaves the response untouched, so an error handler further out can
// still write it.
func (gzw *gzipResponseWriter) close() {
	switch gzw.state {
	case gzipPending:
		if gzw.wroteHeader || len(gzw.buf) > 0 {
			gzw.startPlain()
		}
	case gzipCompressing:
		_ = gzw.gz.Close()
		// Drop the reference to this response before pooling the writer.
		gzw.gz.Reset(io.Discard)
		gzw.pool.Put(gzw.gz)
		gzw.gz = nil
	case gzipPassthrough, gzipClosed:
	}

	gzw.releaseBuf()
	gzw.state = gzipClosed
}
