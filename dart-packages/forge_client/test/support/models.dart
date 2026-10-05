import 'package:forge_client/forge_client.dart';

/// A hand-written model and args shaped like what the Dart generator (plan 02)
/// emits. Args have value equality, as the contracts require.
final class Order {
  const Order({required this.id, required this.total, this.status});

  final int id;
  final int total;
  final String? status;

  /// Counts decodes, so a test can prove the memo spared a decode.
  static int decodes = 0;

  static Order fromClient(Object? client) {
    decodes++;
    final json = client! as Json;

    return Order(
      id: json['id']! as int,
      total: json['total']! as int,
      status: json['status'] as String?,
    );
  }

  Json toClient() => {'id': id, 'total': total, 'status': ?status};

  Order copyWith({int? total, String? status}) =>
      Order(id: id, total: total ?? this.total, status: status ?? this.status);

  @override
  bool operator ==(Object other) =>
      other is Order &&
      other.id == id &&
      other.total == total &&
      other.status == status;

  @override
  int get hashCode => Object.hash(id, total, status);
}

/// A list decoder written the way plan 02 generates one: per element through
/// [decodeCached], so an untouched row keeps its model.
List<Order> ordersFromClient(Object? client) => [
  for (final row in client! as List<Object?>)
    decodeCached(Order.fromClient, row),
];

final class ListOrdersArgs implements OperationArgs {
  const ListOrdersArgs({this.status});

  final String? status;

  @override
  TagContext toTagContext() => TagContext(query: {'status': ?status});

  @override
  bool operator ==(Object other) =>
      other is ListOrdersArgs && other.status == status;

  @override
  int get hashCode => status.hashCode;
}

final class GetOrderArgs implements OperationArgs {
  const GetOrderArgs({required this.id});

  final int id;

  @override
  TagContext toTagContext() => TagContext(path: {'id': id});

  @override
  bool operator ==(Object other) => other is GetOrderArgs && other.id == id;

  @override
  int get hashCode => id.hashCode;
}

final class UpdateOrderArgs implements OperationArgs {
  const UpdateOrderArgs({required this.id, this.status = const Unchanged()});

  final int id;
  final Value<String> status;

  @override
  TagContext toTagContext() => TagContext(
    path: {'id': id},
    body: {if (status case Assign(:final value)) 'status': value},
  );

  @override
  bool operator ==(Object other) =>
      other is UpdateOrderArgs && other.id == id && other.status == status;

  @override
  int get hashCode => Object.hash(id, status);
}

final class CreateOrderArgs implements OperationArgs {
  const CreateOrderArgs({required this.total});

  final int total;

  @override
  TagContext toTagContext() => TagContext(body: {'total': total});

  @override
  bool operator ==(Object other) =>
      other is CreateOrderArgs && other.total == total;

  @override
  int get hashCode => total.hashCode;
}
