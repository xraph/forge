# Cross-runtime client fixtures

JSON files that both client runtimes read, so the TypeScript runtime in
`packages/client-core` and the Dart runtime in `dart-packages/forge_client`
cannot drift apart without a test failing on one side.

| Directory | What a file holds | Written by |
|---|---|---|
| `snapshot/` | the responses a cache fetched, the dehydrated payload it produced, and what each query read | `fixtures-export.test.ts` (TS), and `from-dart.json` by `fixtures_test.dart` (Dart) |
| `codec/` | a client-shaped value, the schema it was normalized against, and its `__ref` wire encoding | `fixtures-export.test.ts` |
| `frames/` | an initial store, batches of stream frames, and the store they produce | `fixtures-export.test.ts` |
| `codec/generated-codecs.json` | wire payloads for generated clients, which the TypeScript and Dart codecs must decode and encode the same way | you, by hand. It is the one file here that is not generated. |
| `ops/` | generator parity tables | `FORGE_WRITE_FIXTURES=1 go test ./internal/client/generators/dart/ -run TestTablesAgreeWithTypeScript` |

Never edit a file by hand, with one exception (below). Regenerate the TS-written ones with:

    cd packages/client-core
    FORGE_WRITE_FIXTURES=1 npx vitest run __tests__/fixtures-export.test.ts

and the Dart-written one with:

    cd dart-packages/forge_client
    FORGE_WRITE_FIXTURES=1 fvm dart test test/fixtures_test.dart

Without the variable both suites only verify. A failure means the runtimes
disagree, or a runtime changed its output on purpose and the files need
regenerating in the same commit.

The TS files are written with object keys sorted at every level and a
trailing newline, so regenerating without a change leaves no diff.

`codec/generated-codecs.json` is the exception. It has the kind
`generated-codec-parity` and no runtime library reads it: both loaders skip any
`codec/` file of another kind. The Go test `TestGeneratedCodecsAgreeAcrossRuntimes`
in `internal/client/generators/dart` reads it, generates a TypeScript client and
a Dart client from one spec, and runs every payload through both codecs under
node and fvm. Add a case there when a rename, union or int64 shape needs
pinning. A case that names a `model` also builds that Dart model from the
decoded value and encodes it back, against TypeScript's encode of its decode,
and `modelWire` states the wire both must reach: that is where Dart converts
an int64. A variant's `int64` sets the Dart `--int64` mode. The test skips
when node, esbuild or fvm is missing.

The `ops/` files are the tables each generator emits for one spec, so a Dart
client and a TypeScript client cache, invalidate and authorize the same way.
`TestTablesAgreeWithTypeScript` compares the two generators' output, then
checks it against these files. A missing, stale or unlisted file fails it.

## Where the runtimes differ on purpose

Two snapshot files exist to pin behaviour the runtimes do not share.

`snapshot/numeric-principal.json` carries the principal `42`. TypeScript
accepts a string, number, null or undefined principal, so the file is a valid
TS snapshot. Dart refuses a non-string principal by design: its session is
keyed by a string, and a number would compare equal to nothing a server sends
back. The Dart suite asserts the refusal rather than the round trip.

`snapshot/null-header-value.json` carries a query whose `args.headers` map has
a `null` value next to a string one. `TagContext` does not type `headers`, but
`args` is stored and serialized whole, so TypeScript carries the `null` through
into the payload and into the cache key. The file lets Dart pin how it reads
that value.
