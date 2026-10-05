# Cross-runtime client fixtures

JSON files that both client runtimes read, so the TypeScript runtime in
`packages/client-core` and the Dart runtime in `dart-packages/forge_client`
cannot drift apart without a test failing on one side.

| Directory | What a file holds | Written by |
|---|---|---|
| `snapshot/` | the responses a cache fetched, the dehydrated payload it produced, and what each query read | `fixtures-export.test.ts` (TS), and `from-dart.json` by `fixtures_test.dart` (Dart) |
| `codec/` | a client-shaped value, the schema it was normalized against, and its `__ref` wire encoding | `fixtures-export.test.ts` |
| `frames/` | an initial store, batches of stream frames, and the store they produce | `fixtures-export.test.ts` |
| `ops/` | generator parity tables | plan 02's generators |

Never edit a file by hand. Regenerate the TS-written ones with:

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
