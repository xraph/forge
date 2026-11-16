package router

import (
	"errors"
	"io"
	"net/http"
)

// limitedBody caps a request body at the router's limit from the moment the
// request enters the router, before any global middleware, route matching or
// mounted handler sees it.
//
// The cap used to be installed only in the per-route wrapper. Everything that
// runs before or instead of a route handler read the body unbounded: UseGlobal
// middleware, the 404 and 405 paths, and handlers mounted with Handle. A
// global middleware that buffered the body (to verify a signature, or log it)
// would read 64 MiB from a single request even on a path that does not exist.
//
// A plain http.MaxBytesReader cannot be loosened once installed, which would
// stop a route raising its own cap with WithMaxBodySize. limitedBody can be
// re-limited instead: the per-route wrapper calls setLimit with the route's
// cap, raising or lowering it, and SetRequestBodyLimit does the same for a
// mounted handler. Bytes already read count against the new limit.
type limitedBody struct {
	src io.ReadCloser

	// w is the ResponseWriter the server handed the router. MaxBytesReader
	// uses it to tell the server to close the connection once the cap is
	// hit, which only works on the server's own writer.
	w http.ResponseWriter

	// r is what reads go through: a MaxBytesReader over src sized to what is
	// left of limit, or src itself when the body is unlimited.
	r     io.ReadCloser
	limit int64 // 0 means unlimited
	read  int64
	err   error // sticky once the cap is hit
}

func newLimitedBody(w http.ResponseWriter, src io.ReadCloser, limit int64) *limitedBody {
	b := &limitedBody{src: src, w: w}
	b.setLimit(limit)

	return b
}

// setLimit changes the cap. A limit of zero or less removes it. Once a read
// has failed for being over the cap the body stays failed, because the reader
// may already have consumed a byte past the old cap.
func (b *limitedBody) setLimit(limit int64) {
	if limit < 0 {
		limit = 0
	}

	if b.err != nil || (b.r != nil && limit == b.limit) {
		return
	}

	b.limit = limit

	if limit == 0 {
		b.r = b.src

		return
	}

	b.r = http.MaxBytesReader(b.w, b.src, max(limit-b.read, 0))
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}

	n, err := b.r.Read(p)
	b.read += int64(n)

	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		// The reader was sized to what was left of the limit, so its own
		// error reports the remainder. Report the limit the caller set.
		b.err = &http.MaxBytesError{Limit: b.limit}
		err = b.err
	}

	return n, err
}

func (b *limitedBody) Close() error {
	return b.src.Close()
}

// applyRouteBodyLimit installs a route's body cap on r. When the router has
// already wrapped the body, the cap is changed in place, so a route can raise
// the router's limit as well as lower it. Otherwise, because the router has no
// limit or a middleware replaced the body, a MaxBytesReader is added.
func applyRouteBodyLimit(w http.ResponseWriter, r *http.Request, limit int64) {
	if r.Body == nil || r.Body == http.NoBody {
		return
	}

	if lb, ok := r.Body.(*limitedBody); ok {
		lb.setLimit(limit)

		return
	}

	if limit > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, limit)
	}
}

// SetRequestBodyLimit changes the body cap the router put on r. It is for
// handlers mounted with Handle, which have no route options: a mounted upload
// endpoint calls it before reading to accept more than the router-wide limit.
// A limit of zero or less removes the cap. It reports false, and changes
// nothing, when r's body is not the one the router capped, because there is no
// router limit or a middleware replaced the body.
func SetRequestBodyLimit(r *http.Request, limit int64) bool {
	if r == nil {
		return false
	}

	lb, ok := r.Body.(*limitedBody)
	if !ok {
		return false
	}

	lb.setLimit(limit)

	return true
}
