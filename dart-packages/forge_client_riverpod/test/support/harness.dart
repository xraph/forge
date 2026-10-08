import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_flutter/testing.dart';
import 'package:forge_client_riverpod/forge_client_riverpod.dart';

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

final listOrders = query<List<Order>, ListOrdersArgs>(
  opListOrders,
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
  final ManualScheduler scheduler;
}

Harness harness(
  FutureOr<Object?> Function(TransportRequest request, int call) handler, {
  Clock clock = realClock,
}) {
  final transport = FakeTransport(handler);
  final scheduler = ManualScheduler();
  final cache = QueryCache(
    transport: transport,
    entities: schema,
    scheduler: scheduler,
    clock: clock,
  );
  return Harness(cache, transport, scheduler);
}

Object? idOf(TransportRequest request) => request.args.path['id'];

/// A container over [h]'s cache with fake signals and no automatic retry,
/// disposed when the test ends.
ProviderContainer containerFor(
  Harness h, {
  FakeFocusSignal? focus,
  FakeConnectivitySignal? connectivity,
}) {
  final container = ProviderContainer(
    overrides: [
      forgeClientProvider.overrideWithValue(h.cache),
      forgeFocusSignalProvider.overrideWithValue(focus ?? FakeFocusSignal()),
      forgeConnectivitySignalProvider.overrideWithValue(
        connectivity ?? FakeConnectivitySignal(),
      ),
    ],
    retry: (_, _) => null,
  );
  addTearDown(container.dispose);
  return container;
}

/// Lets the fake transport's microtasks and Riverpod's scheduled disposals
/// run.
Future<void> settle() async {
  for (var i = 0; i < 10; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

Future<void> invalidate(Harness h, List<String> tags) async {
  h.cache.invalidate(tags);
  h.scheduler.flush();
  await settle();
}
