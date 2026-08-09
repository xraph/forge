# Streaming Frame Decoder Follow-ups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the gaps left by `cb773c34` — make the default decoder read the streaming envelope, and make the cross-language contract test actually enforce what its comments claim.

**Architecture:** Two independent threads. The first reorders one expression in `decodeFrame` so the shipped default stops discarding streaming frames, which removes the requirement that every application opt in. The second replaces two hand-written literal lists in `frame_test.go` with values derived from the sources they claim to mirror — the Go constants by parsing the AST, the TypeScript set by reading the file — so drift on either side fails a test instead of passing one.

**Tech Stack:** TypeScript (vitest) in `packages/client-core`; Go 1.26 (stdlib `go/ast`, `go/parser`, `regexp`) in `extensions/streaming`.

## Global Constraints

- **Never add `Co-Authored-By` trailers** to any commit. No co-author trailers of any kind.
- All Go commands in `extensions/streaming` require `GOWORK=off`: `cd extensions/streaming && GOWORK=off go build ./...`
- `extensions/streaming` is its **own Go module** (`extensions/streaming/go.mod`, `go 1.26.0`). It can be consumed standalone, so a test may read a file outside the module only if it skips cleanly when that file is absent.
- `extensions/streaming/internal/streaming.go`, `manager.go` and `extension.go` are shared with a parallel workstream. Task 2 **reads** `internal/streaming.go` and must not modify it.
- The streaming package's test binary is intermittently broken by that parallel workstream's in-flight test files. If `go test ./` fails to compile in a file you did not touch, that is not your change — verify with `GOWORK=off go build ./...` (library only) and note it.
- Comments in this codebase document *why* a choice was made and what the alternative cost. Match that. Do not write comments that describe what the next line does.

## Preflight (not a task)

`npm run typecheck` in `packages/client-core` fails with `TS2688: Cannot find type definition file for 'node'` because `@types/node` is not installed. This predates all of this work. Fix once, before starting:

```bash
cd packages/client-core && npm install
```

Verify: `npm run typecheck` exits 0. If it still fails, the remaining tasks are unaffected — `npx tsc --noEmit -p tsconfig.json` covers `src/` and passes today.

## Decision this plan assumes

**Task 1 changes the behaviour of the shipped `decodeFrame`.** It is the highest-value item here and the only one a reviewer might reject outright, so it is Task 1 and nothing else depends on it — rejecting it leaves Tasks 2–5 intact.

The change is reordering `type ?? event ?? name` to `event ?? type ?? name`. Rationale:

- It is correct for **all three** documented envelope shapes rather than two. The plain Forge WebSocket shape (`{type: 'order.created', payload}`) carries no `event`, so `type` still wins. The SSE/AsyncAPI shape (`{event: 'order.created', data}`) already wanted `event` first. The streaming extension shape gets the domain name instead of the transport kind.
- The only server it breaks is one sending **both** `type` as a message name and `event` as something else. No test in the repo does this; the sole frame carrying both fields is `streaming.test.ts:199`, where both are empty strings.
- It does not make `forgeStreamingDecoder` redundant. That decoder still filters the extension's transport frames out of `onUnknown` and still owns `channelOf`. The reorder only stops the default from being catastrophically wrong.

**If you reject Task 1**, the alternative is to leave `decodeFrame` alone and have every application pass `decode: forgeStreamingDecoder()`, which the docs committed in `5463788c` already instruct. That is a valid end state; it just means the failure mode stays one forgotten line away.

---

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `packages/client-core/src/live.ts` | `decodeFrame` name resolution order + its doc comment | 1 |
| `packages/client-core/__tests__/streaming.test.ts` | Streaming envelope behaviour; one test currently pins the *old* default and must be rewritten | 1, 4 |
| `packages/client-core/src/streaming.ts` | `forgeStreamingDecoder` channel resolution | 4 |
| `extensions/streaming/frame_test.go` | The cross-language contract assertions | 2, 3 |
| `extensions/streaming/frame.go` | `NewEventMessage` godoc | 5 |

---

### Task 1: Make the default decoder read `event` first

**Files:**
- Modify: `packages/client-core/src/live.ts:217-240` (the `decodeFrame` doc comment and its `name` expression)
- Test: `packages/client-core/__tests__/streaming.test.ts:110-121` (rewrite the test that pins the old behaviour), plus one new case

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces: `decodeFrame` keeps its exported signature `FrameDecoder = (message: unknown) => DecodedFrame | undefined`. No call site changes.

