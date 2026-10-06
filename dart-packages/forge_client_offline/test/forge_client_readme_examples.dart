// The Dart blocks of the "Devtools" section of forge_client's README, pasted
// verbatim, so `flutter analyze` compiles them against the real APIs (forge_client
// itself cannot: they use the outbox). test/forge_client_readme_test.dart fails
// when a block in that README is not found here. Not a test file itself: it
// only has to compile.
//
// Each block sits inside a function that declares what the README leaves free:
// the generated tables, the app's storage and connectivity, the cache and the
// offline client.
// ignore_for_file: unused_import, unused_local_variable
import 'package:forge_client/devtools.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/forge_client_offline.dart';

import 'support/readme_stubs.dart';

//README-BLOCK open-with-devtools
Future<void> openWithDevtools(
  RestTransport rest,
  EncryptedSqliteStorage storage,
  ConnectivitySignal connectivity,
  String userId,
) async {
  final offline = await OfflineClient.open(
    transport: rest,
    entities: entities,
    operations: operations,
    storage: storage,
    principal: userId,
    connectivity: connectivity,
    devtools: true,
  );
}

//README-BLOCK by-hand
void byHand(StorageAdapter storage, ConnectivitySignal connectivity) {
  final rest = RestTransport(baseUrl: Uri.parse('https://api.example.com'));
  final controls = kForgeDevtools ? ControlledTransport(rest) : null;
  final outbox = OutboxTransport(controls ?? rest);
  final cache = QueryCache(transport: outbox, entities: entities, storage: storage);
  final offline = OfflineClient(
    cache: cache,
    operations: operations,
    connectivity: controls == null ? connectivity : withSimulatedConnectivity(connectivity, controls),
    transport: outbox,
  );

  registerForgeServiceExtensions(
    cache,
    transport: rest,         // feeds the request log
    controls: controls,      // the offline and latency switches
    operations: operations,  // the generated table, for "what would this invalidate"
    outbox: offline,         // replay and discard
  );
}

//README-BLOCK configure-client-over-outbox
void overOutbox(OutboxTransport outbox, StorageAdapter storage) {
  final cache = configureClient(transport: outbox, entities: entities, storage: storage);
}

//README-BLOCK register-outbox
void registerOutbox(QueryCache cache, OfflineClient offlineClient) {
  registerForgeServiceExtensions(cache, outbox: offlineClient);
}

//README-BLOCK merged-signal
void mergedSignal(QueryCache cache, ConnectivitySignal connectivity) {
  final controls = forgeDevtoolsFor(cache)?.controls;
  final online = controls == null ? connectivity : withSimulatedConnectivity(connectivity, controls);
}
