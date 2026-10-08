# forge_client_flutter

Flutter widgets over `forge_client`. You don't need a state library to use Forge well. Queries, mutations, invalidation, combined queries, side effects and a little local state are all here, built on Flutter's own `Listenable` machinery.

If your app is already on Riverpod 3, `forge_client_riverpod` (in `dart-packages/forge_client_riverpod`) is the adapter for it. It reuses this package's seams, so both revalidate the same way.

## Install

Everything in `dart-packages/` is `1.0.0-dev` and `publish_to: none`, so there is no version to depend on yet. Publishing is a follow-up. Until then, depend on the packages by path:

```yaml
dependencies:
  forge_client:
    path: ../forge_client
  forge_client_flutter:
    path: ../forge_client_flutter
```

or straight from the repository:

```yaml
dependencies:
  forge_client:
    git:
      url: https://github.com/xraph/forge
      path: dart-packages/forge_client
      ref: <commit sha>
  forge_client_flutter:
    git:
      url: https://github.com/xraph/forge
      path: dart-packages/forge_client_flutter
      ref: <commit sha>
```

Put the same full commit sha in both `ref:` lines. Pub turns this package's own `path: ../forge_client` into a git dependency pinned to the commit it resolved, so your direct `forge_client` entry has to name that exact commit too, and a branch name or a missing `ref` fails to resolve.

You'll need Flutter 3.47 or later and the package your own API generated with `forge client generate --language dart --hooks`. The examples below call it `orders_forge_client`. Its names (`entities`, `operations`, `streams`, `getOrder`, `listOrders`, `updateOrder` and the `...Args` classes) are the ones the examples use.

## Set up

Build the cache once, with the per-frame commit scheduler, and put a scope above the screens that use it:

```dart
import 'package:flutter/material.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';
import 'package:orders_forge_client/orders_forge_client.dart';

final client = configureClient(
  transport: RestTransport(baseUrl: Uri.parse('https://api.example.com')),
  entities: entities,
  commitScheduler: frameCommitScheduler(),
);

void main() => runApp(ForgeScope(client: client, child: const App()));
```

While it is mounted, `ForgeScope` keeps focus revalidation (through `AppLifecycleListener`) and reconnect revalidation (through `connectivity_plus`) installed on the cache. The scope is optional. Without one, widgets fall back to the global client from `configureClient`, and you call `installFlutterSeams(client)` once at startup so stale data still refreshes when the app comes back to the foreground.

Widgets resolve their cache in this order: an explicit `client:` argument, then the nearest `ForgeScope`, then the global client.

Pass signals that live as long as the scope. A `FocusSignal` built inside a `build` method is a new object on every rebuild, and every new object reinstalls the seams. Seams are ref-counted per cache, so when several scopes share one client, the first install's signals stay in effect until every holder has released.

## Queries

```dart
Widget orderView(String id) => ForgeQueryBuilder(
  query: getOrder(GetOrderArgs(id: id)),
  builder: (context, state) => switch (state) {
    QueryIdle() || QueryLoading() => const CircularProgressIndicator(),
    QuerySuccess(:final data) => OrderView(data),
    QueryFailure(:final error, :final previous) => ErrorView(error, previous),
  },
);
```

Write the `query:` inline. The builder keys its subscription on the query's key, the same key the cache uses, so a parent rebuild doesn't refetch.

- `enabled: false` keeps the query idle and fetches nothing. Use it for a query that depends on another one's result.
- `staleTime:` sets how long this call site treats the result as fresh.
- `live: true` also applies server frames pushed on the channels the query's entities ride. It needs the wiring in the next section.
- `select:` rebuilds only when the selected value changes. It receives the whole state and compares by identity, then by deep equality, so a selector that builds a new list each time doesn't rebuild while the list is unchanged. Return a record to watch several things: `select: (s) => (s.dataOrNull?.note, s.isFetching)`.

Without `select`, a builder rebuilds only when something it can render changed. The core keeps unchanged records `identical`, so a write to `Order:1` doesn't rebuild a widget showing `Order:2`.

## Live updates

`live: true` does nothing until the app has built the stream runtime, and nothing in this package builds it for you. Without it, the builder reports a `StateError` through the cache's `onError` (context `live`) and carries on with plain queries. You need three pieces: the cache, a `SubscriptionManager` that owns the sockets, and a `StreamBinder` that decodes frames and writes them into the cache.