- [ ] **Step 1: Rewrite the test that pins the old behaviour**

The existing test asserts the defect. Replace it — open `packages/client-core/__tests__/streaming.test.ts` and swap the whole `it('is unreadable by the default decoder, and drops the whole channel', ...)` block for these two:

```ts
  // What the reorder bought. `type` is the transport kind, so the old
  // `type ?? event` order named every frame on every channel `message`, no
  // manifest row is keyed on `message`, and the whole channel was discarded.
  // Reading `event` first is correct for this envelope and still correct for
  // the two shapes that carry no `event` at all.
  it('is now readable by the default decoder', async () => {
    const { cache, sockets, frames, unknown } = await connect(decodeFrame);

    sockets.last().deliver(frame('order.created', { id: 9, total: 5 }));
    frames.flush();

    expect(unknown).toEqual([]);
    expect(cache.store.getRecord('Order:9')?.data).toEqual({ id: 9, total: 5 });
  });

  // Why forgeStreamingDecoder still exists after the reorder. The default has
  // no notion of a reserved transport kind, so a presence frame reaches it as
  // the name `presence` and is reported -- once per (channel, message) in
  // development -- for a frame that is working exactly as designed.
  it('still reports the extension’s transport frames, which the streaming decoder does not', async () => {
    const { sockets, frames, unknown } = await connect(decodeFrame);

    sockets.last().deliver({ id: 'm', type: 'presence', user_id: 'u-1', data: null });
    frames.flush();

    expect(unknown).toEqual([{ message: 'presence', channel: '/ws/orders' }]);
  });
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd packages/client-core && npx vitest run __tests__/streaming.test.ts
```

Expected: FAIL. `is now readable by the default decoder` fails with `unknown` containing `{message: 'message', channel: '/ws/orders'}` and `Order:9` undefined. The second new test passes already (`presence` is reported under both orders).

- [ ] **Step 3: Reorder the expression**

In `packages/client-core/src/live.ts`, change the one line inside `decodeFrame`:

```ts
  const name = envelope['event'] ?? envelope['type'] ?? envelope['name'];
```

- [ ] **Step 4: Rewrite the doc comment above it**

The existing comment explains the old order and is now wrong. Replace the paragraph beginning "The default envelope reader, over the three shapes in circulation." with:

```ts
/**
 * The default envelope reader, over the three shapes in circulation.
 *
 * `event`/`data` is what an SSE adapter naturally produces, since `EventSource`
 * dispatches by event name, and what `extensions/streaming` sends; `type`/`payload`
 * is what a plain Forge WebSocket handler emits; `name` is the AsyncAPI spelling.
 * A message with a name and no payload field is its own payload, which is what a
 * server that sends the entity flat with a `type` discriminator produces.
 *
 * `event` is read *first*, and the order is the whole of the fix for a defect
 * that discarded entire channels. In the streaming extension `type` is not the
 * message name at all -- it is the transport kind, one of seven reserved strings
 * -- and the domain name lives in `event`. Under the previous `type ?? event`
 * order every frame from that extension decoded as `message`, nothing in any
 * generated manifest is keyed on `message`, and the channel was reported through
 * `onUnknown` while its socket sat open and healthy. Reading `event` first costs
 * the two older shapes nothing, because neither carries an `event` field; the
 * only server this order is wrong for is one sending `type` as a message name
 * *and* `event` as something else, which no shape in circulation does.
 *
 * This does not make `forgeStreamingDecoder` redundant. That decoder still knows
 * which names are reserved transport kinds -- presence, typing, join -- and drops
 * them silently instead of reporting them, and it owns the `channel_id` mapping.
 * This one only stops the default from being wrong about the name.
 */
```

- [ ] **Step 5: Run the full client suite**

```bash
cd packages/client-core && npm test
```

Expected: PASS, all files. Pay attention to `live.test.ts` and `envelope.test.ts` — they exercise the `{type, payload}` shape and must be unaffected.

- [ ] **Step 6: Typecheck the source**

```bash
cd packages/client-core && npx tsc --noEmit -p tsconfig.json
```

Expected: exit 0, no output.

- [ ] **Step 7: Commit**

