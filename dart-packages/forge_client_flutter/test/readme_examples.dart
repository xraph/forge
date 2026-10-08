// dart format off
// Every ```dart block of README.md, pasted verbatim, so `flutter analyze`
// compiles them against the real APIs. test/readme_test.dart fails when a
// block in the README is not found here, so the two cannot drift. Not a test
// file itself: it only has to compile.
//
// Paste each README block below its marker. The only difference allowed is
// the generated package's import, which the README spells
// `package:orders_forge_client/orders_forge_client.dart` and this file
// spells `support/readme_stubs.dart`, and the import of
// `forge_client_offline`, which the README's Offline section tells the reader
// to add and which this file keeps with the other imports.
// ignore_for_file: unused_import

import 'package:forge_client_offline/forge_client_offline.dart';

//README-BLOCK set-up
import 'package:flutter/material.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';
import 'support/readme_stubs.dart';

final client = configureClient(
  transport: RestTransport(baseUrl: Uri.parse('https://api.example.com')),
  entities: entities,
  commitScheduler: frameCommitScheduler(),
);

void main() => runApp(ForgeScope(client: client, child: const App()));

//README-BLOCK queries
Widget orderView(String id) => ForgeQueryBuilder(
  query: getOrder(GetOrderArgs(id: id)),
  builder: (context, state) => switch (state) {
    QueryIdle() || QueryLoading() => const CircularProgressIndicator(),
    QuerySuccess(:final data) => OrderView(data),
    QueryFailure(:final error, :final previous) => ErrorView(error, previous),
  },
);

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

Widget liveOrder(String id) => ForgeQueryBuilder(
  query: getOrder(GetOrderArgs(id: id)),
  live: true,
  builder: (context, state) => Text(state.dataOrNull?.note ?? ''),
);

//README-BLOCK mutations
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

//README-BLOCK invalidation
Future<void> refresh(BuildContext context, String id) async {
  context.forgeInvalidate(listOrders); // every cached variant
  context.forgeInvalidate(getOrder, GetOrderArgs(id: id)); // exactly that one
  await context.forgeRefetch(listOrders); // refetch now and wait
  context.forgeInvalidateTags(['Order[]']); // the tag graph directly
}

//README-BLOCK combined
Widget dashboard() => ForgeQueriesBuilder(
  queries: [listOrders(const NoArgs()), getOrder(const GetOrderArgs(id: '7'))],
  builder: (context, s) => switch (s.status) {
    ForgeCombinedStatus.idle || ForgeCombinedStatus.loading => const CircularProgressIndicator(),
    ForgeCombinedStatus.failure => ErrorView(s.error!, null),
    ForgeCombinedStatus.success => OrderView(s.dataAt<Order>(1)),
  },
);

//README-BLOCK listener
Widget withFailureToast(String id) => ForgeListener<Order>(
  query: getOrder(GetOrderArgs(id: id)),
  listener: (context, previous, next) => switch (next) {
    QueryFailure(:final error) =>
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('$error'))),
    QueryIdle() || QueryLoading() || QuerySuccess() => null,
  },
  child: const App(),
);

//README-BLOCK local-state
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

//README-BLOCK offline
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

//README-BLOCK plain-dart
Future<void> plainDart() async {
  final order = await getOrder(const GetOrderArgs(id: '7')).fetch(client);
  debugPrint('${order.note}');

  final subscription = getOrder(const GetOrderArgs(id: '7'))
      .watch(client)
      .listen((state) => debugPrint('${state.dataOrNull?.note}'));
  await subscription.cancel();
}

