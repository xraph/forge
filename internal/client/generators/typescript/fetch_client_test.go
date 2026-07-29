package typescript

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFetchOnly generates src/fetch.ts plus src/codecs.ts into a fresh temp
// dir, for tests that exercise HTTPClient's timeout/abort/retry/serialization
// machinery and don't need the rest of the generated tree (rest.ts, types.ts,
// etc). codecs.ts is included — not just fetch.ts alone — because executeRequest
// imports { encode, decode } from './codecs' to apply RequestConfig's
// bodyCodec/responseCodec at the HTTP boundary; without it, esbuild would fail
// to resolve that import for every test using this helper, including the ones
// that never set bodyCodec/responseCodec at all.
func writeFetchOnly(t *testing.T) string {
	t.Helper()

	code := NewFetchClientGenerator().GenerateBaseClient(baseSpec(), baseConfig())
	codecCode, _ := NewCodecGenerator().Generate(baseSpec(), baseConfig())
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"src/fetch.ts":  code,
		"src/codecs.ts": codecCode,
	})

	return dir
}

// decodeLastLine unmarshals the last non-empty line of driver stdout into v.
func decodeLastLine(t *testing.T, stdout string, v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(lastLine(stdout))), v), "driver stdout:\n%s", stdout)
}

// TestFetchTimeoutCoversResponseBodyRead is the runtime proof for defect 3a:
// clearTimeout(timeoutId) used to fire the moment fetch() resolved (headers
// only), before the body was read. A server that sends headers then stalls
// the body hung forever, because nothing was left to abort the stalled
// response.blob() call.
//
// Measured BEFORE the fix (100ms timeout, body a ReadableStream that never
// closes): the request never rejected; the driver's own 1200ms watchdog fired
// and printed {"outcome":"STILL-HANGING","elapsedMs":~1200} because
// client.request() was still pending.
//
// Also carries a positive control: an ordinary request whose body closes
// normally must still resolve with the real value, well inside the timeout.
func TestFetchTimeoutCoversResponseBodyRead(t *testing.T) {
	dir := writeFetchOnly(t)

	cases := []struct {
		name       string
		fetchImpl  string
		wantResult string
	}{
		{
			name: "stalled-body-must-timeout",
			// A realistic mock: headers arrive immediately and the body never
			// closes on its own, but — matching what a real fetch()
			// implementation does internally — the response body stream is
			// wired to the request's abort signal, so aborting mid-read
			// actually terminates the pending body read. Without this wiring
			// the mock wouldn't exercise the fix at all: a signal nothing is
			// listening to can't reject anything no matter how the client
			// code is written.
			fetchImpl: `(globalThis as any).fetch = async (_url: string, init: any) => {
    let ctrl: ReadableStreamDefaultController<any>;
    const stream = new ReadableStream({ start(c) { ctrl = c; } });
    if (init && init.signal) {
      if (init.signal.aborted) {
        ctrl!.error(init.signal.reason);
      } else {
        init.signal.addEventListener('abort', () => ctrl.error(init.signal.reason), { once: true });
      }
    }
    return new Response(stream, { status: 200, headers: { 'content-type': 'application/json' } });
  };`,
			wantResult: "rejected",
		},
		{
			name: "normal-body-succeeds-within-timeout",
			fetchImpl: `(globalThis as any).fetch = async () => new Response(JSON.stringify({ ok: true }), {
    status: 200,
    headers: { 'content-type': 'application/json' },
  });`,
			wantResult: "resolved",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			driver := `
import { HTTPClient } from './fetch';

async function main() {
  const client = new HTTPClient('http://example.invalid', 100); // 100ms timeout

  ` + tc.fetchImpl + `

  const start = Date.now();
  const watchdog = setTimeout(() => {
    console.log(JSON.stringify({ outcome: 'STILL-HANGING', elapsedMs: Date.now() - start }));
    process.exit(0);
  }, 1200);

  try {
    const value = await client.request<any>({ method: 'GET', url: '/slow-body' });
    clearTimeout(watchdog);
    console.log(JSON.stringify({ outcome: 'resolved', value, elapsedMs: Date.now() - start }));
  } catch (err) {
    clearTimeout(watchdog);
    console.log(JSON.stringify({
      outcome: 'rejected',
      name: err instanceof Error ? err.name : typeof err,
      elapsedMs: Date.now() - start,
    }));
  }
}

main().catch((err) => { console.error(err); process.exit(1); });
`
			driverFile := "src/__driver_3a_" + tc.name + ".ts"
			writeTree(t, dir, map[string]string{driverFile: driver})

			stdout := runNodeDriver(t, dir, driverFile)

			var result struct {
				Outcome   string `json:"outcome"`
				Name      string `json:"name"`
				ElapsedMs int    `json:"elapsedMs"`
			}
			decodeLastLine(t, stdout, &result)

			assert.NotEqual(t, "STILL-HANGING", result.Outcome,
				"measured before the fix: STILL-HANGING-after-1200ms (clearTimeout fires when headers arrive, not when the body finishes) — elapsedMs=%d", result.ElapsedMs)
			assert.Equal(t, tc.wantResult, result.Outcome)
			assert.Less(t, result.ElapsedMs, 1000,
				"a stalled body must be aborted by the 100ms timeout well before the 1200ms watchdog; a normal body must resolve fast")
		})
	}
}

