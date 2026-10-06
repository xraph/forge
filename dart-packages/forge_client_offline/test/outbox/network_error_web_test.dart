@TestOn('browser')
library;

import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'package:http/http.dart' as http;

void main() {
  test('a fetch failure while the browser is online is uncertain', () {
    expect(
      classifyNetworkError(http.ClientException('Failed to fetch')),
      NetworkFailure.uncertain,
    );
  });

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
