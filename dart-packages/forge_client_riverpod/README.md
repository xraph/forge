# forge_client_riverpod

Riverpod 3 providers over `forge_client`, for apps already on Riverpod. If yours isn't, `forge_client_flutter` gives you everything here without a state library.

It uses `forge_client_flutter`'s installers and none of its widgets, so focus and reconnect revalidation behave exactly as they do under `ForgeScope`. No codegen.

## Install

Everything in `dart-packages/` is `1.0.0-dev` and `publish_to: none`, so there is no version to depend on yet. Depend on the packages by path:

```yaml
dependencies:
  flutter_riverpod: ^3.4.3
  forge_client:
    path: ../forge_client
  forge_client_riverpod:
    path: ../forge_client_riverpod
```

or straight from the repository:

```yaml
dependencies:
  flutter_riverpod: ^3.4.3
  forge_client:
    git:
      url: https://github.com/xraph/forge
      path: dart-packages/forge_client
      ref: <commit sha>
  forge_client_riverpod:
    git:
      url: https://github.com/xraph/forge
      path: dart-packages/forge_client_riverpod
      ref: <commit sha>
```

Put the same full commit sha in both `ref:` lines. Pub turns this package's own `path: ../forge_client` into a git dependency pinned to the commit it resolved, so your direct `forge_client` entry has to name that exact commit too, and a branch name or a missing `ref` fails to resolve.

You'll also need Flutter 3.47 or later and the package your API generated with `forge client generate --language dart --hooks`. The examples call it `orders_forge_client`, and use its names: `entities`, `streams`, `getOrder`, `listOrders`, `updateOrder` and the `...Args` classes.

This package re-exports the pieces of `forge_client_flutter` you call directly (`frameCommitScheduler`, `ConnectivityPlusSignal`, `invalidateBinding` and `refetchBinding`), so you don't import that package yourself.

## Set up

```dart
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_riverpod/forge_client_riverpod.dart';
import 'package:orders_forge_client/orders_forge_client.dart';

final client = configureClient(
  transport: RestTransport(baseUrl: Uri.parse('https://api.example.com')),
  entities: entities,
  commitScheduler: frameCommitScheduler(),
);

void main() => runApp(ProviderScope(
  overrides: [forgeClientProvider.overrideWithValue(client)],
  child: const App(),
));
```

Every provider here reads `forgeInstalledClientProvider`, which installs focus and reconnect revalidation on your client for as long as it lives. Without an override, `forgeClientProvider` falls back to the global client from `configureClient`.

## Queries

```dart
final getOrderProvider = queryProvider(getOrder);

class OrderNote extends ConsumerWidget {
  const OrderNote({super.key, required this.id});

  final String id;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return switch (ref.watch(getOrderProvider(GetOrderArgs(id: id)))) {
      AsyncData(:final value) => Text(value.note ?? ''),
      AsyncError(:final error) => Text('$error'),
      AsyncLoading() => const CircularProgressIndicator(),
    };
  }
}
```

The family keys on the query key, the same key the cache uses, so writing `GetOrderArgs(id: id)` inline is the same provider on every build. The query holds its mount while watched and releases it when the provider is disposed.

- `getOrderProvider(args, enabled: false)` stays `AsyncLoading` and fetches nothing.
- `getOrderProvider(args, live: true)` also applies server frames. It needs the wiring under Live updates.
- `getOrderProvider(args, staleTime: ...)` sets how long this call site treats the result as fresh.
- `ref.watch(getOrderProvider(args).select((v) => v.value?.note))` is Riverpod's own `select`.
- `ref.watch(getOrderProvider.state(args))` is the full `QueryState`.

When you want `isFetching`, `isOptimistic`, `syncStatus`, or the idle state of a disabled query, switch on the `QueryState`:

```dart
class OrderStatus extends ConsumerWidget {
  const OrderStatus({super.key, required this.id});

  final String id;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return switch (ref.watch(getOrderProvider.state(GetOrderArgs(id: id)))) {
      QueryIdle() => const Text('Not requested'),
      QueryLoading() => const CircularProgressIndicator(),
      QuerySuccess(:final data, :final isFetching) => Text('${data.note}${isFetching ? ' (refreshing)' : ''}'),
      QueryFailure(:final error, :final previous) => Text('$error, last seen: ${previous?.note}'),
    };
  }
}
```

`getOrderProvider(args)` is a plain `Provider<AsyncValue<Order>>`. It has no `.notifier` and no `.future`. To refetch, use `refetchBinding` or `invalidateBinding` (see Invalidation). A failed refetch is an `AsyncError` that keeps the previous value. Automatic retry is off for these providers, because a query's failure is data and a retried provider would refetch behind the cache's back.

The value always belongs to the current client and principal, and so does `.state`. A `setPrincipal` or a new `forgeClientProvider` starts the value provider over from `AsyncLoading` with no previous value, and rebuilds `.state` from the new principal's query, so `.value`, `select` and a read of `.state` never show the last user's data. That holds even for a read made while `setPrincipal` is still running, from a `.state` listener say.

One thing stays out of this package's hands. The `previous` argument Riverpod passes to a `ref.listen` or `container.listen` callback across the change can still be the last user's value, for the value provider and for `.state` alike. Act on `next`, and don't show `previous`.

## Live updates

`live: true` does nothing until your app builds the stream runtime, and nothing in this package builds it for you. Without it, the cache reports a `StateError` through its `onError` (context `live`) and the query carries on as a plain one. You need three pieces: the cache, a `SubscriptionManager` that owns the sockets, and a `StreamBinder` that decodes frames and writes them into the cache.

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

