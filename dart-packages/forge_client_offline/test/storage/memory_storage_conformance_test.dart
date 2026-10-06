import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client/forge_client.dart';

import 'storage_conformance.dart';

/// The harness against the contract's reference adapter: proof that the port
/// and the extra cases ask nothing of an adapter that memoryStorage() does not
/// already do. If a case fails here, the harness is wrong, not the adapter.
void main() {
  group('memoryStorage', () => storageConformance(memoryStorage));
}
