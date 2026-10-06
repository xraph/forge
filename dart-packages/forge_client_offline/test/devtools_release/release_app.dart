// ignore_for_file: avoid_print
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/src/offline_client.dart';

// A pure-Dart entry point (the package barrel pulls in Flutter-only keystore
// code) that calls `OfflineClient.open(devtools: true)`. Compiled by
// test/devtools_release_test.dart; not itself a test.
Future<void> main() async {
  final offline = await OfflineClient.open(
    transport: RestTransport(baseUrl: Uri.parse('http://forge.test')),
    entities: const {},
    operations: const {},
    storage: memoryStorage(),
    principal: 'release',
    devtools: true,
  );
  print('forge-release-fixture-marker ${offline.cache.principal}');
}