```bash
git add packages/client-core/src/live.ts packages/client-core/__tests__/streaming.test.ts
git commit -m "fix(client-core): read event before type in the default frame decoder

The streaming extension puts the domain name in event and the transport
kind in type, so the type-first order named every frame message and
discarded whole channels through onUnknown. Reading event first is
correct for that envelope and costs the two older shapes nothing, since
neither carries an event field.

forgeStreamingDecoder is still the right choice for a streaming channel:
it drops the extension's reserved transport frames rather than reporting
them, and it owns the channel_id mapping. This only fixes the name."
```

- [ ] **Step 8: Update the docs that describe the old default**

`5463788c` documented the default decoder as reading `type`. Three places now overstate the problem — they say a streaming channel is *discarded* without `forgeStreamingDecoder`, which after Task 1 is only true of its transport frames.

In `packages/client-core/README.md`, in the "Which envelope the frames arrive in" section, replace the blockquote beginning `> **Getting this wrong is silent and total.**` with:

```markdown
> **The default reads `event` first**, so a streaming channel's domain frames
> bind without any configuration. What `forgeStreamingDecoder` adds is the rest
> of the envelope: it knows `presence`, `typing` and `join` are transport kinds
> rather than message names, and drops them instead of reporting each one
> through `onUnknown` once per channel.
```

In `docs/content/docs/web-client/invalidation.mdx`, in the `<Callout type="warn">` under "Which field carries the name", replace the first paragraph with:

```markdown
**A channel served by `extensions/streaming` should pass `forgeStreamingDecoder`** as the binder's `decode`. The default decoder reads `event` first, so domain frames bind without it — but it has no notion of a reserved transport kind, so every `presence`, `typing` and `join` frame is reported as an unknown message on a channel that is working exactly as designed.
```

In `docs/content/docs/web-client/adapters.mdx`, in the live-queries callout, replace the paragraph beginning `And they need the binder to be reading the envelope` with:

```markdown
A channel served by `extensions/streaming` should also pass `decode: forgeStreamingDecoder()`. Its domain frames bind under the default decoder, but its transport frames — presence, typing, join — are reported as unknown messages without it. See [Which field carries the name](/docs/web-client/invalidation#which-field-carries-the-name).
```

- [ ] **Step 9: Verify the docs still compile and commit**

```bash
cd docs && npx fumadocs-mdx
```

Expected: `[MDX] generated files in <n>ms`, exit 0.

```bash
git add packages/client-core/README.md docs/content/docs/web-client/invalidation.mdx docs/content/docs/web-client/adapters.mdx
git commit -m "docs(web-client): the default decoder now reads event first

Narrows the warning to what is still true after the reorder: a streaming
channel's domain frames bind out of the box, and forgeStreamingDecoder
earns its place by knowing which names are transport kinds."
```

---

### Task 2: Derive the Go constant list instead of copying it

**Files:**
- Modify: `extensions/streaming/frame_test.go:74-111` (`TestTransportKindsMirrorTheConstants`)
- Create: `extensions/streaming/testdata/constants_fixture.go`
- Read only: `extensions/streaming/internal/streaming.go` (parsed, never modified — a parallel workstream owns it)

**Interfaces:**
- Consumes: `streaming.TransportKinds() []string` from `frame.go`, already committed.
- Produces: two unexported test helpers used only within `frame_test.go` —
  `messageTypesIn(t *testing.T, path string) []string` (parses one file) and
  `declaredMessageTypes(t *testing.T) []string` (calls it on `internal/streaming.go`
  and fails when the parse finds nothing).

**Why:** the test's comment claims that adding a `MessageType*` constant without adding it to `TransportKinds()` fails here. It does not. `declared` is a hand-written literal that changes only when someone edits the test, so the assertion compares two copies of the same list and a new constant slips past both. The fix is to read the constants from the source that declares them.

- [ ] **Step 1: Write the failing test**

Add this helper and rewrite the first assertion in `extensions/streaming/frame_test.go`. The imports needed are `go/ast`, `go/parser`, `go/token`, `path/filepath`, `strconv`, `strings`.

