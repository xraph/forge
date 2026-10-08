// Stands in for the package `forge client generate --language dart --hooks`
// writes, and for the app functions the README examples mention, so the
// README's code compiles. test/readme_examples.dart imports it where the
// README imports the generated package.
//
// The generator is the source of truth, and this file copies its output
// shapes; it does not invent any (see forge_client_flutter's
// test/support/readme_stubs.dart for the same copy, which lists the generator
// files each shape comes from):
//   - the model class: optional fields are nullable, and copyWith takes a
//     Value<T>? for each of them,
//   - the binding line, the Args class (an optional body field is a Value<T>
//     defaulting to const Unchanged()) and the operations and entities tables,
//   - an operation whose route uses forge.WithIdempotency() carries
//     `idempotent: true`.
//
// Generated code is copied as written, so the lints that flag its style are
// off here.
// ignore_for_file: unnecessary_this, use_null_aware_elements
import 'package:forge_client/forge_client.dart'
    show
        Assign,
        EntityMeta,
        EntitySchema,
        Json,
        OperationArgs,
        OperationMeta,
        TagContext,
        Unchanged,
        Value,
        decodeCached,
        mutation;
import 'package:forge_client_offline/forge_client_offline.dart'
    show OutboxConflict, OutboxUncertain, OutboxUnauthorized, OutboxValidation, StorageReset;

export 'package:forge_client/forge_client.dart' show Assign, Json, NoArgs, Unchanged, Value;

Map<String, Object?> decodeObject(Object? value) =>
    (value as Map<Object?, Object?>).cast<String, Object?>();

T? decodeNullable<T>(Object? value, T Function(Object?) decode) =>
    value == null ? null : decode(value);

bool valueEquals<T>(Value<T> a, Value<T> b) => switch ((a, b)) {
  (Unchanged<T>(), Unchanged<T>()) => true,
  (Assign<T>(value: final x), Assign<T>(value: final y)) => x == y,
  _ => false,
};

int valueHash<T>(Value<T> value) => switch (value) {
  Unchanged<T>() => 0,
  Assign<T>(value: final v) => Object.hash(1, v),
};

// models/order.dart: a required `id` and an optional `note`.
final class Order {
  const Order({
    required this.id,
    this.note,
  });

  factory Order.fromClient(Object? client) {
    final json = decodeObject(client);
    return Order(
      id: json['id']! as String,
      note: decodeNullable(json['note'], (v0) => v0! as String),
    );
  }

  final String id;

  final String? note;

  Order copyWith({
    String? id,
    Value<String>? note,
  }) => Order(
    id: id ?? this.id,
    note: switch (note) { Assign(:final value) => value, _ => this.note },
  );

  Json toClient() => <String, Object?>{
    'id': id,
    if (note case final v?) 'note': v,
  };

  @override
  bool operator ==(Object other) =>
      identical(this, other) || other is Order && this.id == other.id && this.note == other.note;

  @override
  int get hashCode => Object.hashAll([
    id,
    note,
  ]);
}

// ops.dart: one constant per operation, then the tables.
const opUpdateOrder = OperationMeta(
  id: 'op_update_order',
  method: 'PATCH',
  path: '/orders/{id}',
  entity: 'Order',
  rootType: 'Order',
  provides: ['Order:{id}'],
  invalidates: ['Order[]'],
  idempotent: true,
);

const Map<String, OperationMeta> operations = {
  'op_update_order': opUpdateOrder,
};

const EntitySchema entities = {
  'Order': EntityMeta(idField: 'id'),
};

// bindings/update_order.dart.
final updateOrder = mutation<Order, UpdateOrderArgs, Order>(
  opUpdateOrder,
  _updateOrderFromClient,
  entityFromClient: Order.fromClient,
  entityToClient: (e) => e.toClient(),
);

Order _updateOrderFromClient(Object? client) => decodeCached(Order.fromClient, client);

final class UpdateOrderArgs implements OperationArgs {
  const UpdateOrderArgs({
    required this.id,
    this.note = const Unchanged(),
  });

  final String id;

  final Value<String> note;

  @override
  TagContext toTagContext() => TagContext(
    path: {'id': id},
    body: <String, Object?>{
      if (note case Assign(:final value)) 'note': value,
    },
  );

  @override
  bool operator ==(Object other) =>
      other is UpdateOrderArgs && this.id == other.id && valueEquals(this.note, other.note);

  @override
  int get hashCode => Object.hashAll([id, valueHash(note)]);
}

// App functions the examples mention. Each one stands for a screen, a dialog
// or an auth call the app owns.
void askWhichCopyWins(OutboxConflict failure) {}

void showInvalid(OutboxValidation failure) {}

void signInAgain(OutboxUnauthorized failure) {}

void askWhetherToResend(OutboxUncertain failure) {}

void tellUserAboutReset(StorageReset reset) {}

Future<bool> userConfirmsErase() async => true;

String askForPassphrase() => 'correct horse battery staple';

Future<void> dropFailureScreens() async {}
