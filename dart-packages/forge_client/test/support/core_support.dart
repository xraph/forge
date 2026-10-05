/// Test doubles the stream, live, snapshot and sync suites add to 01a's
/// `harness.dart` (`FakeTransport`, `settle`) and `schema.dart` (`schema`).
/// The socket half of `harness.ts` lives in `fake_sockets.dart`.
library;

import 'dart:async';

import 'package:forge_client/forge_client.dart';

/// A transport handing out pre-registered responses, one per call, in order.
///
/// Each entry is a thunk so that a failing response is not created, and left
/// unhandled, before the call that is meant to receive it.
final class ScriptedTransport implements Transport {
  /// Serves [responses] in order.
  ScriptedTransport(List<FutureOr<Object?> Function()> responses)
    : _responses = [...responses];

  final List<FutureOr<Object?> Function()> _responses;

  /// Every request, in order.
  final List<TransportRequest> calls = [];

  @override
  Future<Object?> execute(TransportRequest request) {
    calls.add(request);

    final next = _responses.removeAt(0);

    return Future<Object?>.sync(next);
  }
}

/// A one-slot commit scheduler the test flushes by hand, like `ManualScheduler`.
final class ManualCommitScheduler implements CommitScheduler {
  void Function()? _pending;

  /// How many times something asked for a commit.
  int scheduled = 0;

  @override
  void schedule(void Function() commit) {
    scheduled++;
    _pending = commit;
  }

  /// Run the pending commit, if any.
  void flush() {
    final pending = _pending;
    _pending = null;
    pending?.call();
  }
}

/// A clock that always reads [value].
final class FixedClock implements Clock {
  /// Reads [value] milliseconds forever.
  const FixedClock(this.value);

  /// The reading.
  final int value;

  @override
  int now() => value;
}

/// `GET /orders`.
const orderList = OperationMeta(
  id: 'op_order_list',
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
);

/// `GET /orders/{id}`.
const orderGet = OperationMeta(
  id: 'op_order_get',
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  provides: ['Order:{id}'],
);

/// `GET /customers`.
const customerList = OperationMeta(
  id: 'op_customer_list',
  method: 'GET',
  path: '/customers',
  entity: 'Customer',
  provides: ['Customer[]'],
);

/// `GET /widgets`, an entity with no edge to or from `Order`.
const widgetList = OperationMeta(
  id: 'op_widget_list',
  method: 'GET',
  path: '/widgets',
  entity: 'Widget',
  provides: ['Widget[]'],
);
