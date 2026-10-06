@TestOn('vm')
library;

import 'dart:developer';
import 'dart:isolate';

import 'package:forge_client/devtools.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:test/test.dart';
import 'package:vm_service/vm_service.dart' as vm;
import 'package:vm_service/vm_service_io.dart';

import 'harness.dart';

// The real `dart:developer` path, end to end: this isolate starts its own VM
// service, connects to itself, and calls the extensions the way DevTools does.
// No host override here, so the registrations are the production ones.
void main() {
  test('registers ext.forge.* on the running isolate and answers over the VM service', () async {
    final info = await Service.controlWebServer(
      enable: true,
      silenceOutput: true,
    );
    final uri = info.serverWebSocketUri;
    if (uri == null) {
      markTestSkipped(
        'the VM service could not be started in this environment',
      );
      return;
    }

    final service = await vmServiceConnectUri(uri.toString());
    addTearDown(service.dispose);

    final h = Harness();
    registerForgeServiceExtensions(h.cache);

    final isolateId = Service.getIsolateId(Isolate.current)!;
    final isolate = await service.getIsolate(isolateId);

    expect(isolate.extensionRPCs, containsAll(ForgeDevtoolsProtocol.methods));

    final hello = await service.callServiceExtension(
      ForgeDevtoolsProtocol.hello,
      isolateId: isolateId,
    );
    expect(hello.json!['protocol'], ForgeDevtoolsProtocol.version);

    await service.streamListen(vm.EventStreams.kExtension);
    final event = service.onExtensionEvent.firstWhere(
      (e) => e.extensionKind == ForgeDevtoolsProtocol.eventKind,
    );

    final sub = h.mount(Ops.orderList);
    await h.settle();

    final posted = await event.timeout(const Duration(seconds: 10));
    expect(posted.extensionData!.data['entries'], isNotEmpty);

    final snapshot = await service.callServiceExtension(
      ForgeDevtoolsProtocol.snapshot,
      isolateId: isolateId,
    );
    expect((snapshot.json!['store']! as Map<String, Object?>)['records'], 3);

    final bad = service.callServiceExtension(
      ForgeDevtoolsProtocol.entities,
      isolateId: isolateId,
      args: {'limit': 'abc'},
    );
    await expectLater(bad, throwsA(isA<vm.RPCError>()));

    await sub.cancel();
  });
}