// TestFetchCallerAbortReachesManualFallbackDuringBodyRead is the runtime
// proof for defect 3b: on the combineSignals manual fallback (used on
// runtimes without AbortSignal.any, e.g. Node < 20.3, Safari < 17.4),
// combined.dispose() removed the forwarding listeners at the same point
// clearTimeout fired — i.e. the instant headers arrived — so a caller abort
// during the body read never reached the merged controller.
//
// Measured BEFORE the fix: with native AbortSignal.any, aborting mid-body
// threw AbortError in ~51ms. With the fallback forced (AbortSignal.any
// deleted), the same abort left the request STILL-HANGING past 1200ms — the
// exact runtimes the fallback exists for were both untimed AND
// uncancellable after headers arrived.
func TestFetchCallerAbortReachesManualFallbackDuringBodyRead(t *testing.T) {
	dir := writeFetchOnly(t)

	cases := []struct {
		name          string
		forceFallback bool
		abort         bool
		wantResult    string
	}{
		{name: "native-AbortSignal-any", forceFallback: false, abort: true, wantResult: "rejected"},
		{name: "manual-fallback-forced", forceFallback: true, abort: true, wantResult: "rejected"},
		{name: "manual-fallback-no-abort-succeeds", forceFallback: true, abort: false, wantResult: "resolved"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forceLine := ""
			if tc.forceFallback {
				forceLine = "delete (AbortSignal as any).any;"
			}

			abortSetup := "// no caller abort"
			if tc.abort {
				abortSetup = "setTimeout(() => controller.abort(), 50);"
			}

			// A realistic mock fetch: the response body never closes on its
			// own, but — matching what a real fetch() implementation does
			// internally — the body stream is wired to whatever signal was
			// actually passed to fetch() (HTTPClient's merged/combined
			// signal), so an abort that reaches that signal terminates the
			// pending body read. In the no-abort case the stream instead
			// closes on its own shortly after the request starts, so the
			// positive control resolves instead of hanging.
			streamSetup := `let ctrl: ReadableStreamDefaultController<any>;
    const stream = new ReadableStream({ start(c) { ctrl = c; } });
    if (init && init.signal) {
      if (init.signal.aborted) {
        ctrl!.error(init.signal.reason);
      } else {
        init.signal.addEventListener('abort', () => ctrl.error(init.signal.reason), { once: true });
      }
    }`
			if !tc.abort {
				streamSetup += `
    setTimeout(() => { ctrl.enqueue(new TextEncoder().encode('{"ok":true}')); ctrl.close(); }, 20);`
			}

			driver := `
import { HTTPClient } from './fetch';

async function main() {
  ` + forceLine + `

  const client = new HTTPClient('http://example.invalid', 5000); // long per-attempt timeout: isolate the caller abort as the cause

  (globalThis as any).fetch = async (_url: string, init: any) => {
    ` + streamSetup + `
    return new Response(stream, { status: 200, headers: { 'content-type': 'application/json' } });
  };

  const controller = new AbortController();
  ` + abortSetup + `

  const start = Date.now();
  const watchdog = setTimeout(() => {
    console.log(JSON.stringify({ outcome: 'STILL-HANGING', elapsedMs: Date.now() - start }));
    process.exit(0);
  }, 1200);

  try {
    await client.request<any>({ method: 'GET', url: '/slow-body', signal: controller.signal });
    clearTimeout(watchdog);
    console.log(JSON.stringify({ outcome: 'resolved', elapsedMs: Date.now() - start }));
  } catch (err) {
    clearTimeout(watchdog);
    console.log(JSON.stringify({
      outcome: 'rejected',
      name: err instanceof Error ? err.name : typeof err,
      elapsedMs: Date.now() - start,
    }));
  }
}

main().catch((err) => { console.error(err); process.exit(1); });
`
			driverFile := "src/__driver_3b_" + tc.name + ".ts"
			writeTree(t, dir, map[string]string{driverFile: driver})

			stdout := runNodeDriver(t, dir, driverFile)

			var result struct {
				Outcome   string `json:"outcome"`
				Name      string `json:"name"`
				ElapsedMs int    `json:"elapsedMs"`
			}
			decodeLastLine(t, stdout, &result)

			assert.NotEqual(t, "STILL-HANGING", result.Outcome,
				"measured before the fix (manual fallback): STILL-HANGING-after-1200ms (combined.dispose() removes the forwarding listeners as soon as headers arrive, so a caller abort during the body read never reaches the merged controller) — elapsedMs=%d", result.ElapsedMs)
			assert.Equal(t, tc.wantResult, result.Outcome)

			if tc.abort {
				assert.Less(t, result.ElapsedMs, 500, "a caller abort 50ms into the body read must reject promptly, not hang")
			}
		})
	}
}