```go
// declaredMessageTypes reads every MessageType* constant out of the file that
// declares them, rather than restating them here.
//
// A hand-written copy was the first version, and it asserted nothing: it changed
// only when somebody edited this test, so the comparison was between two copies
// of the same list and a newly declared kind passed both. Parsing the source is
// the only spelling in which "a constant was added and TransportKinds was not"
// is a detectable event.
func declaredMessageTypes(t *testing.T) []string {
	t.Helper()

	path := filepath.Join("internal", "streaming.go")

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var declared []string

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}

		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for i, name := range value.Names {
				if !strings.HasPrefix(name.Name, "MessageType") || i >= len(value.Values) {
					continue
				}

				lit, ok := value.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}

				unquoted, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", name.Name, err)
				}

				declared = append(declared, unquoted)
			}
		}
	}

	if len(declared) == 0 {
		t.Fatalf("no MessageType* constants found in %s; the parse found nothing to check", path)
	}

	return declared
}
```

Then replace the `declared := []string{...}` literal in `TestTransportKindsMirrorTheConstants` with:

```go
	declared := declaredMessageTypes(t)
```

and change the function's doc comment, which currently overstates what it caught:

```go
// TestTransportKindsMirrorTheConstants fails when a MessageType* constant is
// declared and not added to TransportKinds.
//
// The failure is the point. An unmirrored kind reaches the client as a frame
// name no binding claims and is reported as an unknown message on every channel
// that emits it -- a quiet, permanent warning for something working exactly as
// designed. The constants are parsed out of internal/streaming.go rather than
// copied here, because a copy is not a check: it agrees with whatever it was
// last edited to agree with.
```

- [ ] **Step 2: Split the path out so the parse itself is testable**

The proof that "a new constant would be caught" must not require editing
`internal/streaming.go` — a parallel workstream owns that file and a temporary
edit there risks colliding with its writes, or being left behind if this task
errors out partway. Take the path as a parameter and point a second test at a
fixture instead.

Restructure the helper from Step 1 into two functions:

```go
// messageTypesIn reads every MessageType* constant declared in one file.
func messageTypesIn(t *testing.T, path string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var declared []string

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}

		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for i, name := range value.Names {
				if !strings.HasPrefix(name.Name, "MessageType") || i >= len(value.Values) {
					continue
				}

				lit, ok := value.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}

				unquoted, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", name.Name, err)
				}

				declared = append(declared, unquoted)
			}
		}
	}

	return declared
}

// declaredMessageTypes reads the constants out of the file that declares them,
// rather than restating them here.
//
// A hand-written copy was the first version, and it asserted nothing: it changed
// only when somebody edited this test, so the comparison was between two copies
// of the same list and a newly declared kind passed both. Parsing the source is
// the only spelling in which "a constant was added and TransportKinds was not"
// is a detectable event.
func declaredMessageTypes(t *testing.T) []string {
	t.Helper()

	path := filepath.Join("internal", "streaming.go")
	declared := messageTypesIn(t, path)

	if len(declared) == 0 {
		t.Fatalf("no MessageType* constants found in %s; the parse found nothing to check", path)
	}

	return declared
}
```

- [ ] **Step 3: Write the fixture**

Create `extensions/streaming/testdata/constants_fixture.go`. The `testdata`
directory is ignored by the go tool, so a `.go` file inside it is never compiled
or vetted — it exists only to be parsed.

```go
package fixture

// A stand-in for the constant block in internal/streaming.go, with one kind the
// real file does not declare. If messageTypesIn stops noticing an added
// constant, the ack below stops appearing and the test that reads this fails --
// which is the proof the real assertion cannot give without editing a file this
// module shares with another workstream.

// Message types.
const (
	MessageTypeMessage = "message"
	MessageTypeAck     = "ack"
)

// Deliberately not a message type: the prefix filter must skip it.
const NotAMessageType = "ignored"
```

- [ ] **Step 4: Write the test that proves the parse catches an added constant**

Add to `extensions/streaming/frame_test.go`:

```go
// TestMessageTypesInFindsEveryDeclaredConstant is the proof that
// declaredMessageTypes would notice a newly declared kind.
//
// Asserted against a fixture rather than by temporarily editing
// internal/streaming.go: that file is shared with another workstream, and a
// proof that requires mutating somebody else's file is a proof that will one
// day be left half-applied.
func TestMessageTypesInFindsEveryDeclaredConstant(t *testing.T) {
	got := messageTypesIn(t, filepath.Join("testdata", "constants_fixture.go"))

	want := []string{"message", "ack"}

	if !slices.Equal(got, want) {
		t.Errorf("messageTypesIn(fixture) = %v, want %v", got, want)
	}
}
```

- [ ] **Step 5: Run both tests**

```bash
cd extensions/streaming && GOWORK=off go test -run 'TestTransportKindsMirrorTheConstants|TestMessageTypesInFindsEveryDeclaredConstant' -v ./
```

