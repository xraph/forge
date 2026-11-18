package middleware

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	forge "github.com/xraph/forge"
	forge_http "github.com/xraph/go-utils/http"
)

func TestCompress_WithGzipSupport(t *testing.T) {
	body := strings.Repeat("compressed content ", 100)

	// Create a forge handler
	handler := Compress(gzip.DefaultCompression)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, body)
	})

	// Convert to http.Handler for testing
	httpHandler := func(w http.ResponseWriter, r *http.Request) {
		ctx := forge_http.NewContext(w, r, nil)

		defer func() {
			if cleanup, ok := ctx.(interface{ Cleanup() }); ok {
				cleanup.Cleanup()
			}
		}()

		_ = handler(ctx)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	rec := httptest.NewRecorder()

	httpHandler(rec, req)

	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	assert.Contains(t, rec.Header().Get("Vary"), "Accept-Encoding")

	// Decompress to verify content
	gz, err := gzip.NewReader(rec.Body)
	assert.NoError(t, err)

	defer gz.Close()

	content, err := io.ReadAll(gz)
	assert.NoError(t, err)
	assert.Equal(t, body, string(content))
}

func TestCompress_WithoutGzipSupport(t *testing.T) {
	// Create a forge handler
	handler := Compress(gzip.DefaultCompression)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "uncompressed content")
	})

	// Convert to http.Handler for testing
	httpHandler := func(w http.ResponseWriter, r *http.Request) {
		ctx := forge_http.NewContext(w, r, nil)

		defer func() {
			if cleanup, ok := ctx.(interface{ Cleanup() }); ok {
				cleanup.Cleanup()
			}
		}()

		_ = handler(ctx)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	httpHandler(rec, req)

	assert.Empty(t, rec.Header().Get("Content-Encoding"))
	assert.Equal(t, "uncompressed content", rec.Body.String())
}

func TestCompressDefault(t *testing.T) {
	// Create a forge handler
	handler := CompressDefault()(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, strings.Repeat("content ", 200))
	})

	// Convert to http.Handler for testing
	httpHandler := func(w http.ResponseWriter, r *http.Request) {
		ctx := forge_http.NewContext(w, r, nil)

		defer func() {
			if cleanup, ok := ctx.(interface{ Cleanup() }); ok {
				cleanup.Cleanup()
			}
		}()

		_ = handler(ctx)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	rec := httptest.NewRecorder()

	httpHandler(rec, req)

	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
}

func TestCompress_InvalidLevel(t *testing.T) {
	// Create a forge handler
	handler := Compress(999)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "content")
	})

	// Convert to http.Handler for testing
	httpHandler := func(w http.ResponseWriter, r *http.Request) {
		ctx := forge_http.NewContext(w, r, nil)

		defer func() {
			if cleanup, ok := ctx.(interface{ Cleanup() }); ok {
				cleanup.Cleanup()
			}
		}()

		_ = handler(ctx)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	rec := httptest.NewRecorder()

	httpHandler(rec, req)

	// Should fall back to no compression on error
	assert.Empty(t, rec.Header().Get("Content-Encoding"))
	assert.Equal(t, "content", rec.Body.String())
}

// TestCompress_KeepsContextValues pins that the handler behind Compress sees
// what earlier middleware stored. Compress hands the handler a new Context
// around the gzip writer, and that Context used to start with no values.
func TestCompress_KeepsContextValues(t *testing.T) {
	var seen any

	handler := Compress(gzip.DefaultCompression)(func(ctx forge.Context) error {
		seen = ctx.Get("auth_context")
		ctx.Set("from-handler", "h")

		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	ctx := forge_http.NewContext(httptest.NewRecorder(), req, nil)
	defer ctx.(forge_http.ContextWithClean).Cleanup()

	ctx.Set("auth_context", "user-1")

	assert.NoError(t, handler(ctx))
	assert.Equal(t, "user-1", seen)
	assert.Equal(t, "h", ctx.Get("from-handler"))
}

func gunzip(t *testing.T, b []byte) string {
	t.Helper()

	zr, err := gzip.NewReader(bytes.NewReader(b))
	require.NoError(t, err)

	defer zr.Close()

	out, err := io.ReadAll(zr)
	require.NoError(t, err)

	return string(out)
}

// runCompress sends one request through mw and handler and returns the
// recorded response.
func runCompress(t *testing.T, mw forge.Middleware, method, acceptEncoding string, handler forge.Handler) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, "/test", nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}

	rec := httptest.NewRecorder()
	ctx := forge_http.NewContext(rec, req, nil)

	defer ctx.(forge_http.ContextWithClean).Cleanup()

	_ = mw(handler)(ctx)

	return rec
}

