import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/devtools_protocol.dart';
import 'package:forge_client_devtools/src/backend/isolate_watch.dart';

import '../support/fake_hooks.dart';

const _hello = ForgeDevtoolsProtocol.hello;

IsolateWatch _watch(FakeServiceHooks hooks) {
  final watch = IsolateWatch(hooks, _hello);
  addTearDown(watch.dispose);
  return watch;
}

void main() {
  for (final closeLate in [false, true]) {
    final order = closeLate
        ? 'DevTools drops its notifiers after the watch hears the close'
        : 'DevTools drops its notifiers before the watch hears the close';

    test('follows the extension across a hot restart, when $order', () async {
      final hooks = FakeServiceHooks(
        registered: {_hello},
        closeLate: closeLate,
      );
      final watch = _watch(hooks);
      expect(watch.available.value, isTrue);
      final before = watch.isolate.value;

      hooks.closeIsolate();
      await pumpEventQueue();
      expect(watch.available.value, isFalse);

      hooks.openIsolate('isolate-2');
      await pumpEventQueue();
      expect(watch.available.value, isFalse);

      // The notifier the watch held before the restart was dropped; only a
      // re-resolved one hears this.
      hooks.register(_hello);
      await pumpEventQueue();

      expect(watch.available.value, isTrue);
      expect(watch.isolate.value, before + 2);
    });
  }

  test(
    'an isolate replaced without closing changes the isolate signal',
    () async {
      final hooks = FakeServiceHooks(registered: {_hello});
      final watch = _watch(hooks);
      final before = watch.isolate.value;

      hooks.replaceIsolate('isolate-2');
      await pumpEventQueue();

      expect(watch.isolate.value, before + 1);
      expect(watch.available.value, isTrue);
    },
  );

  test('setting the same isolate again is not a change', () async {
    final hooks = FakeServiceHooks(registered: {_hello});
    final watch = _watch(hooks);
    final before = watch.isolate.value;

    hooks.replaceIsolate('isolate-1');
    await pumpEventQueue();

    expect(watch.isolate.value, before);
  });

  test('a VM service reconnect re-resolves the extension', () async {
    final hooks = FakeServiceHooks(registered: {_hello});
    final watch = _watch(hooks);
    final before = watch.isolate.value;

    hooks.reconnect();
    await pumpEventQueue();
    expect(watch.available.value, isFalse);
    expect(watch.isolate.value, greaterThan(before));

    hooks.register(_hello);
    await pumpEventQueue();
    expect(watch.available.value, isTrue);
  });
}