Expected: both PASS. `TestMessageTypesInFindsEveryDeclaredConstant` proves the
parse picks up `ack` — a constant `TransportKinds()` does not contain — so the
same helper pointed at the real file will notice a real addition.

- [ ] **Step 6: Confirm you changed nothing you do not own**

```bash
git status --short extensions/streaming/internal/
```

Expected: no output attributable to this task. The parallel workstream may have
its own edits there; none of them should be yours.

- [ ] **Step 7: Format, vet and commit**

```bash
cd extensions/streaming && gofmt -l frame_test.go && GOWORK=off go vet ./
```

Expected: no output from either.

```bash
git add extensions/streaming/frame_test.go extensions/streaming/testdata/constants_fixture.go
git commit -m "test(streaming): parse the message-type constants instead of copying them

The list was hand-written, so it agreed with whatever it was last edited
to agree with: a newly declared MessageType* constant that never reached
TransportKinds passed the assertion that exists to catch exactly that.
Reading them out of internal/streaming.go makes the omission detectable."
```

---

### Task 3: Read the TypeScript mirror from the TypeScript file

**Files:**
- Modify: `extensions/streaming/frame_test.go` (the second assertion in `TestTransportKindsMirrorTheConstants`)
- Read only: `packages/client-core/src/streaming.ts`

**Interfaces:**
- Consumes: `declaredMessageTypes` from Task 2 is *not* required — this task is independent and touches a different assertion in the same function. If Task 2 has not been done, the `declared` literal is still there and untouched by this task.
- Produces: an unexported test helper `mirroredTransportKinds(t *testing.T) ([]string, bool)`.

**Why:** `mirrored` is a hardcoded snapshot of the TypeScript set inside a Go file. Editing `TRANSPORT_KINDS` in `streaming.ts` fails no test anywhere — the Go test only notices Go drifting away from a frozen copy, which is the less likely direction. Reading the actual file makes the mirror bidirectional.

**Module boundary:** `extensions/streaming` is its own Go module and can be consumed without the rest of the repo. The helper returns `false` when the file is absent and the test skips, rather than failing for a consumer who has no `packages/` directory.

- [ ] **Step 1: Write the helper**

Add to `extensions/streaming/frame_test.go`. Imports needed: `os`, `regexp`, and `path/filepath` — the last is already present if Task 2 was done first, and must be added if this task is done alone.

```go
// transportKindsLiteral matches the TRANSPORT_KINDS declaration in
// packages/client-core/src/streaming.ts and captures the body of its Set.
//
// A regexp rather than a TypeScript parse, and the narrowness is deliberate: it
// matches one declaration whose exact text is a few lines away in a file this
// repository owns. If that declaration is ever rewritten into a form this does
// not match, the helper reports no kinds and the test fails loudly rather than
// passing on an empty comparison -- see the length check below.
var transportKindsLiteral = regexp.MustCompile(`(?s)TRANSPORT_KINDS[^=]*=\s*new Set\(\[(.*?)\]\)`)

var quotedKind = regexp.MustCompile(`'([^']*)'`)

// mirroredTransportKinds reads the set the TypeScript decoder actually holds.
//
// Returns false when the client package is not present. This module is
// publishable on its own, and a consumer who fetched it without the repository
// around it has no packages/ directory -- skipping there is correct, whereas
// failing would make the module untestable outside its own tree.
func mirroredTransportKinds(t *testing.T) ([]string, bool) {
	t.Helper()

	path := filepath.Join("..", "..", "packages", "client-core", "src", "streaming.ts")

	source, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false
		}

		t.Fatalf("read %s: %v", path, err)
	}

	block := transportKindsLiteral.FindSubmatch(source)
	if block == nil {
		t.Fatalf("no TRANSPORT_KINDS set found in %s; the decoder's reserved kinds could not be read", path)
	}

	var kinds []string

	for _, match := range quotedKind.FindAllSubmatch(block[1], -1) {
		kinds = append(kinds, string(match[1]))
	}

	if len(kinds) == 0 {
		t.Fatalf("TRANSPORT_KINDS in %s parsed to nothing", path)
	}

	return kinds, true
}
```

- [ ] **Step 2: Replace the hardcoded mirror**

In `TestTransportKindsMirrorTheConstants`, swap the `mirrored := []string{...}` literal and the comment above it for:

```go
	// The set the TypeScript decoder actually holds, read from the file that
	// holds it. A copy pinned here would only catch this side drifting; the
	// direction that matters as much is the client's set gaining a kind that
	// Go never reserved.
	mirrored, present := mirroredTransportKinds(t)
	if !present {
		t.Skip("packages/client-core is not present; nothing to mirror against")
	}
