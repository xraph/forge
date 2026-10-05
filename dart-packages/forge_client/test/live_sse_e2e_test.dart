@TestOn('vm')
library;

import 'package:forge_client/forge_client.dart';
import 'package:test/test.dart';

import 'support/core_support.dart';
import 'support/harness.dart';
import 'support/schema.dart';
import 'support/test_servers.dart';

void main() {
  // Review Focus: reconnect after missed frames, end to end. A real SSE
  // stream drops mid-event, the manager reconnects carrying Last-Event-ID, the
  // server cannot replay and says nothing, and once the grace window passes
  // the binder invalidates Order[] and refetches the live query.
  test('refetches live queries after an SSE stream drops and the server cannot replay', () async {
    final server = SseTestServer();
    await server.start();
    addTearDown(server.stop);

    final transport = FakeTransport(
      (_, call) => [
        {'id': 7, 'total': call == 0 ? 99 : 4242},
      ],
    );
    final cache = QueryCache(transport: transport, entities: schema);
    final manager = SubscriptionManager(
      connect: eventSourceConnection(events: ['order.updated']),
      baseUrl: server.url,
      backoff: const BackoffPolicy(
        initial: Duration(milliseconds: 20),
        max: Duration(milliseconds: 40),
      ),
      principal: () => cache.principal,
    );
    addTearDown(manager.closeAll);

    final binder = StreamBinder(
      cache: cache,
      streams: const [
        EntityStreamBinding(
          channel: '/sse/orders',
          message: 'order.updated',
          entity: 'Order',
          intent: StreamIntent.patch,
        ),
      ],
      manager: manager,
      resumeGrace: const Duration(milliseconds: 50),
    );
    addTearDown(binder.dispose);

    final watching = cache.watch(orderList, TagContext.empty).listen((_) {});
    addTearDown(watching.cancel);
    final release = binder.subscribe(orderList);
    addTearDown(release);

    await server.connections(1);
    await until(() => transport.calls.length == 1);

    server.send(
      0,
      'id: e-1\nevent: order.updated\ndata: {"id":7,"total":100}\n\n',
    );
    await until(() => cache.store.getRecord('Order:7')?.data['total'] == 100);

    server.send(0, 'id: e-2\nevent: order.updated\ndata: {"id":7,');
    await server.end(0);

    await server.connections(2);
    expect(server.requests[1]['last-event-id'], 'e-1');

    await until(() => transport.calls.length == 2);
    await until(() => cache.store.getRecord('Order:7')?.data['total'] == 4242);
  });
}
