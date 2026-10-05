# forge_client

The runtime a generated Forge Dart client runs on. You don't usually import it
on its own: a package generated with `forge client generate --language dart
--hooks` depends on it, and you talk to that package's bindings. What you get
underneath is the same engine the TypeScript client uses, ported module for
module. For a typical call, a query keyed in Dart and one keyed in TypeScript
land on the same cache key and the same entity records. The keys aren't
byte-identical in every case, mind you: Dart's `operationQueryKey` drops an
empty path, query or header map and any null path or query value, so a call
that passes those can key differently from TypeScript. Hydration doesn't care,
because it re-derives every key from the operation and its arguments.

It's pure Dart. No Flutter, no state library. The Flutter and Riverpod
adapters live in their own packages.

## Install

During development the packages in `dart-packages/` depend on each other by
path:

```yaml
dependencies:
  forge_client:
    path: ../forge_client
```

You'll need Dart 3.13 or later. In this repository run everything through
fvm (`fvm dart test`), because the pinned toolchain lives in
`dart-packages/.fvmrc`.

## Example

```dart
import 'package:forge_client/forge_client.dart';
import 'package:orders_forge_client/orders_forge_client.dart';

Future<void> main() async {
  final cache = configureClient(
    transport: RestTransport(baseUrl: Uri.parse('https://api.example.com')),
    entities: entities,
  );

  final order = await getOrder(const GetOrderArgs(id: '7')).fetch(cache);
  getOrder(const GetOrderArgs(id: '7')).watch(cache).listen((state) => print(state.dataOrNull));
  await updateOrder(cache, const UpdateOrderArgs(id: '7', note: Assign('gift')));
}
```

The update writes the new order into the entity store, so the stream above
emits it without a refetch, and every other query showing `Order:7` updates
too. A read that changed nothing hands back the identical model object, which
is what lets a widget skip its rebuild.

## Language features used

We read the Dart SDK changelog for 3.10 through 3.13
(https://github.com/dart-lang/sdk/blob/main/CHANGELOG.md) before writing this
package. These are the features it adopts, and where:

| Feature | Since | Used for |
|---|---|---|
| `sealed`, `final` and `interface` class modifiers | 3.0 | every closed hierarchy: `QueryState`, `MutationState`, `Optimistic`, `EntityPatch`, `Value`, `SyncStatus`, `CacheEvent`, `RequestEvent`, `StreamBinding` |
| Records and patterns, exhaustive `switch` expressions | 3.0 | spec translation in the overlay, typed state mapping, the key and tag renderers |
| Null-aware elements (`'body': ?value`) | 3.8 | building cache keys and request maps without `if` noise |
| Dot shorthands (`.pending`) | 3.10 | `QueryStatus` transitions inside the cache |
| Private named parameters (`this._onError`) | 3.12 | constructors of `QueryCache` and `RestTransport` keep their fields private and their parameters readable (`Invalidator` assigns its private fields in an initializer list) |
| Primary constructors | 3.13 | small private value classes |

Two things from the spec's baseline are not here, on purpose. Extension types
for entity IDs and int64 values belong to the generated packages, because this
runtime never holds a typed ID. `dart:js_interop` and `package:web` arrive with
the stream transports. Nothing in this package imports `dart:html`, and nothing
ever should.
