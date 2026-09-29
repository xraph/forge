// http_limit_test.go
package transport

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// countingReader records how many bytes the handler pulled off the wire, so
// a test can tell "refused after reading 1 KiB" from "refused after reading
// all 4 MiB".
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// envelopeOfSize returns a valid users.list query envelope that is exactly
// size bytes long, padded through its payload.
func envelopeOfSize(t *testing.T, size int64) []byte {
	t.Helper()
	mk := func(pad int64) []byte {
		b, err := json.Marshal(contract.Request{
			Envelope: "v1", Kind: contract.KindQuery, Contributor: "users", Intent: "users.list", IntentVersion: 1,
			Payload: json.RawMessage(`"` + strings.Repeat("a", int(pad)) + `"`),
		})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	base := int64(len(mk(0)))
	if size < base {
		t.Fatalf("size %d is below the minimum envelope size %d", size, base)
	}
	b := mk(size - base)
	if int64(len(b)) != size {
		t.Fatalf("envelope is %d bytes, want %d", len(b), size)
	}
	return b
}

// postBody sends body with no declared length, the way a chunked upload
// arrives, so the handler cannot refuse it from the header alone.
func postBody(h http.Handler, body []byte) (*httptest.ResponseRecorder, *countingReader) {
	cr := &countingReader{r: bytes.NewReader(body)}
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", cr)
	req.ContentLength = -1
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w, cr
}

func assertTooLarge(t *testing.T, w *httptest.ResponseRecorder, disp *stubDispatcher) {
	t.Helper()
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%.200s", w.Code, w.Body)
	}
	var resp contract.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != contract.CodeBadRequest {
		t.Errorf("error = %+v, want code %s", resp.Error, contract.CodeBadRequest)
	}
	if disp.called != "" {
		t.Errorf("dispatcher ran for an oversized body: %s", disp.called)
	}
}

func TestHandler_RefusesOversizedBodyWithoutReadingIt(t *testing.T) {
	reg, wreg := setupRegistry(t)
	disp := &stubDispatcher{response: json.RawMessage(`{}`)}
	h := NewHandler(reg, wreg, disp, contract.NoopAuditEmitter{})

	body := envelopeOfSize(t, 4*DefaultMaxBodyBytes)
	w, cr := postBody(h, body)

	assertTooLarge(t, w, disp)
	// http.MaxBytesReader stops one byte past the cap. Anything more means
	// the handler kept reading after it knew the answer.
	if cr.n > DefaultMaxBodyBytes+1 {
		t.Errorf("handler read %d of %d bytes; want at most %d", cr.n, len(body), DefaultMaxBodyBytes+1)
	}
}

func TestHandler_RefusesOversizedDeclaredLengthBeforeReading(t *testing.T) {
	reg, wreg := setupRegistry(t)
	disp := &stubDispatcher{response: json.RawMessage(`{}`)}
	h := NewHandler(reg, wreg, disp, contract.NoopAuditEmitter{}, WithMaxBodyBytes(1024))

	body := envelopeOfSize(t, 2048)
	cr := &countingReader{r: bytes.NewReader(body)}
	req := httptest.NewRequest(http.MethodPost, "/api/dashboard/v1", cr)
	req.ContentLength = int64(len(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assertTooLarge(t, w, disp)
	if cr.n != 0 {
		t.Errorf("handler read %d bytes of a body whose declared length was already over the cap", cr.n)
	}
}

func TestHandler_AcceptsBodyAtDefaultLimit(t *testing.T) {
	reg, wreg := setupRegistry(t)
	disp := &stubDispatcher{response: json.RawMessage(`{}`)}
	h := NewHandler(reg, wreg, disp, contract.NoopAuditEmitter{})

	w, _ := postBody(h, envelopeOfSize(t, DefaultMaxBodyBytes))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%.200s", w.Code, w.Body)
	}
	if disp.called != "query:users.list" {
		t.Errorf("dispatcher not called: %q", disp.called)
	}
}

func TestHandler_HonoursConfiguredLimit(t *testing.T) {
	const limit = 4096
	reg, wreg := setupRegistry(t)

	t.Run("at limit", func(t *testing.T) {
		disp := &stubDispatcher{response: json.RawMessage(`{}`)}
		h := NewHandler(reg, wreg, disp, contract.NoopAuditEmitter{}, WithMaxBodyBytes(limit))
		w, _ := postBody(h, envelopeOfSize(t, limit))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%.200s", w.Code, w.Body)
		}
	})

	// One byte over a configured cap that sits far below the default, so a
	// handler that ignored the option would accept it.
	t.Run("one byte over", func(t *testing.T) {
		disp := &stubDispatcher{response: json.RawMessage(`{}`)}
		h := NewHandler(reg, wreg, disp, contract.NoopAuditEmitter{}, WithMaxBodyBytes(limit))
		w, cr := postBody(h, envelopeOfSize(t, limit+1))
		assertTooLarge(t, w, disp)
		if cr.n > limit+1 {
			t.Errorf("handler read %d bytes; want at most %d", cr.n, limit+1)
		}
	})

	t.Run("raised above default", func(t *testing.T) {
		disp := &stubDispatcher{response: json.RawMessage(`{}`)}
		h := NewHandler(reg, wreg, disp, contract.NoopAuditEmitter{}, WithMaxBodyBytes(2*DefaultMaxBodyBytes))
		w, _ := postBody(h, envelopeOfSize(t, DefaultMaxBodyBytes+1))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%.200s", w.Code, w.Body)
		}
	})

	t.Run("non-positive keeps default", func(t *testing.T) {
		disp := &stubDispatcher{response: json.RawMessage(`{}`)}
		h := NewHandler(reg, wreg, disp, contract.NoopAuditEmitter{}, WithMaxBodyBytes(0))
		w, _ := postBody(h, envelopeOfSize(t, DefaultMaxBodyBytes+1))
		assertTooLarge(t, w, disp)
	})
}