func TestCompress_SkipsSmallBodies(t *testing.T) {
	rec := runCompress(t, CompressDefault(), http.MethodGet, "gzip", func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "small")
	})

	assert.Empty(t, rec.Header().Get("Content-Encoding"))
	assert.Equal(t, "small", rec.Body.String())
	// The response still varies on Accept-Encoding: a larger body from the
	// same resource would have been compressed.
	assert.Equal(t, "Accept-Encoding", rec.Header().Get("Vary"))
}

func TestCompress_MinSizeIsConfigurable(t *testing.T) {
	mw := CompressWithConfig(CompressConfig{Level: gzip.BestSpeed, MinSize: 4})

	rec := runCompress(t, mw, http.MethodGet, "gzip", func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "small")
	})

	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	assert.Equal(t, "small", gunzip(t, rec.Body.Bytes()))
}

// Many small writes that add up past the threshold are compressed as one
// stream, with nothing lost at the switch.
func TestCompress_SmallWritesCrossingTheThreshold(t *testing.T) {
	var want strings.Builder

	rec := runCompress(t, CompressDefault(), http.MethodGet, "gzip", func(ctx forge.Context) error {
		ctx.Response().Header().Set("Content-Type", "text/plain")

		for i := range 300 {
			line := fmt.Sprintf("line %d\n", i)
			want.WriteString(line)

			if _, err := ctx.Response().Write([]byte(line)); err != nil {
				return err
			}
		}

		return nil
	})

	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	assert.Equal(t, want.String(), gunzip(t, rec.Body.Bytes()))
}

func TestCompress_SkipsHEAD(t *testing.T) {
	rec := runCompress(t, CompressDefault(), http.MethodHead, "gzip", func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, strings.Repeat("x", 4096))
	})

	assert.Empty(t, rec.Header().Get("Content-Encoding"))
	assert.Equal(t, "Accept-Encoding", rec.Header().Get("Vary"))
}

func TestCompress_SkipsBodylessStatuses(t *testing.T) {
	for _, code := range []int{http.StatusNoContent, http.StatusNotModified} {
		rec := runCompress(t, CompressDefault(), http.MethodGet, "gzip", func(ctx forge.Context) error {
			return ctx.NoContent(code)
		})

		assert.Equal(t, code, rec.Code)
		assert.Empty(t, rec.Header().Get("Content-Encoding"), "status %d", code)
		assert.Zero(t, rec.Body.Len(), "status %d", code)
	}
}

// A handler that already encoded its body must not have it encoded twice.
func TestCompress_LeavesExistingContentEncodingAlone(t *testing.T) {
	payload := bytes.Repeat([]byte{0xbe, 0xef}, 2048)

	rec := runCompress(t, CompressDefault(), http.MethodGet, "gzip, br", func(ctx forge.Context) error {
		ctx.Response().Header().Set("Content-Encoding", "br")

		return ctx.Bytes(http.StatusOK, payload)
	})

	assert.Equal(t, "br", rec.Header().Get("Content-Encoding"))
	assert.Equal(t, payload, rec.Body.Bytes())
}