class LiveOrderNote extends ConsumerWidget {
  const LiveOrderNote({super.key, required this.id});

  final String id;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return switch (ref.watch(getOrderProvider(GetOrderArgs(id: id), live: true))) {
      AsyncData(:final value) => Text(value.note ?? ''),
      AsyncError(:final error) => Text('$error'),
      AsyncLoading() => const CircularProgressIndicator(),
    };
  }
}
```

Pass the returned `cache` to `forgeClientProvider.overrideWithValue` as in Set up. The binder attaches itself to the cache, which is how `live: true` finds it, so keep the reference if you'll ever tear the runtime down (`binder.dispose()`). A live provider takes a reference on each channel its entities are pushed on, and the manager shares one socket per endpoint between everything that asked. `live: true` and `live: false` are two providers over one cache query, so watching both still costs one request.

The `principal:` line is the one to get right. The manager opens each socket for a principal, and the binder checks that the manager's principal equals the cache's. When they differ, the binder reports it and fails closed: every frame is dropped, because those sockets carry another identity's data. Passing `() => cache.principal` keeps the two equal across a sign-in or sign-out. `revive:` retries abandoned sockets when the network comes back, with the same `ConnectivityPlusSignal` this package installs for reconnect revalidation.

## Mutations

```dart
final updateOrderProvider = mutationProvider(updateOrder);

class SaveButton extends ConsumerWidget {
  const SaveButton({super.key, required this.id, required this.note});

  final String id;
  final String note;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final status = ref.watch(updateOrderProvider);
    return FilledButton(
      onPressed: status is MutationPending<Order>
          ? null
          : () => ref.read(updateOrderProvider.notifier).mutate(
                UpdateOrderArgs(id: id, note: Assign(note)),
                optimistic: OptimisticUpdate(
                  (order) => order.copyWith(note: Assign(note)),
                  key: entityKey('Order', id),
                ),
              ),
      child: switch (status) {
        MutationIdle() || MutationSuccess() => const Text('Save'),
        MutationPending() => const Text('Saving'),
        MutationFailure() => const Text('Retry'),
      },
    );
  }
}
```

Pass `key:`. A generated `PATCH` invalidates only the collection (`Order[]`), so the runtime can't tell which record to patch on its own. Without a key the cache reports a `StateError` through `onError` under the context `optimistic`, and the write goes out with no optimism.

An optional field of a PATCH body is a `Value`. It defaults to `const Unchanged()`, which leaves the field out of the request, and `Assign(x)` sets it (`Assign(null)` clears it on the server). A model's `copyWith` takes the same `Value<T>?` for each nullable field. An operation with no parameters takes `NoArgs`.

`mutate` never throws. A failure is recorded in the provider's state and the future resolves with null, so an `onPressed` can't raise an unhandled error. `mutateAsync` records the same state and rethrows, for code that mustn't continue after a failed write. Both take `options: RequestOptions(headers: ..., cancel: ...)` for per-call headers and a cancel future, and `place:` for placement callbacks. When two calls overlap, the later one wins, and `reset()` returns the provider to idle and drops whatever was still in flight.

Everything watching one mutation provider shares its status. Declare a second provider, or use `ForgeMutationBuilder` from `forge_client_flutter`, when you need two independent ones. The provider auto-disposes, so watch it for as long as you care about the status: a call whose provider was disposed still runs to its end and resolves for its caller, and records nothing.

The status belongs to the client and principal the call ran for. A `setPrincipal` or a new `forgeClientProvider` puts the provider back to idle, and a call that was in flight across the change isn't recorded when it lands. The write itself still happens.

## Invalidation

Invalidation is a cache operation, so you call the plain functions with the client you already have:

```dart
Future<void> refreshOrders(WidgetRef ref, String id) async {
  final client = ref.read(forgeClientProvider);
  invalidateBinding(client, listOrders); // every cached variant
  invalidateBinding(client, getOrder, GetOrderArgs(id: id)); // exactly that one
  await refetchBinding(client, listOrders); // refetch now and wait
}
```

A mounted match refetches in the next batch, while an unmounted one is only marked stale and refetches when something next watches it, so a list on a screen you left costs nothing until you go back. `refetchBinding` starts the mounted ones straight away, waits for them, and throws if one fails.

## Inside a provider's build

The cache notifies its listeners synchronously, so a provider's `build` must not start cache work: no `mutate`, `refetch`, `invalidate` or `setPrincipal` in there, and no listening to a raw `QueryRef.watch` or `cache.watch` stream (a `StreamProvider` over one included). Watch queries through `queryProvider`. Its providers, and the mutation providers, hold back a state that arrives in the middle of a build and apply it once the build has returned. Those other calls can't, and Riverpod asserts in debug builds when they modify a provider mid-build.

Call `mutate` from an event handler. A call from a widget's `build` method won't assert (a state change made while the tree is building is applied straight after that build), but it writes on every rebuild, which is rarely what you meant.

A `watchPrincipalChanging` listener your app registers itself must not read this package's providers. It can run before they have moved to the new principal, and then it reads the previous one's state. Use `watchPrincipal`, which runs after the switch, when you need to read them.

## Testing

`package:forge_client_flutter/testing.dart` has a `FakeTransport` that answers on the microtask queue, plus a `FakeFocusSignal` and a `FakeConnectivitySignal`. Add `forge_client_flutter` to your `dev_dependencies` and override `forgeFocusSignalProvider` and `forgeConnectivitySignalProvider` with the fakes, next to `forgeClientProvider`, so a test never touches a platform channel.

## Riverpod 2

`app-flutter` is on Riverpod 2.6.1. This package needs Riverpod 3, so using it there means migrating the app first.
