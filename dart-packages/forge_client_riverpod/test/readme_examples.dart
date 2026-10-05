// Every ```dart block of README.md, pasted verbatim, so `flutter analyze`
// compiles them against the real APIs. test/readme_test.dart fails when a
// block in the README is not found here, so the two cannot drift. Not a test
// file itself: it only has to compile.
//
// Paste each README block below its marker. The only difference allowed is
// the generated package's import, which the README spells
// `package:orders_forge_client/orders_forge_client.dart` and this file
// spells `support/readme_stubs.dart`.
// ignore_for_file: unused_import

//README-BLOCK set-up
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_riverpod/forge_client_riverpod.dart';
import 'support/readme_stubs.dart';

final client = configureClient(
  transport: RestTransport(baseUrl: Uri.parse('https://api.example.com')),
  entities: entities,
  commitScheduler: frameCommitScheduler(),
);

void main() => runApp(ProviderScope(
  overrides: [forgeClientProvider.overrideWithValue(client)],
  child: const App(),
));

//README-BLOCK queries
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

//README-BLOCK query-state
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

//README-BLOCK live
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

//README-BLOCK mutations
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
                optimistic: OptimisticUpdate((order) => order.copyWith(note: Assign(note))),
              ),
      child: switch (status) {
        MutationIdle() || MutationSuccess() => const Text('Save'),
        MutationPending() => const Text('Saving'),
        MutationFailure() => const Text('Retry'),
      },
    );
  }
}

//README-BLOCK invalidation
Future<void> refreshOrders(WidgetRef ref, String id) async {
  final client = ref.read(forgeClientProvider);
  invalidateBinding(client, listOrders); // every cached variant
  invalidateBinding(client, getOrder, GetOrderArgs(id: id)); // exactly that one
  await refetchBinding(client, listOrders); // refetch now and wait
}
