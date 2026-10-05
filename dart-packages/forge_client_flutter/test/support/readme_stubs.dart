// Stands in for the package `forge client generate --language dart` writes,
// and for the app widgets the README examples mention, so the README's code
// compiles. test/readme_examples.dart imports it where the README imports the
// generated package.
import 'package:flutter/material.dart';
import 'package:forge_client/forge_client.dart';

final class Order {
  const Order({required this.id, this.note});

  final String id;
  final String? note;

  static Order fromClient(Object? client) {
    final json = client! as Map<String, Object?>;
    return Order(id: json['id']! as String, note: json['note'] as String?);
  }

  Json toClient() => {'id': id, 'note': note};

  Order copyWith({String? note}) => Order(id: id, note: note ?? this.note);
}

List<Order> ordersFromClient(Object? client) =>
    List<Order>.unmodifiable((client! as List<Object?>).map(Order.fromClient));

final class GetOrderArgs implements OperationArgs {
  const GetOrderArgs({required this.id});

  final String id;

  @override
  TagContext toTagContext() => TagContext(path: {'id': id});
}

final class ListOrdersArgs implements OperationArgs {
  const ListOrdersArgs();

  @override
  TagContext toTagContext() => TagContext.empty;
}

final class UpdateOrderArgs implements OperationArgs {
  const UpdateOrderArgs({required this.id, required this.note});

  final String id;
  final String note;

  @override
  TagContext toTagContext() => TagContext(path: {'id': id}, body: {'note': note});
}

const EntitySchema entities = {'Order': EntityMeta(idField: 'id')};

const _getOrder = OperationMeta(
  id: 'op_get_order',
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  rootType: 'Order',
  provides: ['Order:{id}'],
);

const _listOrders = OperationMeta(
  id: 'op_list_orders',
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  rootType: 'Order',
  provides: ['Order[]'],
);

const _updateOrder = OperationMeta(
  id: 'op_update_order',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  rootType: 'Order',
  provides: ['Order:{id}'],
  invalidates: ['Order[]', 'Order:{id}'],
);

const Map<String, OperationMeta> operations = {
  'op_get_order': _getOrder,
  'op_list_orders': _listOrders,
  'op_update_order': _updateOrder,
};

const List<StreamBinding> streams = [
  EntityStreamBinding(
    channel: '/ws/orders',
    message: 'order.updated',
    entity: 'Order',
    intent: StreamIntent.patch,
  ),
];

final getOrder = query<Order, GetOrderArgs>(_getOrder, Order.fromClient);
final listOrders = query<List<Order>, ListOrdersArgs>(_listOrders, ordersFromClient);
final updateOrder = mutation<Order, UpdateOrderArgs, Order>(
  _updateOrder,
  Order.fromClient,
  entityFromClient: Order.fromClient,
  entityToClient: (order) => order.toClient(),
);

// App widgets the examples mention.
class App extends StatelessWidget {
  const App({super.key});

  @override
  Widget build(BuildContext context) => const SizedBox();
}

class SplashScreen extends StatelessWidget {
  const SplashScreen({super.key});

  @override
  Widget build(BuildContext context) => const SizedBox();
}

class OrderView extends StatelessWidget {
  const OrderView(this.order, {super.key});

  final Order order;

  @override
  Widget build(BuildContext context) => Text(order.note ?? '');
}

class ErrorView extends StatelessWidget {
  const ErrorView(this.error, this.previous, {super.key});

  final Object error;
  final Order? previous;

  @override
  Widget build(BuildContext context) => Text('$error');
}