// TestInterceptorHeaderMutationDoesNotCompoundAcrossRetries is the runtime
// proof for defect 2: executeRequest's `let requestConfig = { ...config }`
// is a shallow copy, so requestConfig.headers === config.headers. A request
// interceptor that mutates config.headers in place — a legitimate reading of
// onRequest(config): RequestConfig — compounded its mutation on every retry
// attempt, since each attempt re-spread the same already-mutated headers
// object.
//
// Measured BEFORE the fix, across a 503 -> 503 -> 200 retry, an
// interceptor appending '>hop' to x-trace on each attempt produced
// "t>hop", "t>hop>hop", "t>hop>hop>hop" on the three outgoing requests.
// After the fix each attempt must independently see "t>hop".
func TestInterceptorHeaderMutationDoesNotCompoundAcrossRetries(t *testing.T) {
	dir := writeFetchOnly(t)

	driver := `
import { HTTPClient } from './fetch';

async function main() {
  const client = new HTTPClient('http://example.invalid', 5000);

  client.addRequestInterceptor({
    onRequest(config) {
      config.headers = config.headers || {};
      config.headers['x-trace'] = (config.headers['x-trace'] || 't') + '>hop';
      return config;
    },
  });

  let calls = 0;
  const seenTraces: string[] = [];
  (globalThis as any).fetch = async (_url: string, init: any) => {
    calls++;
    seenTraces.push(init.headers['x-trace']);
    if (calls < 3) {
      return new Response(null, { status: 503 });
    }
    return new Response(JSON.stringify({ ok: true }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  };

  const result = await client.request<any>({
    method: 'GET',
    url: '/x',
    headers: { 'x-trace': 't' },
    retry: { maxAttempts: 3, delay: 5, maxDelay: 20, retryableStatusCodes: [503] },
  });

  console.log(JSON.stringify({ seenTraces, calls, result }));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_2.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_2.ts")

	var result struct {
		SeenTraces []string       `json:"seenTraces"`
		Calls      int            `json:"calls"`
		Result     map[string]any `json:"result"`
	}
	decodeLastLine(t, stdout, &result)

	require.Len(t, result.SeenTraces, 3, "driver stdout:\n%s", stdout)
	assert.Equal(t, []string{"t>hop", "t>hop", "t>hop"}, result.SeenTraces,
		"measured before the fix: [\"t>hop\", \"t>hop>hop\", \"t>hop>hop>hop\"] — a shallow config copy let the interceptor's in-place header mutation compound across retries")

	// Positive control: the retry flow itself must still work.
	assert.Equal(t, 3, result.Calls)
	assert.Equal(t, true, result.Result["ok"])
}

// TestBackoffSleepAbortsPromptly is the runtime proof for defect 3c: the
// backoff sleep (`await new Promise(resolve => setTimeout(resolve, delay))`)
// ignored the caller's signal entirely, so aborting partway into a backoff
// still waited out the full delay. With production defaults (1s/2s/4s) a
// caller could wait seconds past their own abort.
//
// Measured BEFORE the fix: aborting 20ms into a 300ms backoff still took
// ~300ms to reject. After the fix it must reject in roughly 20ms.
func TestBackoffSleepAbortsPromptly(t *testing.T) {
	dir := writeFetchOnly(t)

	t.Run("abort-mid-backoff", func(t *testing.T) {
		driver := `
import { HTTPClient } from './fetch';

async function main() {
  const client = new HTTPClient('http://example.invalid', 5000);

  (globalThis as any).fetch = async () => new Response(null, { status: 503 });

  const controller = new AbortController();
  setTimeout(() => controller.abort(), 20);

  const start = Date.now();
  try {
    await client.request<any>({
      method: 'GET',
      url: '/x',
      signal: controller.signal,
      retry: { maxAttempts: 5, delay: 300, maxDelay: 300, retryableStatusCodes: [503] },
    });
    console.log(JSON.stringify({ outcome: 'resolved', elapsedMs: Date.now() - start }));
  } catch (err) {
    console.log(JSON.stringify({
      outcome: 'rejected',
      name: err instanceof Error ? err.name : typeof err,
      elapsedMs: Date.now() - start,
    }));
  }
}

main().catch((err) => { console.error(err); process.exit(1); });
`
		writeTree(t, dir, map[string]string{"src/__driver_3c_abort.ts": driver})

		stdout := runNodeDriver(t, dir, "src/__driver_3c_abort.ts")

		var result struct {
			Outcome   string `json:"outcome"`
			Name      string `json:"name"`
			ElapsedMs int    `json:"elapsedMs"`
		}
		decodeLastLine(t, stdout, &result)

		assert.Equal(t, "rejected", result.Outcome)
		assert.Less(t, result.ElapsedMs, 150,
			"measured before the fix: aborting 20ms into a 300ms backoff still took ~300ms to reject; elapsedMs=%d", result.ElapsedMs)
	})

	// Positive control: without an abort, the retry/backoff flow itself must
	// still work end to end.
	t.Run("no-abort-retry-still-succeeds", func(t *testing.T) {
		driver := `
import { HTTPClient } from './fetch';

async function main() {
  const client = new HTTPClient('http://example.invalid', 5000);

  let calls = 0;
  (globalThis as any).fetch = async () => {
    calls++;
    if (calls < 3) return new Response(null, { status: 503 });
    return new Response(JSON.stringify({ ok: true }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
  };

  const result = await client.request<any>({
    method: 'GET',
    url: '/x',
    retry: { maxAttempts: 3, delay: 10, maxDelay: 20, retryableStatusCodes: [503] },
  });

  console.log(JSON.stringify({ outcome: 'resolved', calls, result }));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
		writeTree(t, dir, map[string]string{"src/__driver_3c_control.ts": driver})

		stdout := runNodeDriver(t, dir, "src/__driver_3c_control.ts")

		var result struct {
			Outcome string         `json:"outcome"`
			Calls   int            `json:"calls"`
			Result  map[string]any `json:"result"`
		}
		decodeLastLine(t, stdout, &result)

		assert.Equal(t, "resolved", result.Outcome)
		assert.Equal(t, 3, result.Calls)
		assert.Equal(t, true, result.Result["ok"])
	})
}

// TestFallbackAbortListenersDoNotLeakAcrossManyRequests re-verifies, after
// this task's teardown restructuring, a guarantee an earlier task proved: on
// the combineSignals manual fallback, 500 sequential requests reusing one
// long-lived caller AbortController leave 0 'abort' listeners on that
// controller's signal once they've all settled. Moving the teardown later
// (to cover the body read, per defects 3a/3b) must not reintroduce the
// listener leak that earlier fix closed.
func TestFallbackAbortListenersDoNotLeakAcrossManyRequests(t *testing.T) {
	dir := writeFetchOnly(t)

	driver := `
import { HTTPClient } from './fetch';
import { getEventListeners } from 'node:events';

async function main() {
  delete (AbortSignal as any).any; // force the manual combineSignals fallback

  const client = new HTTPClient('http://example.invalid', 5000);
  (globalThis as any).fetch = async () => new Response(JSON.stringify({ ok: true }), {
    status: 200,
    headers: { 'content-type': 'application/json' },
  });

  // One long-lived controller reused by every request, never aborted —
  // exactly the shape that leaks if dispose() isn't called on every
  // non-aborted request too.
  const controller = new AbortController();

  const before = getEventListeners(controller.signal, 'abort').length;

  for (let i = 0; i < 500; i++) {
    await client.request<any>({ method: 'GET', url: '/x', signal: controller.signal });
  }

  const after = getEventListeners(controller.signal, 'abort').length;

  console.log(JSON.stringify({ before, after }));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_leak.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_leak.ts")

	var result struct {
		Before int `json:"before"`
		After  int `json:"after"`
	}
	decodeLastLine(t, stdout, &result)

	assert.Equal(t, 0, result.Before)
	assert.Equal(t, 0, result.After,
		"500 sequential requests reusing one caller AbortController must leave 0 'abort' listeners on the manual fallback path")
}

// TestRequestBodySerializationByRuntimeType is task 8's runtime proof that
// executeRequest stopped unconditionally JSON.stringify-ing every body and
// stopped forcing 'Content-Type: application/json' on every request. Before
// this fix, `body: requestConfig.body ? JSON.stringify(requestConfig.body) :
// undefined` flattened a FormData/Blob body to the literal string "{}" (both
// have no own enumerable properties), silently sending an empty payload to a
// server expecting a multipart upload or raw bytes — exactly the class of
// defect this task exists to fix, one level below rest.go's generated
// signatures. This drives HTTPClient.request() directly (bypassing rest.ts)
// so it can exercise runtime body types no single generated method's
// declared parameter type could ever mix into one call.
func TestRequestBodySerializationByRuntimeType(t *testing.T) {
	dir := writeFetchOnly(t)

	driver := `
import { HTTPClient } from './fetch';

function hasContentTypeHeader(headers: any): boolean {
  if (!headers) return false;
  return Object.keys(headers).some((k) => k.toLowerCase() === 'content-type');
}

async function main() {
  const client = new HTTPClient('http://example.invalid', 5000);
  const results: Record<string, any> = {};

  // 1. A plain object body is JSON.stringify'd and gets the JSON Content-Type.
  {
    let captured: any;
    (globalThis as any).fetch = async (_url: string, init: any) => {
      captured = init;
      return new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'content-type': 'application/json' } });
    };
    await client.request<any>({ method: 'POST', url: '/json', body: { a: 1 } });
    results.json = {
      bodyIsString: typeof captured.body === 'string',
      bodyValue: captured.body,
      contentType: captured.headers['Content-Type'],
    };
  }

  // 2. A FormData body passes through by identity, with NO explicit
  //    Content-Type — the runtime computes the multipart boundary only when
  //    it sets the header itself; a caller- or default-supplied
  //    'multipart/form-data' (or JSON) Content-Type has no boundary and
  //    breaks the request server-side.
  {
    let captured: any;
    const fd = new FormData();
    fd.append('file', 'contents');
    (globalThis as any).fetch = async (_url: string, init: any) => {
      captured = init;
      return new Response(null, { status: 204 });
    };
    await client.request<any>({ method: 'POST', url: '/upload', body: fd, allowEmptyBody: true });
    results.formData = {
      sameReference: captured.body === fd,
      hasContentType: hasContentTypeHeader(captured.headers),
    };
  }

  // 3. A Blob body passes through by identity, with no forced JSON Content-Type.
  {
    let captured: any;
    const blob = new Blob(['raw bytes'], { type: 'application/octet-stream' });
    (globalThis as any).fetch = async (_url: string, init: any) => {
      captured = init;
      return new Response(null, { status: 204 });
    };
    await client.request<any>({ method: 'POST', url: '/raw', body: blob, allowEmptyBody: true });
    results.blob = {
      sameReference: captured.body === blob,
      hasContentType: hasContentTypeHeader(captured.headers),
    };
  }

  // 4. A string body (e.g. a text/plain endpoint) passes through as-is, not
  //    JSON.stringify'd (which would have wrapped it in quotes).
  {
    let captured: any;
    (globalThis as any).fetch = async (_url: string, init: any) => {
      captured = init;
      return new Response(null, { status: 204 });
    };
    await client.request<any>({ method: 'POST', url: '/note', body: 'hello world', allowEmptyBody: true });
    results.string = {
      bodyValue: captured.body,
      hasContentType: hasContentTypeHeader(captured.headers),
    };
  }

  console.log(JSON.stringify(results));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_body_types.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_body_types.ts")

	var result struct {
		JSON struct {
			BodyIsString bool   `json:"bodyIsString"`
			BodyValue    string `json:"bodyValue"`
			ContentType  string `json:"contentType"`
		} `json:"json"`
		FormData struct {
			SameReference  bool `json:"sameReference"`
			HasContentType bool `json:"hasContentType"`
		} `json:"formData"`
		Blob struct {
			SameReference  bool `json:"sameReference"`
			HasContentType bool `json:"hasContentType"`
		} `json:"blob"`
		String struct {
			BodyValue      string `json:"bodyValue"`
			HasContentType bool   `json:"hasContentType"`
		} `json:"string"`
	}
	decodeLastLine(t, stdout, &result)

	assert.True(t, result.JSON.BodyIsString, "driver stdout:\n%s", stdout)
	assert.Equal(t, `{"a":1}`, result.JSON.BodyValue)
	assert.Equal(t, "application/json", result.JSON.ContentType)

	assert.True(t, result.FormData.SameReference, "FormData body must pass through by identity, not be re-wrapped or JSON-flattened")
	assert.False(t, result.FormData.HasContentType, "FormData body must not get an explicit Content-Type header — the runtime computes the multipart boundary only when it sets the header itself")

	assert.True(t, result.Blob.SameReference, "Blob body must pass through by identity")
	assert.False(t, result.Blob.HasContentType, "Blob body must not be forced to a JSON Content-Type")

	assert.Equal(t, "hello world", result.String.BodyValue, "a string body must pass through as-is, not be JSON.stringify'd (which would add surrounding quotes)")
	assert.False(t, result.String.HasContentType, "a bare string body must not be forced to a JSON Content-Type")
}

// TestExplicitContentTypeHeaderIsNotOverridden asserts that a caller-supplied
// (or endpoint-declared-header-parameter-supplied) Content-Type always wins
// over executeRequest's own JSON-body default. An endpoint can declare an
// explicit Content-Type header parameter alongside a JSON-shaped body (e.g.
// 'application/vnd.custom+json'); the runtime default must not clobber it.
func TestExplicitContentTypeHeaderIsNotOverridden(t *testing.T) {
	dir := writeFetchOnly(t)

	driver := `
import { HTTPClient } from './fetch';

async function main() {
  const client = new HTTPClient('http://example.invalid', 5000);
  let captured: any;
  (globalThis as any).fetch = async (_url: string, init: any) => {
    captured = init;
    return new Response(null, { status: 204 });
  };

  await client.request<any>({
    method: 'POST',
    url: '/custom',
    body: { a: 1 },
    headers: { 'Content-Type': 'application/vnd.custom+json' },
    allowEmptyBody: true,
  });

  console.log(JSON.stringify({ contentType: captured.headers['Content-Type'] }));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_ct_override.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_ct_override.ts")

	var result struct {
		ContentType string `json:"contentType"`
	}
	decodeLastLine(t, stdout, &result)

	assert.Equal(t, "application/vnd.custom+json", result.ContentType,
		"an explicit Content-Type header (e.g. from a declared header parameter) must win over the JSON-body default; driver stdout:\n%s", stdout)
}

// TestRetryResendsSameFormDataBodyByIdentity is the retry-safety half of task
// 8's KNOWN HAZARD: FormData is re-readable (unlike a ReadableStream), so a
// retried request must resend the exact same FormData object on every
// attempt — not re-serialize, clone, or drop it.
func TestRetryResendsSameFormDataBodyByIdentity(t *testing.T) {
	dir := writeFetchOnly(t)

	driver := `
import { HTTPClient } from './fetch';

async function main() {
  const client = new HTTPClient('http://example.invalid', 5000);
  const fd = new FormData();
  fd.append('file', 'contents');

  let calls = 0;
  const seenBodies: boolean[] = [];
  (globalThis as any).fetch = async (_url: string, init: any) => {
    calls++;
    seenBodies.push(init.body === fd);
    if (calls < 2) {
      return new Response(null, { status: 503 });
    }
    return new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'content-type': 'application/json' } });
  };

  const result = await client.request<any>({
    method: 'POST',
    url: '/upload',
    body: fd,
    retry: { maxAttempts: 3, delay: 5, maxDelay: 20, retryableStatusCodes: [503] },
  });

  console.log(JSON.stringify({ calls, seenBodies, result }));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_retry_formdata.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_retry_formdata.ts")

	var result struct {
		Calls      int            `json:"calls"`
		SeenBodies []bool         `json:"seenBodies"`
		Result     map[string]any `json:"result"`
	}
	decodeLastLine(t, stdout, &result)

	assert.Equal(t, 2, result.Calls, "driver stdout:\n%s", stdout)
	assert.Equal(t, []bool{true, true}, result.SeenBodies,
		"a retried FormData body must be the exact same object on every attempt — FormData is re-readable, so no re-serialization or cloning should occur")
	assert.Equal(t, true, result.Result["ok"])
}

// TestStreamBodyDisablesRetry is the stream-safety half of task 8's KNOWN
// HAZARD: once native bodies pass through unmodified, a ReadableStream body
// is one-shot — it cannot be re-sent on a second attempt. The chosen policy
// is to refuse to retry at all when the body is a stream: the retry loop's
// effective maxAttempts is capped to 1, so a failure is thrown immediately
// and loudly on the first attempt rather than being silently retried with a
// disturbed (or, in some runtimes, empty) stream.
func TestStreamBodyDisablesRetry(t *testing.T) {
	dir := writeFetchOnly(t)

	driver := `
import { HTTPClient } from './fetch';

async function main() {
  const client = new HTTPClient('http://example.invalid', 5000);

  const stream = new ReadableStream({
    start(controller) {
      controller.enqueue(new TextEncoder().encode('chunk'));
      controller.close();
    },
  });

  let calls = 0;
  (globalThis as any).fetch = async (_url: string, _init: any) => {
    calls++;
    return new Response(null, { status: 503 });
  };

  let outcome: string;
  try {
    await client.request<any>({
      method: 'POST',
      url: '/stream',
      body: stream,
      retry: { maxAttempts: 5, delay: 5, maxDelay: 20, retryableStatusCodes: [503] },
    });
    outcome = 'resolved';
  } catch (err) {
    outcome = 'rejected:' + (err instanceof Error ? err.name : typeof err);
  }

  console.log(JSON.stringify({ calls, outcome }));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_stream_no_retry.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_stream_no_retry.ts")

	var result struct {
		Calls   int    `json:"calls"`
		Outcome string `json:"outcome"`
	}
	decodeLastLine(t, stdout, &result)

	assert.Equal(t, 1, result.Calls,
		"a ReadableStream body must never be retried — it is one-shot and a second fetch() attempt would send a disturbed/empty stream, not the original data; driver stdout:\n%s", stdout)
	assert.Contains(t, result.Outcome, "rejected",
		"a failed request with a stream body must fail loudly on the first attempt, not hang or silently succeed")
}

// TestNativeBodyInitTypesPassThroughAcrossRealms covers the two gaps review
// found in task 8's first cut: the runtime's BodyInit enumeration was
// incomplete (URLSearchParams, ArrayBuffer and TypedArray fell through to
// JSON.stringify and went out as "{}" or an index-keyed object, with a wrong
// application/json Content-Type), and it dispatched with `instanceof`, which
// is realm-bound — a FormData or ReadableStream from another realm (an
// iframe, a polyfill, a bundler-substituted global) is not an instance of
// THIS realm's constructor and was silently JSON.stringify-ed too.
//
// The cross-realm cases are simulated the way they actually bite: the value
// is real and fully functional, but globalThis's constructor is a different
// object, so `x instanceof globalThis.FormData` is false.
func TestNativeBodyInitTypesPassThroughAcrossRealms(t *testing.T) {
	dir := writeFetchOnly(t)

	driver := `
import { HTTPClient } from './fetch';

function contentTypeOf(headers: any): string | null {
  if (!headers) return null;
  const k = Object.keys(headers).find((h) => h.toLowerCase() === 'content-type');
  return k ? headers[k] : null;
}

async function send(client: any, body: any) {
  let captured: any;
  (globalThis as any).fetch = async (_url: string, init: any) => {
    captured = init;
    return new Response(null, { status: 204 });
  };
  await client.request({ method: 'POST', url: '/x', body, allowEmptyBody: true });
  return captured;
}

async function main() {
  const client = new HTTPClient('http://example.invalid', 5000);
  const results: Record<string, any> = {};

  // URLSearchParams: a native BodyInit fetch form-urlencodes itself. It has
  // no own enumerable properties, so JSON.stringify flattens it to "{}".
  {
    const usp = new URLSearchParams({ a: '1', b: '2' });
    const c = await send(client, usp);
    results.urlSearchParams = { sameReference: c.body === usp, contentType: contentTypeOf(c.headers) };
  }

  // Uint8Array: JSON.stringify turns it into {"0":1,"1":2,...}.
  {
    const bytes = new Uint8Array([1, 2, 3, 255]);
    const c = await send(client, bytes);
    results.typedArray = { sameReference: c.body === bytes, contentType: contentTypeOf(c.headers) };
  }

  // ArrayBuffer: JSON.stringify flattens it to "{}".
  {
    const buf = new ArrayBuffer(8);
    const c = await send(client, buf);
    results.arrayBuffer = { sameReference: c.body === buf, contentType: contentTypeOf(c.headers) };
  }

  // Cross-realm FormData: real and functional, but globalThis.FormData is a
  // different constructor, so instanceof is false.
  {
    const fd = new FormData();
    fd.append('file', 'contents');
    const RealFormData = globalThis.FormData;
    (globalThis as any).FormData = class Decoy {};
    try {
      const c = await send(client, fd);
      results.crossRealmFormData = {
        sameReference: c.body === fd,
        contentType: contentTypeOf(c.headers),
        instanceofWouldHaveMissed: !(fd instanceof (globalThis as any).FormData),
      };
    } finally {
      (globalThis as any).FormData = RealFormData;
    }
  }

  // Cross-realm ReadableStream: must pass through AND still suppress retries.
  {
    const stream = new ReadableStream({ start(c) { c.enqueue(new TextEncoder().encode('x')); c.close(); } });
    const RealRS = globalThis.ReadableStream;
    (globalThis as any).ReadableStream = class Decoy {};
    let calls = 0;
    const warnings: string[] = [];
    const realWarn = console.warn;
    console.warn = (...args: any[]) => { warnings.push(args.join(' ')); };
    try {
      (globalThis as any).fetch = async (_url: string, init: any) => {
        calls++;
        return new Response('nope', { status: 503 });
      };
      let threw = '';
      try {
        await client.request({ method: 'POST', url: '/stream', body: stream, retry: { maxAttempts: 5 } });
      } catch (err: any) {
        threw = err?.constructor?.name ?? 'unknown';
      }
      results.crossRealmStream = { calls, threw, warned: warnings.length > 0 };
    } finally {
      (globalThis as any).ReadableStream = RealRS;
      console.warn = realWarn;
    }
  }

  // Positive control: a plain object still becomes JSON with a JSON type.
  {
    const c = await send(client, { a: 1 });
    results.plainObject = { body: c.body, contentType: contentTypeOf(c.headers) };
  }

  console.log(JSON.stringify(results));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_bodyinit.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_bodyinit.ts")

	var result struct {
		URLSearchParams struct {
			SameReference bool    `json:"sameReference"`
			ContentType   *string `json:"contentType"`
		} `json:"urlSearchParams"`
		TypedArray struct {
			SameReference bool    `json:"sameReference"`
			ContentType   *string `json:"contentType"`
		} `json:"typedArray"`
		ArrayBuffer struct {
			SameReference bool    `json:"sameReference"`
			ContentType   *string `json:"contentType"`
		} `json:"arrayBuffer"`
		CrossRealmFormData struct {
			SameReference             bool    `json:"sameReference"`
			ContentType               *string `json:"contentType"`
			InstanceofWouldHaveMissed bool    `json:"instanceofWouldHaveMissed"`
		} `json:"crossRealmFormData"`
		CrossRealmStream struct {
			Calls  int    `json:"calls"`
			Threw  string `json:"threw"`
			Warned bool   `json:"warned"`
		} `json:"crossRealmStream"`
		PlainObject struct {
			Body        string  `json:"body"`
			ContentType *string `json:"contentType"`
		} `json:"plainObject"`
	}
	decodeLastLine(t, stdout, &result)

	assert.True(t, result.URLSearchParams.SameReference,
		"a URLSearchParams body must pass through by identity, not be JSON.stringify-ed to \"{}\"; driver stdout:\n%s", stdout)
	assert.Nil(t, result.URLSearchParams.ContentType,
		"fetch sets application/x-www-form-urlencoded for URLSearchParams itself; the client must not force a JSON Content-Type")

	assert.True(t, result.TypedArray.SameReference,
		"a Uint8Array body must pass through by identity, not be stringified index-by-index; driver stdout:\n%s", stdout)
	assert.Nil(t, result.TypedArray.ContentType, "a binary body must not get a JSON Content-Type")

	assert.True(t, result.ArrayBuffer.SameReference,
		"an ArrayBuffer body must pass through by identity, not be flattened to \"{}\"; driver stdout:\n%s", stdout)
	assert.Nil(t, result.ArrayBuffer.ContentType, "a binary body must not get a JSON Content-Type")

	assert.True(t, result.CrossRealmFormData.InstanceofWouldHaveMissed,
		"test setup is wrong: the decoy constructor should make instanceof false")
	assert.True(t, result.CrossRealmFormData.SameReference,
		"a cross-realm FormData must still pass through by identity — instanceof is realm-bound; driver stdout:\n%s", stdout)
	assert.Nil(t, result.CrossRealmFormData.ContentType,
		"a cross-realm FormData must still get no Content-Type, so the runtime supplies the multipart boundary")

	assert.Equal(t, 1, result.CrossRealmStream.Calls,
		"a cross-realm ReadableStream body must still suppress retries — it is just as one-shot; driver stdout:\n%s", stdout)
	assert.True(t, result.CrossRealmStream.Warned,
		"capping a caller's explicit maxAttempts:5 down to 1 must warn, or the caller has no way to discover it")

	assert.Equal(t, `{"a":1}`, result.PlainObject.Body, "positive control: a plain object must still be JSON-serialized")
	if assert.NotNil(t, result.PlainObject.ContentType) {
		assert.Equal(t, "application/json", *result.PlainObject.ContentType)
	}
}

// TestExecuteRequestEncodesBodyViaBodyCodec is the runtime proof for task 6's
// core behaviour: a JSON request body carrying client-side (camelCase) field
// names must be renamed to their wire (snake_case) names before
// JSON.stringify, when RequestConfig.bodyCodec names a schema in the codec
// table. baseSpec()'s "User" schema declares "user_id", which tsFieldName
// (under the default TypeScript camel-case strategy) renames to "userId" —
// exactly the case the task brief specifies.
func TestExecuteRequestEncodesBodyViaBodyCodec(t *testing.T) {
	dir := writeFetchOnly(t)

	driver := `
import { HTTPClient } from './fetch';

async function main() {
  const client = new HTTPClient('http://example.invalid', 5000);
  let captured: any;
  (globalThis as any).fetch = async (_url: string, init: any) => {
    captured = init;
    return new Response(null, { status: 204 });
  };

  await client.request<any>({
    method: 'POST',
    url: '/users',
    body: { userId: 'x' },
    bodyCodec: 'User',
    allowEmptyBody: true,
  });

  console.log(JSON.stringify({ wireBody: captured.body }));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_codec_encode.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_codec_encode.ts")

	var result struct {
		WireBody string `json:"wireBody"`
	}
	decodeLastLine(t, stdout, &result)

	assert.Equal(t, `{"user_id":"x"}`, result.WireBody,
		"a request body with bodyCodec:'User' must put the wire-cased {\"user_id\":\"x\"} on the wire; driver stdout:\n%s", stdout)
}

// TestExecuteRequestDecodesResponseViaResponseCodec is the decode-direction
// counterpart: a JSON response carrying wire (snake_case) field names must be
// renamed to their client-side (camelCase) names when RequestConfig.
// responseCodec names a schema in the codec table.
func TestExecuteRequestDecodesResponseViaResponseCodec(t *testing.T) {
	dir := writeFetchOnly(t)

	driver := `
import { HTTPClient } from './fetch';

async function main() {
  const client = new HTTPClient('http://example.invalid', 5000);
  (globalThis as any).fetch = async () => new Response(JSON.stringify({ user_id: 'x' }), {
    status: 200,
    headers: { 'content-type': 'application/json' },
  });

  const result = await client.request<any>({ method: 'GET', url: '/users/x', responseCodec: 'User' });

  console.log(JSON.stringify({ result }));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_codec_decode.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_codec_decode.ts")

	var result struct {
		Result map[string]any `json:"result"`
	}
	decodeLastLine(t, stdout, &result)

	assert.Equal(t, "x", result.Result["userId"],
		"a response of {\"user_id\":\"x\"} with responseCodec:'User' must resolve to {userId:'x'}; driver stdout:\n%s", stdout)
	_, hasWireKey := result.Result["user_id"]
	assert.False(t, hasWireKey, "the wire key must be renamed away, not left alongside the renamed one")
}

// TestExecuteRequestWithoutCodecRefsPassesThroughUntouched proves the
// negative case: when a call site sets neither bodyCodec nor responseCodec (a
// request against a schema the codec table doesn't cover, or a caller that
// never opted in), the body and response are passed through exactly as they
// were before this task — no renaming, in either direction.
func TestExecuteRequestWithoutCodecRefsPassesThroughUntouched(t *testing.T) {
	dir := writeFetchOnly(t)

	driver := `
import { HTTPClient } from './fetch';

async function main() {
  const client = new HTTPClient('http://example.invalid', 5000);
  const results: Record<string, any> = {};

  {
    let captured: any;
    (globalThis as any).fetch = async (_url: string, init: any) => {
      captured = init;
      return new Response(null, { status: 204 });
    };
    await client.request<any>({ method: 'POST', url: '/users', body: { userId: 'x' }, allowEmptyBody: true });
    results.wireBody = captured.body;
  }

  {
    (globalThis as any).fetch = async () => new Response(JSON.stringify({ user_id: 'x' }), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    });
    results.response = await client.request<any>({ method: 'GET', url: '/users/x' });
  }

  console.log(JSON.stringify(results));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_codec_none.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_codec_none.ts")

	var result struct {
		WireBody string         `json:"wireBody"`
		Response map[string]any `json:"response"`
	}
	decodeLastLine(t, stdout, &result)

	assert.Equal(t, `{"userId":"x"}`, result.WireBody,
		"no bodyCodec means no renaming: the body must be serialized exactly as the caller wrote it; driver stdout:\n%s", stdout)
	assert.Equal(t, "x", result.Response["user_id"],
		"no responseCodec means no renaming: the response must resolve exactly as parsed; driver stdout:\n%s", stdout)
}

// TestBodyCodecNeverAppliesToNativeBodyInitTypes is the runtime proof for
// hazard 1 in the task brief: encode() must never walk a FormData, Blob,
// URLSearchParams, ReadableStream, ArrayBuffer, or TypedArray body, even when
// bodyCodec is set on the same RequestConfig — those native BodyInit shapes
// are dispatched to fetch() by reference in executeRequest, before the
// JSON-only branch that calls encode() is ever reached. If encode() were
// mistakenly invoked on one of these, `walk`'s 'object' case would call
// Object.entries on it (a FormData/URLSearchParams instance has no own
// enumerable properties) and return a brand-new plain object instead of the
// original reference — so a same-reference assertion catches a misplaced
// encode() call even though none of these instances have a "user_id"/"userId"
// field for a rename to visibly corrupt.
func TestBodyCodecNeverAppliesToNativeBodyInitTypes(t *testing.T) {
	dir := writeFetchOnly(t)

	driver := `
import { HTTPClient } from './fetch';

async function send(client: any, body: any) {
  let captured: any;
  (globalThis as any).fetch = async (_url: string, init: any) => {
    captured = init;
    return new Response(null, { status: 204 });
  };
  await client.request({ method: 'POST', url: '/x', body, bodyCodec: 'User', allowEmptyBody: true });
  return captured;
}

async function main() {
  const client = new HTTPClient('http://example.invalid', 5000);
  const results: Record<string, any> = {};

  {
    const fd = new FormData();
    fd.append('user_id', 'x');
    const c = await send(client, fd);
    results.formData = { sameReference: c.body === fd };
  }

  {
    const blob = new Blob(['{"user_id":"x"}'], { type: 'application/json' });
    const c = await send(client, blob);
    results.blob = { sameReference: c.body === blob };
  }

  {
    const usp = new URLSearchParams({ user_id: 'x' });
    const c = await send(client, usp);
    results.urlSearchParams = { sameReference: c.body === usp };
  }

  {
    const bytes = new Uint8Array([1, 2, 3]);
    const c = await send(client, bytes);
    results.typedArray = { sameReference: c.body === bytes };
  }

  {
    const buf = new ArrayBuffer(4);
    const c = await send(client, buf);
    results.arrayBuffer = { sameReference: c.body === buf };
  }

  {
    const stream = new ReadableStream({ start(ctrl) { ctrl.enqueue(new TextEncoder().encode('x')); ctrl.close(); } });
    const c = await send(client, stream);
    results.stream = { sameReference: c.body === stream };
  }

  console.log(JSON.stringify(results));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_codec_bodyinit.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_codec_bodyinit.ts")

	var result struct {
		FormData        struct{ SameReference bool } `json:"formData"`
		Blob            struct{ SameReference bool } `json:"blob"`
		URLSearchParams struct{ SameReference bool } `json:"urlSearchParams"`
		TypedArray      struct{ SameReference bool } `json:"typedArray"`
		ArrayBuffer     struct{ SameReference bool } `json:"arrayBuffer"`
		Stream          struct{ SameReference bool } `json:"stream"`
	}
	decodeLastLine(t, stdout, &result)

	assert.True(t, result.FormData.SameReference, "bodyCodec:'User' must not cause encode() to walk a FormData body; driver stdout:\n%s", stdout)
	assert.True(t, result.Blob.SameReference, "bodyCodec:'User' must not cause encode() to walk a Blob body; driver stdout:\n%s", stdout)
	assert.True(t, result.URLSearchParams.SameReference, "bodyCodec:'User' must not cause encode() to walk a URLSearchParams body; driver stdout:\n%s", stdout)
	assert.True(t, result.TypedArray.SameReference, "bodyCodec:'User' must not cause encode() to walk a TypedArray body; driver stdout:\n%s", stdout)
	assert.True(t, result.ArrayBuffer.SameReference, "bodyCodec:'User' must not cause encode() to walk an ArrayBuffer body; driver stdout:\n%s", stdout)
	assert.True(t, result.Stream.SameReference, "bodyCodec:'User' must not cause encode() to walk a ReadableStream body; driver stdout:\n%s", stdout)
}

// TestResponseCodecNeverAppliesToNonJSONResponses is the runtime proof for
// hazard 2 in the task brief: decode() must never run for a response
// executeRequest resolves as void/undefined, a Blob, or a text/* string, even
// when responseCodec is set on the same RequestConfig.
func TestResponseCodecNeverAppliesToNonJSONResponses(t *testing.T) {
	dir := writeFetchOnly(t)

	driver := `
import { HTTPClient } from './fetch';

async function main() {
  const client = new HTTPClient('http://example.invalid', 5000);
  const results: Record<string, any> = {};

  // 204: the unconditional no-body status path.
  {
    (globalThis as any).fetch = async () => new Response(null, { status: 204 });
    const r = await client.request<any>({ method: 'GET', url: '/x', responseCodec: 'User' });
    results.status204 = r === undefined ? 'undefined' : typeof r;
  }

  // Empty 202 with allowEmptyBody: the spec-gated empty-to-undefined path.
  {
    (globalThis as any).fetch = async () => new Response(null, { status: 202 });
    const r = await client.request<any>({ method: 'GET', url: '/x', responseCodec: 'User', allowEmptyBody: true });
    results.empty202 = r === undefined ? 'undefined' : typeof r;
  }

  // text/plain: a raw string response.
  {
    (globalThis as any).fetch = async () => new Response('user_id=raw-text', {
      status: 200,
      headers: { 'content-type': 'text/plain' },
    });
    results.textPlain = await client.request<any>({ method: 'GET', url: '/x', responseCodec: 'User' });
  }

  // application/octet-stream: a Blob response.
  {
    (globalThis as any).fetch = async () => new Response(new Blob(['bytes']), {
      status: 200,
      headers: { 'content-type': 'application/octet-stream' },
    });
    const r = await client.request<any>({ method: 'GET', url: '/x', responseCodec: 'User' });
    results.octetStream = { isBlob: typeof Blob !== 'undefined' && r instanceof Blob, size: r.size };
  }

  console.log(JSON.stringify(results));
}

main().catch((err) => { console.error(err); process.exit(1); });
`
	writeTree(t, dir, map[string]string{"src/__driver_codec_nonjson.ts": driver})

	stdout := runNodeDriver(t, dir, "src/__driver_codec_nonjson.ts")

	var result struct {
		Status204   string `json:"status204"`
		Empty202    string `json:"empty202"`
		TextPlain   string `json:"textPlain"`
		OctetStream struct {
			IsBlob bool `json:"isBlob"`
			Size   int  `json:"size"`
		} `json:"octetStream"`
	}
	decodeLastLine(t, stdout, &result)

	assert.Equal(t, "undefined", result.Status204, "responseCodec:'User' must not affect the 204 no-body path; driver stdout:\n%s", stdout)
	assert.Equal(t, "undefined", result.Empty202, "responseCodec:'User' must not affect the allowEmptyBody empty-202 path; driver stdout:\n%s", stdout)
	assert.Equal(t, "user_id=raw-text", result.TextPlain, "responseCodec:'User' must not walk a text/plain response; driver stdout:\n%s", stdout)
	assert.True(t, result.OctetStream.IsBlob, "responseCodec:'User' must not walk an application/octet-stream response; driver stdout:\n%s", stdout)
}
