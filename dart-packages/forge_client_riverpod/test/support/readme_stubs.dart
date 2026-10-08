// Stands in for the package `forge client generate --language dart --hooks`
// writes, and for the app widgets the README examples mention, so the
// README's code compiles. A copy of
// forge_client_flutter's test/support/readme_stubs.dart, with only the app
// widget this README needs. test/readme_examples.dart imports it where the
// README imports the generated package.
//
// The generator is the source of truth, and this file copies its output
// shapes; it does not invent any:
//   - internal/client/generators/dart/models.go: the model class (optional
//     fields are nullable, and copyWith takes a Value<T>? for each of them),
//   - internal/client/generators/dart/bindings.go: the binding line, the
//     private _fromClient decoder and the Args class (an optional body field
//     is a Value<T> defaulting to const Unchanged(), and an operation with no
//     parameters or body uses NoArgs),
//   - internal/client/generators/dart/ops.go: the operations, entities and
//     streams tables,
//   - internal/client/generators/dart/support.go: the decode and equality
//     helpers, and the symbols re-exported from forge_client.
//
// When the generator changes one of those shapes, change it here and the
// README follows.
//
// Generated code is copied as written, so the lints that flag its style are
// off here.
// ignore_for_file: unnecessary_this, use_null_aware_elements
import 'package:flutter/material.dart';
import 'package:forge_client/forge_client.dart'
    show
        Assign,
        EntityMeta,
        EntitySchema,
        EntityStreamBinding,
        Json,
        NoArgs,
        OperationArgs,
        OperationMeta,
        StreamBinding,
        StreamIntent,
        TagContext,
        Unchanged,
        Value,
        decodeCached,
        mutation,
        query;

export 'package:forge_client/forge_client.dart'
    show Assign, Json, NoArgs, Unchanged, Value, decodeCached;

// support.dart helpers, as renderSupport writes them.
Map<String, Object?> decodeObject(Object? value) =>
    (value as Map<Object?, Object?>).cast<String, Object?>();

List<T> decodeList<T>(Object? value, T Function(Object?) item) =>
    List<T>.unmodifiable((value as List<Object?>).map(item));

T? decodeNullable<T>(Object? value, T Function(Object?) decode) =>
    value == null ? null : decode(value);

bool deepEquals(Object? a, Object? b) => a == b;

bool valueEquals<T>(Value<T> a, Value<T> b) => switch ((a, b)) {
  (Unchanged<T>(), Unchanged<T>()) => true,
  (Assign<T>(value: final x), Assign<T>(value: final y)) => deepEquals(x, y),
  _ => false,
};

int valueHash<T>(Value<T> value) => switch (value) {
  Unchanged<T>() => 0,
  Assign<T>(value: final v) => Object.hash(1, v),
};

// models/order.dart: a required `id` and an optional `note`.
final class Order {
  const Order({required this.id, this.note});

  factory Order.fromClient(Object? client) {
    final json = decodeObject(client);
    return Order(
      id: json['id']! as String,
      note: decodeNullable(json['note'], (v0) => v0! as String),
    );
  }

  final String id;

  final String? note;

  Order copyWith({String? id, Value<String>? note}) => Order(
    id: id ?? this.id,
    note: switch (note) {
      Assign(:final value) => value,
      _ => this.note,
    },
  );

  Json toClient() => <String, Object?>{
    'id': id,
    if (note case final v?) 'note': v,
  };

  @override
  bool operator ==(Object other) =>
      identical(this, other) ||
      other is Order && this.id == other.id && this.note == other.note;

  @override
  int get hashCode => Object.hashAll([id, note]);
}

// ops.dart: one constant per operation, then the tables.
const opGetOrder = OperationMeta(
  id: 'op_get_order',
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  rootType: 'Order',
  provides: ['Order:{id}'],
);

const opListOrders = OperationMeta(
  id: 'op_list_orders',
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  rootType: 'Order',
  provides: ['Order[]'],
);

const opUpdateOrder = OperationMeta(
  id: 'op_update_order',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  rootType: 'Order',
  provides: ['Order:{id}'],
  invalidates: ['Order[]'],
);

const Map<String, OperationMeta> operations = {
  'op_get_order': opGetOrder,
  'op_list_orders': opListOrders,
  'op_update_order': opUpdateOrder,
};

const EntitySchema entities = {'Order': EntityMeta(idField: 'id')};

const List<StreamBinding> streams = [
  EntityStreamBinding(
    channel: '/ws/orders',
    message: 'order.updated',
    entity: 'Order',
    intent: StreamIntent.patch,
  ),
];

// bindings/get_order.dart. In the generated package each binding file holds
// its own private `_fromClient`; here they are named per binding.
final getOrder = query<Order, GetOrderArgs>(opGetOrder, _getOrderFromClient);

Order _getOrderFromClient(Object? client) =>
    decodeCached(Order.fromClient, client);

final class GetOrderArgs implements OperationArgs {
  const GetOrderArgs({required this.id});

  final String id;

  @override
  TagContext toTagContext() => TagContext(path: {'id': id});

  @override
  bool operator ==(Object other) =>
      other is GetOrderArgs && this.id == other.id;

  @override
  int get hashCode => Object.hashAll([id]);
}

// bindings/list_orders.dart: no parameters and no body, so NoArgs.
final listOrders = query<List<Order>, NoArgs>(
  opListOrders,
  _listOrdersFromClient,
);

List<Order> _listOrdersFromClient(Object? client) =>
    decodeList(client, (v0) => decodeCached(Order.fromClient, v0));

// bindings/update_order.dart: a path parameter, and a flattened PATCH body
// whose optional field is a Value.
final updateOrder = mutation<Order, UpdateOrderArgs, Order>(
  opUpdateOrder,
  _updateOrderFromClient,
  entityFromClient: Order.fromClient,
  entityToClient: (e) => e.toClient(),
);

Order _updateOrderFromClient(Object? client) =>
    decodeCached(Order.fromClient, client);

final class UpdateOrderArgs implements OperationArgs {
  const UpdateOrderArgs({required this.id, this.note = const Unchanged()});

  final String id;

  final Value<String> note;

  @override
  TagContext toTagContext() => TagContext(
    path: {'id': id},
    body: <String, Object?>{if (note case Assign(:final value)) 'note': value},
  );

  @override
  bool operator ==(Object other) =>
      other is UpdateOrderArgs &&
      this.id == other.id &&
      valueEquals(this.note, other.note);

  @override
  int get hashCode => Object.hashAll([id, valueHash(note)]);
}

// The app widget the examples mention.
class App extends StatelessWidget {
  const App({super.key});

  @override
  Widget build(BuildContext context) => const SizedBox();
}
