import 'dart:async';

import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/forge_client_flutter.dart';
import 'package:forge_client_flutter/testing.dart';

/// The two entities every test renders. Ported from client-react's harness.
const EntitySchema schema = {
  'Order': EntityMeta(idField: 'id', fields: {'customer': 'Customer'}),
  'Customer': EntityMeta(idField: 'id'),
};

const opListOrders = OperationMeta(
  id: 'op_list_orders',
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  rootType: 'Order',
  provides: ['Order[]'],
);

/// A read that declares no tags at all, which is the shape most generated
/// reads have today. A list-level invalidation cannot reach it.
const opSearchOrders = OperationMeta(
  id: 'op_search_orders',
  method: 'GET',
  path: '/orders/search',
  entity: 'Order',
  rootType: 'Order',
);

const opGetOrder = OperationMeta(
  id: 'op_get_order',
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  rootType: 'Order',
  provides: ['Order:{id}'],
);

const opPatchOrder = OperationMeta(
  id: 'op_patch_order',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  rootType: 'Order',
  provides: ['Order:{id}'],
  invalidates: ['Order[]', 'Order:{id}'],
);

const opCreateOrder = OperationMeta(
  id: 'op_create_order',
  method: 'POST',
  path: '/orders',
  entity: 'Order',
  rootType: 'Order',
  invalidates: ['Order[]'],
);

/// A hand-written model with the generated shape: `fromClient`, `toClient`,
/// `copyWith` and value equality.
final class Order {
  const Order({required this.id, required this.total});

  final int id;
  final int total;

  static Order fromClient(Object? client) {
    final json = client! as Map<String, Object?>;
    return Order(id: json['id']! as int, total: json['total']! as int);
  }

  Json toClient() => {'id': id, 'total': total};

  Order copyWith({int? total}) => Order(id: id, total: total ?? this.total);

  @override
  bool operator ==(Object other) =>
      other is Order && other.id == id && other.total == total;

  @override
  int get hashCode => Object.hash(id, total);

  @override
  String toString() => 'Order($id, $total)';
}

List<Order> ordersFromClient(Object? client) =>
    List<Order>.unmodifiable((client! as List<Object?>).map(Order.fromClient));

/// One client-shaped order row, as a transport returns it.
Json order(Object? id, int total) => {'id': id, 'total': total};

final class OrderArgs implements OperationArgs {
  const OrderArgs(this.id);

  final int id;

  @override
  TagContext toTagContext() => TagContext(path: {'id': id});
}

final class ListOrdersArgs implements OperationArgs {
  const ListOrdersArgs({this.status});

  final String? status;

  @override
  TagContext toTagContext() =>
      status == null ? TagContext.empty : TagContext(query: {'status': status});
}

final class PatchOrderArgs implements OperationArgs {
  const PatchOrderArgs(this.id, {this.total});

  final int id;
  final int? total;

  @override
  TagContext toTagContext() =>
      TagContext(path: {'id': id}, body: {'total': ?total});
}

final class CreateOrderArgs implements OperationArgs {
  const CreateOrderArgs(this.total);

  final int total;

  @override
  TagContext toTagContext() => TagContext(body: {'total': total});
}

/// The bindings, exactly as a generated package declares them.
final listOrders = query<List<Order>, ListOrdersArgs>(
  opListOrders,
  ordersFromClient,
);
final searchOrders = query<List<Order>, ListOrdersArgs>(
  opSearchOrders,
  ordersFromClient,
);
final getOrder = query<Order, OrderArgs>(opGetOrder, Order.fromClient);
final patchOrder = mutation<Order, PatchOrderArgs, Order>(
  opPatchOrder,
  Order.fromClient,
  entityFromClient: Order.fromClient,
  entityToClient: (order) => order.toClient(),
);
final createOrder = mutation<Order, CreateOrderArgs, Order>(
  opCreateOrder,
  Order.fromClient,
);

/// An error whose message is the whole of its `toString`, so a test can
/// render it and find it.
final class Boom implements Exception {
  const Boom(this.message);

  final String message;

  @override
  String toString() => message;
}

final class Harness {
  Harness(this.cache, this.transport, this.scheduler);

  final QueryCache cache;
  final FakeTransport transport;

  /// Invalidation batches run when the test flushes this, not on a microtask.
  final ManualScheduler scheduler;
}

Harness harness(
  FutureOr<Object?> Function(TransportRequest request, int call) handler, {
  Clock clock = realClock,
  CommitScheduler? commitScheduler,
}) {
  final transport = FakeTransport(handler);
  final scheduler = ManualScheduler();
  final cache = QueryCache(
    transport: transport,
    entities: schema,
    scheduler: scheduler,
    commitScheduler: commitScheduler,
    clock: clock,
  );
  return Harness(cache, transport, scheduler);
}

Object? idOf(TransportRequest request) => request.args.path['id'];

/// Runs queued microtasks and frames until the fake transport's responses
/// have landed and every builder has rebuilt.
Future<void> settle(WidgetTester tester) async {
  for (var i = 0; i < 6; i++) {
    await tester.pump();
  }
}

/// Drives the app lifecycle the way the engine does, through the lifecycle
/// channel, so the binding generates every intermediate state and
/// [AppLifecycleListener] sees a legal sequence.
Future<void> setLifecycle(WidgetTester tester, AppLifecycleState state) async {
  await tester.binding.defaultBinaryMessenger.handlePlatformMessage(
    SystemChannels.lifecycle.name,
    SystemChannels.lifecycle.codec.encodeMessage(state.toString()),
    (_) {},
  );
}

String stateStatusOf(QueryState<Object?> state) => switch (state) {
  QueryIdle() => 'idle',
  QueryLoading() => 'loading',
  QuerySuccess() => 'success',
  QueryFailure() => 'error',
};

String orderText(QueryState<Order> state) =>
    '${stateStatusOf(state)}:${state.dataOrNull?.total ?? '-'}';

String listText(QueryState<List<Order>> state) =>
    '${stateStatusOf(state)}:${state.dataOrNull?.map((o) => o.total).join(',') ?? '-'}';

Widget ltr(Widget child) => Directionality(textDirection: .ltr, child: child);

/// A scope over [h]'s cache with fake signals, so no test touches the
/// connectivity platform channel, and with text direction set.
Widget scope(
  Harness h,
  Widget child, {
  FakeFocusSignal? focus,
  FakeConnectivitySignal? connectivity,
}) => ForgeScope(
  client: h.cache,
  focus: focus ?? FakeFocusSignal(),
  connectivity: connectivity ?? FakeConnectivitySignal(),
  child: ltr(child),
);

/// A harness signed in as alice whose server answers by who is signed in:
/// an order's total is 100 + id for alice and 200 + id for bob, and a patch
/// answers 150 for alice and 250 for bob. A test that switches to bob can
/// then tell any of alice's data apart by its hundreds digit.
Harness principalHarness() {
  late final Harness h;
  h = harness((request, _) {
    final base = h.cache.principal == 'bob' ? 200 : 100;
    final id = idOf(request)! as int;
    return order(id, request.meta.method == 'PATCH' ? base + 50 : base + id);
  });
  h.cache.setPrincipal('alice');
  return h;
}

/// Whether [text] shows any of alice's data from [principalHarness].
bool showsAlice(String text) => RegExp(r'\b1\d\d\b').hasMatch(text);