```dart
({QueryCache cache, StreamBinder binder}) buildLiveClient() {
  void onError(Object error, String context) => debugPrint('forge $context: $error');

  final cache = configureClient(
    transport: RestTransport(baseUrl: Uri.parse('https://api.example.com')),
    entities: entities,
    commitScheduler: frameCommitScheduler(),
    onError: onError,
  );
  final manager = SubscriptionManager(
    connect: webSocketConnection(),
    baseUrl: Uri.parse('wss://api.example.com'),
    principal: () => cache.principal,
    revive: ConnectivityPlusSignal(),
    onError: onError,
  );
  final binder = StreamBinder(cache: cache, streams: streams, manager: manager, onError: onError);
  return (cache: cache, binder: binder);
}

Widget liveOrder(String id) => ForgeQueryBuilder(
  query: getOrder(GetOrderArgs(id: id)),
  live: true,
  builder: (context, state) => Text(state.dataOrNull?.note ?? ''),
);
```

The binder attaches itself to the cache, which is how `live: true` finds it. Keep the reference if you will ever tear the runtime down. With `live: true` on, a builder takes a reference on each channel its entities are pushed on, and the manager shares one socket per endpoint between every builder that asked. Toggling `live` opens or releases the channel and never refetches. Frames land through the cache's commit scheduler, so a burst of them costs one rebuild per frame.

The `principal:` line is the one to get right. The manager opens each socket for a principal, and the binder checks that the manager's principal equals the cache's. When they differ, the binder reports it and fails closed: every frame is dropped, because those sockets carry another identity's data. Passing `() => cache.principal` keeps the two equal, including across a sign-in or sign-out. `revive:` retries abandoned sockets when the network comes back, with the same `ConnectivityPlusSignal` the scope uses for reconnect revalidation. Call `binder.dispose()` if you tear the runtime down.

## Mutations

```dart
Widget saveButton(String id, String note) => ForgeMutationBuilder(
  mutation: updateOrder,
  optimistic: (args) => OptimisticUpdate(
    (order) => order.copyWith(note: args.note),
    key: entityKey('Order', id),
  ),
  builder: (context, m) => FilledButton(
    onPressed: m.isPending ? null : () => m.mutate(UpdateOrderArgs(id: id, note: Assign(note))),
    child: const Text('Save'),
  ),
);
```

Pass `key:`. A generated `PATCH` invalidates only the collection (`Order[]`), so the runtime can't tell which record to patch on its own. Without a key the cache reports a `StateError` through `onError` under the context `optimistic`, and the write goes out with no optimism.

An optional field of a PATCH body is a `Value`. It defaults to `const Unchanged()`, which leaves the field out of the request, and `Assign(x)` sets it (`Assign(null)` clears it on the server). A model's `copyWith` takes the same `Value<T>?` for each nullable field, which is why `args.note` goes straight into it above and `order.copyWith(note: Assign('gift'))` works anywhere else. An operation with no parameters takes `NoArgs`.

`m.mutate` never throws. A failure is recorded in `m.state` and the future resolves with null, so the spelling an `onPressed` uses can't raise an unhandled error. `m.mutateAsync` records the same state and rethrows, for code that must not continue after a failed write. Two overlapping calls settle in favour of the later one.

The status belongs to the client and principal the call ran for. A `setPrincipal` or a new client for the builder (a different `ForgeScope` client or `client:` argument) puts it back to idle, and a call that was in flight across the change isn't recorded when it lands. The write itself still happens, and the caller of `mutate` still gets its result.

## Invalidation

```dart
Future<void> refresh(BuildContext context, String id) async {
  context.forgeInvalidate(listOrders); // every cached variant
  context.forgeInvalidate(getOrder, GetOrderArgs(id: id)); // exactly that one
  await context.forgeRefetch(listOrders); // refetch now and wait
  context.forgeInvalidateTags(['Order[]']); // the tag graph directly
}
```

This is a port of the TypeScript `useInvalidate`. It matches every query the cache remembers: settled ones, failed ones, and ones still on their first fetch. A mounted match refetches in the next batch. An unmounted match is only marked stale, and refetches when something next mounts it, so a list on a screen the user left costs nothing until they go back. `forgeRefetch` does the mounted ones straight away and waits for them, and throws if one fails.