func TestCompress_RemovesContentLength(t *testing.T) {
	body := strings.Repeat("abc", 2000)

	rec := runCompress(t, CompressDefault(), http.MethodGet, "gzip", func(ctx forge.Context) error {
		ctx.Response().Header().Set("Content-Length", strconv.Itoa(len(body)))

		_, err := ctx.Response().Write([]byte(body))

		return err
	})

	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	assert.Empty(t, rec.Header().Get("Content-Length"))
	assert.Equal(t, body, gunzip(t, rec.Body.Bytes()))
}

// With no Content-Type set, net/http would sniff the gzip bytes and label the
// response application/x-gzip. The type must come from the plain body.
func TestCompress_SniffsContentTypeFromPlainBody(t *testing.T) {
	body := "<!DOCTYPE html><html><body>" + strings.Repeat("<p>hi</p>", 300) + "</body></html>"

	rec := runCompress(t, CompressDefault(), http.MethodGet, "gzip", func(ctx forge.Context) error {
		_, err := ctx.Response().Write([]byte(body))

		return err
	})

	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
}

func TestCompress_HonoursZeroQuality(t *testing.T) {
	for _, ae := range []string{"gzip;q=0", "br, gzip; q=0.0", "identity", "*"} {
		rec := runCompress(t, CompressDefault(), http.MethodGet, ae, func(ctx forge.Context) error {
			return ctx.String(http.StatusOK, strings.Repeat("x", 4096))
		})

		assert.Empty(t, rec.Header().Get("Content-Encoding"), ae)
	}

	for _, ae := range []string{"gzip", "br;q=1.0, GZIP;q=0.5", "x-gzip", "deflate, gzip"} {
		assert.True(t, acceptsGzip(ae), ae)
	}
}

func TestCompress_DoesNotDuplicateVary(t *testing.T) {
	rec := runCompress(t, CompressDefault(), http.MethodGet, "gzip", func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "x")
	})
	assert.Equal(t, []string{"Accept-Encoding"}, rec.Header().Values("Vary"))

	h := http.Header{}
	h.Set("Vary", "Origin, accept-encoding")
	addVaryAcceptEncoding(h)
	assert.Equal(t, []string{"Origin, accept-encoding"}, h.Values("Vary"))
}

// A handler that returns an error without writing must leave the response
// empty, so the error handler can write a plain one. Previously the gzip
// header and trailer were written regardless, under Content-Encoding: gzip.
func TestCompress_UnwrittenResponseStaysEmpty(t *testing.T) {
	rec := runCompress(t, CompressDefault(), http.MethodGet, "gzip", func(forge.Context) error {
		return errors.New("boom")
	})

	assert.Empty(t, rec.Header().Get("Content-Encoding"))
	assert.Zero(t, rec.Body.Len())
}

// Writers come from a pool; each response must still be a complete,
// independent gzip stream.
func TestCompress_PooledWritersProduceIndependentStreams(t *testing.T) {
	for i := range 5 {
		for _, level := range []int{gzip.BestSpeed, gzip.DefaultCompression, gzip.HuffmanOnly} {
			body := strings.Repeat(fmt.Sprintf("response %d level %d ", i, level), 100)

			rec := runCompress(t, Compress(level), http.MethodGet, "gzip", func(ctx forge.Context) error {
				return ctx.String(http.StatusOK, body)
			})

			assert.Equal(t, body, gunzip(t, rec.Body.Bytes()))
		}
	}
}

// End to end through a real server and client: the client's transport asks for
// gzip and transparently decodes it.
func TestCompress_ThroughRouterAndServer(t *testing.T) {
	body := strings.Repeat(`{"id":1,"name":"widget"},`, 200)

	router := forge.NewRouter()
	router.Use(CompressDefault())
	require.NoError(t, router.GET("/x", func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, body)
	}))

	srv := httptest.NewServer(router)
	defer srv.Close()

	for range 3 {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/x", nil)
		require.NoError(t, err)

		resp, err := srv.Client().Do(req)
		require.NoError(t, err)

		got, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())

		assert.True(t, resp.Uncompressed, "response was not gzip encoded")
		assert.Equal(t, body, string(got))
	}
}
