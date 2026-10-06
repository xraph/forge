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

## Devtools

In debug and profile builds, `configureClient` attaches the forge devtools to
the cache it creates. Open Flutter DevTools, enable the forge tab the first
time it asks, and you can see why a query did or did not refetch, the event
log, the frame ring, the request log, the outbox and sync state, and switch the
network to offline or slow.

Add the extension as a dev dependency so DevTools can find it:

```yaml
dev_dependencies:
  forge_client_devtools:
    path: ../forge/dart-packages/forge_client_devtools
```

Release builds contain none of it. `kForgeDevtools` is a constant that is false
under `dart.vm.product`, which every Flutter release build defines, so the
compiler removes the code. To leave it out of a debug build too, pass
`--dart-define=forge.devtools=false`.

`configureClient` wraps a `RestTransport` in the offline and latency simulator
and feeds the request log from it. It never wraps anything else. That matters
for an offline app, because a simulator outside an `OutboxTransport` would fail
a write before the outbox could queue it.

### Offline apps

The simulator has to sit directly on the network, beneath the outbox. Then a
write made while the panel says offline is queued like one made on a lost
connection, and a replay goes through the simulated network.

The short way is `OfflineClient.open`, which never goes through
`configureClient`, so you ask for devtools explicitly:

```dart
final offline = await OfflineClient.open(
  transport: rest,
  entities: entities,
  operations: operations,
  storage: storage,
  principal: userId,
  connectivity: connectivity,
  devtools: true,
);
```

That wraps `rest` in the simulator beneath the outbox, merges the simulator
into `connectivity` with `withSimulatedConnectivity`, and registers the client
as the panel's `OutboxInspector` so replay and discard work. It does nothing in
a release build.

If you build the client yourself, do the same three steps by hand. The order is
outbox, then simulator, then `RestTransport`:

```dart
final rest = RestTransport(baseUrl: Uri.parse('https://api.example.com'));
final controls = kForgeDevtools ? ControlledTransport(rest) : null;
final outbox = OutboxTransport(controls ?? rest);
final cache = QueryCache(transport: outbox, entities: entities, storage: storage);
final offline = OfflineClient(
  cache: cache,
  operations: operations,
  connectivity: controls == null ? connectivity : withSimulatedConnectivity(connectivity, controls),
  transport: outbox,
);

registerForgeServiceExtensions(
  cache,
  transport: rest,         // feeds the request log
  controls: controls,      // the offline and latency switches
  operations: operations,  // the generated table, for "what would this invalidate"
  outbox: offline,         // replay and discard
);
```

Leave `withSimulatedConnectivity` out and the outbox never hears that the
simulated network is down. A PATCH or POST then fails as uncertain ("the
request may have reached the server") instead of queuing, even though the
simulator never let it out.

### Registering a cache yourself

A cache built with the `QueryCache` constructor, or handed an `OutboxTransport`
to `configureClient`, is attached without a simulator or a request log. Fill in
what is missing with `registerForgeServiceExtensions`. On a cache that is
already attached it registers nothing again: it fills the slots that are still
empty (`transport`, `controls`, `revalidation`, `operations`) and sets
`outbox`. A slot that is already filled is never overwritten, so the first
registration wins.

```dart
import 'package:forge_client/devtools.dart';

registerForgeServiceExtensions(cache, outbox: offlineClient);
```

To make `revalidateOnReconnect` believe the device is offline too, give it the
merged signal:

```dart
final controls = forgeDevtoolsFor(cache)?.controls;
final online = controls == null ? connectivity : withSimulatedConnectivity(connectivity, controls);
```

The request log keeps no headers and no request or response bodies. Assigning
your own `cache.observer` replaces the devtools recorder, which then stops
recording. When the principal changes, the devtools drop everything they
recorded for the previous one.

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
