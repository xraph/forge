@TestOn('browser')
library;

import 'dart:js_interop';
import 'dart:js_interop_unsafe';

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:http/http.dart' as http;
import 'package:web/web.dart' as web;

@JS('Object.defineProperty')
external void _defineProperty(JSObject target, String name, JSObject spec);

@JS('Object.getPrototypeOf')
external JSObject _prototypeOf(JSObject target);

JSObject _spec(bool online) {
  final spec = JSObject();
  spec.setProperty('get'.toJS, (() => online).toJS);
  spec.setProperty('configurable'.toJS, true.toJS);
  return spec;
}

void main() {
  test('a fetch failure while the browser is online is uncertain', () {
    expect(
      classifyNetworkError(http.ClientException('Failed to fetch')),
      NetworkFailure.uncertain,
    );
  });

  test(
    'a fetch failure while the browser reports offline is still uncertain',
    () {
      // navigator.onLine cannot say whether the connection dropped after the
      // request left, so it must not turn an uncertain outcome into notSent.
      final navigator = web.window.navigator as JSObject;
      final prototype = _prototypeOf(navigator);
      _defineProperty(prototype, 'onLine', _spec(false));
      addTearDown(() => _defineProperty(prototype, 'onLine', _spec(true)));

      expect(web.window.navigator.onLine, isFalse);
      expect(
        classifyNetworkError(http.ClientException('Failed to fetch')),
        NetworkFailure.uncertain,
      );
    },
  );

  test('a caller abort is cancelled, not uncertain', () {
    expect(
      classifyNetworkError(http.RequestAbortedException()),
      NetworkFailure.cancelled,
    );
  });

  test('a programming error is not a network failure', () {
    expect(classifyNetworkError(StateError('bug')), isNull);
  });
}
