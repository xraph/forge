package telemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"

	"github.com/xraph/forge/internal/telemetry/spanerror"
)

// InstrumentTransport wraps rt so outbound requests get a client span and
// carry the trace headers. It captures the global tracer provider and
// propagator current at the call, so in a process with several apps the
// transport stays with whichever provider was current then. Build it once and
// keep the result.
//
// otelhttp copies the request URL onto the span as url.full and writes a
// transport error's text into the span status. A URL can carry a password and
// a query string can carry a token, and a dial error names the address it
// tried, so neither reaches otelhttp as is. It sees the request with the
// userinfo and fragment removed and every query value replaced by REDACTED,
// and the real URL goes back on underneath it, just before rt sends the
// request. An error from rt, or from reading the response body, passes up
// through otelhttp carrying only its class (spanerror.ErrorClass), and the
// caller gets the original error back. The path is kept, so a URL with a
// secret in its path, a webhook URL for one, needs a transport that hides it.
func InstrumentTransport(rt http.RoundTripper) http.RoundTripper {
	if rt == nil {
		rt = http.DefaultTransport
	}

	traced := otelhttp.NewTransport(&unmaskTransport{base: rt},
		otelhttp.WithTracerProvider(otel.GetTracerProvider()),
		otelhttp.WithPropagators(otel.GetTextMapPropagator()),
	)

	return &maskTransport{traced: traced}
}

// HTTPClient returns a client whose requests are traced. Every client shares
// http.DefaultTransport's connection pool.
func HTTPClient() *http.Client {
	return &http.Client{Transport: InstrumentTransport(nil)}
}

// realURLKey carries the request's real URL past otelhttp. A nil value means
// there is nothing to restore.
type realURLKey struct{}

// maskTransport sits above otelhttp. It hands otelhttp a copy of the request
// whose URL is scrubbed, and turns masked errors back into the originals.
type maskTransport struct {
	traced http.RoundTripper
}

func (t *maskTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx := r.Context()

	switch {
	case r.URL != nil && urlNeedsMask(r.URL):
		masked := r.WithContext(context.WithValue(ctx, realURLKey{}, r.URL))
		masked.URL = maskURL(r.URL)
		r = masked
	case ctx.Value(realURLKey{}) != nil:
		// The context came from a request that was masked further up, a
		// token fetch inside another traced transport say. Clear it, or the
		// layer below would put that request's URL on this one.
		r = r.WithContext(context.WithValue(ctx, realURLKey{}, (*url.URL)(nil)))
	}

	resp, err := t.traced.RoundTrip(r)
	if err != nil {
		return resp, unmaskError(err)
	}

	if resp.Body != nil {
		resp.Body = wrapBody(resp.Body, unmaskError)
	}

	return resp, nil
}

// unmaskTransport sits under otelhttp. It puts the real URL back before base
// sends the request, and masks what base returns.
type unmaskTransport struct {
	base http.RoundTripper
}

func (t *unmaskTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if orig, ok := r.Context().Value(realURLKey{}).(*url.URL); ok && orig != nil {
		out := r.WithContext(r.Context())
		out.URL = orig
		r = out
	}

	resp, err := t.base.RoundTrip(r)
	if err != nil {
		return resp, maskError(err)
	}

	if resp.Body != nil {
		resp.Body = wrapBody(resp.Body, maskError)
	}

	return resp, nil
}

func urlNeedsMask(u *url.URL) bool {
	return u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawFragment != ""
}

// maskURL returns a copy of u without userinfo or fragment and with every
// query value replaced by REDACTED.
func maskURL(u *url.URL) *url.URL {
	m := *u
	m.User = nil
	m.RawQuery = scrubQuery(u.RawQuery, nil)
	m.Fragment = ""
	m.RawFragment = ""

	return &m
}

// maskedError is what otelhttp sees in place of a transport error. Its text
// and its error.type are the error's class alone.
type maskedError struct {
	err   error
	class string
}

func (e *maskedError) Error() string { return e.class }

// ErrorType is what otelhttp reads for the client span's error.type.
func (e *maskedError) ErrorType() string { return e.class }

func maskError(err error) error {
	return &maskedError{err: err, class: spanerror.ErrorClass(err)}
}

func unmaskError(err error) error {
	if me, ok := errors.AsType[*maskedError](err); ok {
		return me.err
	}

	return err
}

// wrapBody applies fix to the errors a response body's Read and Write return.
// A bare io.EOF passes untouched, since otelhttp compares against it to end
// the span. A body that is an io.ReadWriteCloser, as a 101 Switching
// Protocols response body is, stays one, so a WebSocket client can still
// write to it.
func wrapBody(body io.ReadCloser, fix func(error) error) io.ReadCloser {
	if rw, ok := body.(io.ReadWriteCloser); ok {
		return &fixedRWBody{fixedBody: fixedBody{ReadCloser: rw, fix: fix}, w: rw}
	}

	return &fixedBody{ReadCloser: body, fix: fix}
}

type fixedBody struct {
	io.ReadCloser

	fix func(error) error
}

func (b *fixedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	// otelhttp compares with ==, so only a bare io.EOF ends its span cleanly.
	if err == nil || err == io.EOF {
		return n, err
	}

	return n, b.fix(err)
}

type fixedRWBody struct {
	fixedBody

	w io.Writer
}

func (b *fixedRWBody) Write(p []byte) (int, error) {
	n, err := b.w.Write(p)
	if err != nil {
		err = b.fix(err)
	}

	return n, err
}