```

- [ ] **Step 3: Run it**

```bash
cd extensions/streaming && GOWORK=off go test -run TestTransportKindsMirrorTheConstants -v ./
```

Expected: PASS.

- [ ] **Step 4: Prove it catches TypeScript-side drift**

Temporarily add `'ack',` to the `TRANSPORT_KINDS` set in `packages/client-core/src/streaming.ts`, then:

```bash
cd extensions/streaming && GOWORK=off go test -run TestTransportKindsMirrorTheConstants ./
```

Expected: FAIL — `TransportKinds() = [...], but packages/client-core/src/streaming.ts holds [... ack]`.

**Then revert the TypeScript edit** and re-run to confirm PASS.

- [ ] **Step 5: Prove the skip works**

```bash
cd extensions/streaming && GOWORK=off go test -run TestTransportKindsMirrorTheConstants -v ./ 2>&1 | grep -i skip
```

To exercise it, temporarily rename the file (`git stash` is not appropriate here — it would take the parallel workstream's changes too):

```bash
mv packages/client-core/src/streaming.ts /tmp/streaming.ts.bak
cd extensions/streaming && GOWORK=off go test -run TestTransportKindsMirrorTheConstants -v ./
mv /tmp/streaming.ts.bak packages/client-core/src/streaming.ts
```

Expected: `--- SKIP` with the reason, not a failure. Confirm the file is restored with `git status --short packages/client-core/src/streaming.ts` showing nothing.

- [ ] **Step 6: Format, vet and commit**

```bash
cd extensions/streaming && gofmt -l frame_test.go && GOWORK=off go vet ./
```

```bash
git add extensions/streaming/frame_test.go
git commit -m "test(streaming): read the client's reserved kinds from its source

The mirror was a snapshot of the TypeScript set pinned inside a Go file,
so it only ever caught Go drifting away from it. Editing TRANSPORT_KINDS
in streaming.ts broke no test at all. Reading the file makes the check
bidirectional, and skips when the client package is absent -- this module
is publishable without the repository around it."
```

---

### Task 4: Let an empty `channel_id` fall through to `channel`

**Files:**
- Modify: `packages/client-core/src/streaming.ts:131`
- Test: `packages/client-core/__tests__/streaming.test.ts` (add to the `channel resolution` describe block)

**Interfaces:**
- Consumes: `forgeStreamingDecoder(options?)` as committed.
- Produces: no signature change.

**Why:** `envelope['channel_id'] ?? envelope['channel']` coalesces on null and undefined, not on the empty string. A frame spelling `{"channel_id": "", "channel": "orders"}` takes the empty `channel_id`, fails the non-empty check, and loses the `channel` it did carry. Unreachable from the Go extension, whose `ChannelID` is `omitempty`, and reachable from any hand-rolled server that emits the field unconditionally. Low severity, one line.

- [ ] **Step 1: Write the failing test**

Add inside the `describe('channel resolution', ...)` block in `packages/client-core/__tests__/streaming.test.ts`:

```ts
  // `??` coalesces on null and undefined, not on the empty string, so an
  // envelope spelling channel_id unconditionally used to swallow the `channel`
  // it did carry. Go's ChannelID is omitempty and never produces this; a
  // hand-rolled server that always emits the field does.
  it('falls through an empty channel_id to channel', () => {
    const decode = forgeStreamingDecoder({
      channelOf: (id) => (id === 'orders' ? '/ws/orders' : undefined),
    });

    const decoded = decode({ type: 'message', event: 'order.created', channel_id: '', channel: 'orders', data: { id: 9 } });

    expect(decoded?.channel).toBe('/ws/orders');
  });
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd packages/client-core && npx vitest run __tests__/streaming.test.ts -t 'falls through an empty channel_id'
```

Expected: FAIL — `expected undefined to be '/ws/orders'`.

- [ ] **Step 3: Fix the coalescing**

In `packages/client-core/src/streaming.ts`, replace the `channelID` line:

```ts
    const named = envelope['channel_id'];
    const channelID = typeof named === 'string' && named !== '' ? named : envelope['channel'];
