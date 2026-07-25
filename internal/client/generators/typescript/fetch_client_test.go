package typescript

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFetchOnly generates just src/fetch.ts (self-contained: HTTPClient has
// no imports of its own) into a fresh temp dir, for tests that only exercise
// HTTPClient's timeout/abort/retry machinery and don't need the rest of the
// generated tree (rest.ts, types.ts, etc).
func writeFetchOnly(t *testing.T) string {
	t.Helper()

	code := NewFetchClientGenerator().GenerateBaseClient(baseSpec(), baseConfig())
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"src/fetch.ts": code})

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