`invalidateBinding(client, binding, args)` and `refetchBinding(client, binding, args)` do the same against an explicit cache, for code with no `BuildContext`.

## Several queries, and side effects

`ForgeQueriesBuilder` hands you a `ForgeQueriesState`: each query's state, plus one combined `status` (failure, then loading, then idle, then success).

```dart
Widget dashboard() => ForgeQueriesBuilder(
  queries: [listOrders(const NoArgs()), getOrder(const GetOrderArgs(id: '7'))],
  builder: (context, s) => switch (s.status) {
    ForgeCombinedStatus.idle || ForgeCombinedStatus.loading => const CircularProgressIndicator(),
    ForgeCombinedStatus.failure => ErrorView(s.error!, null),
    ForgeCombinedStatus.success => OrderView(s.dataAt<Order>(1)),
  },
);
```

`ForgeListener` runs a side effect on each transition without rebuilding its child:

```dart
Widget withFailureToast(String id) => ForgeListener<Order>(
  query: getOrder(GetOrderArgs(id: id)),
  listener: (context, previous, next) => switch (next) {
    QueryFailure(:final error) =>
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('$error'))),
    QueryIdle() || QueryLoading() || QuerySuccess() => null,
  },
  child: const App(),
);
```

The listener never runs while the tree is being built. A transition that arrives then is delivered after the frame.

## Local state

```dart
String? noSelection() => null;

Order? selectedOrderOf(ForgeReader read) {
  final id = read.state(selectedId);
  return id == null ? null : read.query(getOrder(GetOrderArgs(id: id))).dataOrNull;
}

const selectedId = ForgeStateKey<String?>(noSelection, debugLabel: 'selectedId');
const selectedOrder = ForgeComputedKey<Order?>(selectedOrderOf, debugLabel: 'selectedOrder');

Widget selectedNote(BuildContext context) => ListenableBuilder(
  listenable: context.forgeComputed(selectedOrder),
  builder: (context, _) => Text(context.forgeComputed(selectedOrder).value?.note ?? ''),
);

void select(BuildContext context, String id) => context.forgeState(selectedId).value = id;
```

Keys are top-level, like bindings. Each `ForgeScope` owns its own state and computed values, and disposes them, along with the queries they read, when it goes away. A computed value can read states, other computed values, any `ValueListenable` (through `read.listen`) and queries. It notifies only when its result changes. That's enough for server data plus some UI state, and it stops there.

A few details worth knowing:

- Read the value inside a `ListenableBuilder`, as above, and not through a `ValueListenableBuilder`. A `ValueListenableBuilder` caches the last value it read. If a recompute fails while the scope moves to another client (an account switch, say), the computed value starts rethrowing, and the cached copy would keep showing something derived from the old client's data.
- Two `const` keys built from the same function and the same label are the same key, so they share one state per scope. Give keys that must differ a different `debugLabel`, or a different function.
- A recompute that throws against the same client keeps the previous value and reports the error through `FlutterError.reportError`. One that throws during a client change fails the computed value instead, until a recompute succeeds.
- Recomputes are batched per scope, and nothing in a batch reads a value that is waiting to be recomputed. That guarantee holds within one batch. When a single change outside any computation notifies two values and one reads the other, the reader can run once against the other's previous value before it recomputes again.

## Offline

`forge_client_offline` (in `dart-packages/forge_client_offline`) keeps the cache's snapshot and a queue of unsent writes in an encrypted database per principal. This package depends on neither it nor its storage, but its `OfflineClient` is built for the two widgets here: it is the `OutboxFailureSource` for `ForgeOutboxListener`, and its `restore` is the callback `ForgeRestoreBoundary` wants. Add `import 'package:forge_client_offline/forge_client_offline.dart';` next to the imports above.

Open the client before `runApp`, hand its cache to the scope, and pass the client to the widgets:

```dart
Future<void> runOffline(EncryptedSqliteStorage storage, String userId) async {
  final offline = await OfflineClient.open(
    transport: RestTransport(baseUrl: Uri.parse('https://api.example.com')),
    entities: entities,
    operations: operations,
    storage: storage,
    principal: userId,
    connectivity: ConnectivityPlusSignal(),
    commitScheduler: frameCommitScheduler(),
  );
  runApp(ForgeScope(client: offline.cache, child: offlineApp(offline)));
}

Widget offlineApp(OfflineClient offline) => ForgeRestoreBoundary(
  key: ValueKey(offline.cache.principal),
  restore: offline.restore,
  placeholder: const SplashScreen(),
  child: ForgeOutboxListener(
    source: offline,
    onFailure: (context, failure) {
      if (failure is! OutboxFailure) return;
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text('A saved change was rejected: $failure')));
    },
    child: const App(),
  ),
);
```