```

- [ ] **Step 4: Run the suite**

```bash
cd packages/client-core && npm test
```

Expected: PASS, all files.

- [ ] **Step 5: Commit**

```bash
git add packages/client-core/src/streaming.ts packages/client-core/__tests__/streaming.test.ts
git commit -m "fix(client-core): fall through an empty channel_id to channel

?? coalesces on null and undefined, not on the empty string, so a server
emitting channel_id unconditionally lost the channel it did carry. Go's
ChannelID is omitempty and never produces this shape; a hand-rolled
server does."
```

---

### Task 5 (optional): Say in `NewEventMessage` that a reserved name is unbindable

**Files:**
- Modify: `extensions/streaming/frame.go` (the `NewEventMessage` doc comment only)
- Test: `extensions/streaming/frame_test.go`

**Interfaces:**
- Consumes: `streaming.IsTransportKind(kind string) bool` as committed.
- Produces: no signature change.

**Why, and why it is optional:** `NewEventMessage("presence", data)` builds a frame whose `event` collides with a reserved transport kind. `IsTransportKind` exists to detect this and nothing calls it. The severity is lower than it first looks: because `event` is non-empty, the client takes the `event` branch and the frame is *reported* through `onUnknown` rather than silently dropped — so the mistake is visible in development. That is why this is a doc change and a test rather than a signature change to a constructor with no consumers yet. **Skip this task if you disagree that it earns its keep.**

- [ ] **Step 1: Write the test**

Add to `extensions/streaming/frame_test.go`:

```go
// TestNewEventMessageAcceptsAReservedName pins the deliberate absence of a
// guard. A domain name colliding with a transport kind is a producer mistake,
// but it is a visible one -- the client takes the event branch, finds no
// binding, and reports it -- so the constructor documents the collision and
// leaves IsTransportKind to the caller who wants to check.
func TestNewEventMessageAcceptsAReservedName(t *testing.T) {
	msg := streaming.NewEventMessage(streaming.MessageTypePresence, nil)

	if msg.Event != streaming.MessageTypePresence {
		t.Errorf("Event = %q, want the name it was given", msg.Event)
	}

	if !streaming.IsTransportKind(msg.Event) {
		t.Error("IsTransportKind is the check a producer runs to catch this")
	}
}
```

- [ ] **Step 2: Run it**

```bash
cd extensions/streaming && GOWORK=off go test -run TestNewEventMessageAcceptsAReservedName ./
```

Expected: PASS immediately — this pins existing behaviour rather than driving a change.

- [ ] **Step 3: Add the paragraph to the godoc**

In `extensions/streaming/frame.go`, append to the `NewEventMessage` doc comment, before the closing line:

```go
// An event whose name collides with a reserved transport kind is accepted and
// is a mistake: the client will look for a binding named "presence" and find
// none. It is not rejected here because the failure is visible -- an event name
// always takes the client's event branch, so the frame is reported rather than
// dropped -- and because a constructor that can fail is a worse trade than a
// caller running IsTransportKind when the name is not a literal.
```

- [ ] **Step 4: Format, vet, commit**

```bash
cd extensions/streaming && gofmt -l frame.go frame_test.go && GOWORK=off go vet ./
```

```bash
git add extensions/streaming/frame.go extensions/streaming/frame_test.go
git commit -m "docs(streaming): say that a reserved event name is unbindable

NewEventMessage accepts one, and the collision is a producer mistake that
IsTransportKind exists to catch. Documented rather than rejected: the
failure is visible on the client, and a constructor that can fail is the
worse trade."
```

---

## Not in this plan

- **`@types/node`** — environmental, handled in Preflight, not a code change.
- **The parallel workstream's test files** (`hooks_delivery_pool_test.go`, `manager_test.go`) breaking the streaming test binary. Not ours; verify with `GOWORK=off go build ./...` and wait.
- **Generator-emitted binder wiring.** No generator emits any `StreamBinder` or `SubscriptionManager` construction today — this was checked against `internal/client/generators/`. Making the generator produce stream wiring is new surface area and a separate design question, not a follow-up to this fix. Task 1 removes the reason it felt urgent.
- **The branch mixing** on `fix/streaming-frame-decoder`. Cherry-pick `cb773c34` and `5463788c` onto a fresh branch off `95e55b70` once the parallel session is idle, if you want them separated.