`OfflineClient.open` already restores the snapshot and the outbox before it returns, so on the first launch the boundary has nothing left to do. It earns its place when the principal changes: call `offline.cache.setPrincipal(next)`, rebuild the widget, and the new key shows the placeholder and runs `offline.restore`, which waits for the principal switch and restores the new principal's session once. `ForgeRestoreBoundary` shows the placeholder until `restore` completes, then the child. It runs `restore` once per mount, after the frame that mounts it, whatever closure later rebuilds pass.

A restored query comes back marked stale, so the child mounts, sees it stale and refetches. A restore that throws still shows the child, on whatever the cache holds. The error goes to `onError` when you pass one and to `FlutterError.reportError` otherwise.

`ForgeOutboxListener` calls `onFailure` for each failure, with a live context, and never rebuilds its child. The stream is typed `Object` here so this package needn't know the offline package, which is why the example checks `failure is OutboxFailure` before it uses it. Switch over the sealed type to pick a screen per case. The listener calls the latest `onFailure`, moves to a new `source` when you pass one, and cancels its subscription when it leaves the tree. A source can emit from inside a build, so a failure that arrives then is held until the frame is done and delivered in order. None are dropped, unless the listener itself was removed in the meantime.

Two things to get right when accounts change. Call `setPrincipal` before you swap the credentials your transport sends, and drop any `OutboxFailure` your screens hold. Each one carries the previous account's server response, and calling `retry()` on it afterwards would reach the wrong outbox. The offline package's README has the details, and what gets retried, how keys are held and what its limits are.

Without the offline package you can still use both widgets with your own `restore` (read `client.session?.readSnapshot()` after `await client.idle`, then `hydrate(client, stored, principal: client.principal, operations: operations, stale: true)`) and any `OutboxFailureSource`.

## Plain Dart access

Repositories, isolates and background work don't need a widget:

```dart
Future<void> plainDart() async {
  final order = await getOrder(const GetOrderArgs(id: '7')).fetch(client);
  debugPrint('${order.note}');

  final subscription = getOrder(const GetOrderArgs(id: '7'))
      .watch(client)
      .listen((state) => debugPrint('${state.dataOrNull?.note}'));
  await subscription.cancel();
}
```

Both are `forge_client` APIs, `QueryRef.fetch` and `QueryRef.watch`. Every widget here is built on the second one. `fetch` serves a settled, fresh query from the cache without a request.

## Testing

`package:forge_client_flutter/testing.dart` has a `FakeTransport` that answers on the microtask queue, and a `FakeFocusSignal` and `FakeConnectivitySignal` you pass to `ForgeScope`, so a widget test never touches a platform channel.

## Differences from the React adapter

- Dart's invalidate also restarts a first fetch that is still in flight, and discards the answer it was about to deliver.
- There is no StrictMode and no server rendering in Flutter. The React tests for them are kept as skipped tests, each with the reason.

## Framework features used

Every API listed here is exercised by this package's tests on Flutter 3.47.5. The release notes for earlier versions were not re-read, so a version older than 3.47 is untested.

- `AppLifecycleListener` (Flutter 3.13) for focus revalidation, never `WidgetsBindingObserver`. Tests drive the lifecycle through `SystemChannels.lifecycle` on `TestDefaultBinaryMessenger`, which generates the intermediate states the engine would.
- `SchedulerBinding.scheduleFrameCallback`, `SchedulerBinding.framesEnabled` and `SchedulerBinding.addPostFrameCallback` for per-frame commits and for deferring work that arrives mid-build.
- `InheritedWidget`, with `dependOnInheritedWidgetOfExactType` for builders and the non-subscribing `getInheritedWidgetOfExactType` for event handlers.
- `ValueNotifier`, `ChangeNotifier` and `ValueListenable` for local state, consumed with `ListenableBuilder`.
- Dart: sealed classes and exhaustive switches (3.0), null-aware map entries (3.8) and dot shorthands (3.10).
