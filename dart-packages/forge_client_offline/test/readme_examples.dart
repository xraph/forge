// Every ```dart block of README.md, pasted verbatim, so `flutter analyze`
// compiles them against the real APIs. test/readme_test.dart fails when a
// block in the README is not found here, so the two cannot drift. Not a test
// file itself: it only has to compile.
//
// Paste each README block below its marker. The only difference allowed is
// the generated package's import, which the README spells
// `package:orders_forge_client/orders_forge_client.dart` and this file
// spells `support/readme_stubs.dart`.
// ignore_for_file: unused_import

//README-BLOCK open
import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:forge_client/forge_client.dart';
import 'package:forge_client_offline/forge_client_offline.dart';
import 'support/readme_stubs.dart';

String? token;
String? tokenOwner;
final heldFailures = <OutboxFailure>[];

Future<OfflineClient> openOffline(
  EncryptedSqliteStorage storage,
  ConnectivitySignal connectivity,
  String userId,
) async {
  token = 'token-for-$userId';
  tokenOwner = userId;

  final offline = await OfflineClient.open(
    transport: RestTransport(
      baseUrl: Uri.parse('https://api.example.com'),
      auth: AuthProvider.callbacks(
        credentials: (_) => token == null ? null : {'Authorization': 'Bearer $token'},
      ),
    ),
    entities: entities,
    operations: operations,
    storage: storage,
    principal: userId,
    connectivity: connectivity,
    authPrincipal: () => tokenOwner,
    onError: (error, context) => debugPrint('offline $context: $error'),
  );

  // Register your own changing listeners after open returns, never before.
  offline.cache.watchPrincipalChanging((next) => heldFailures.clear());
  return offline;
}

//README-BLOCK by-hand
Future<OfflineClient> buildByHand(
  Transport inner,
  EncryptedSqliteStorage storage,
  ConnectivitySignal connectivity,
  String userId,
) async {
  final outbox = OutboxTransport(inner);
  final cache = QueryCache(transport: outbox, entities: entities, storage: storage);
  final offline = OfflineClient(
    cache: cache,
    operations: operations,
    connectivity: connectivity,
    transport: outbox,
    storageResets: storage.resets,
  );
  cache.setPrincipal(userId);
  await offline.restore();
  return offline;
}

//README-BLOCK devtools
Future<OfflineClient> openWithDevtools(
  RestTransport rest,
  EncryptedSqliteStorage storage,
  ConnectivitySignal connectivity,
  String userId,
) {
  return OfflineClient.open(
    transport: rest,
    entities: entities,
    operations: operations,
    storage: storage,
    principal: userId,
    connectivity: connectivity,
    devtools: true,
  );
}

//README-BLOCK write
void saveNote(OfflineClient offline, String id, String note) {
  updateOrder(
    offline.cache,
    UpdateOrderArgs(id: id, note: Assign(note)),
    optimistic: OptimisticUpdate((order) => order.copyWith(note: Assign(note))),
  ).ignore();
}

//README-BLOCK status
StreamSubscription<OutboxFailure> listenForFailures(OfflineClient offline) {
  heldFailures.addAll(offline.currentFailures);
  debugPrint('${offline.pending.length} writes waiting');

  return offline.failures.listen((failure) {
    heldFailures.add(failure);
    switch (failure) {
      case OutboxConflict():
        askWhichCopyWins(failure);
      case OutboxValidation():
        showInvalid(failure);
      case OutboxUnauthorized():
        signInAgain(failure);
      case OutboxGone():
        failure.discard().ignore();
      case OutboxUncertain():
        askWhetherToResend(failure);
    }
  });
}

//README-BLOCK switch-account
Future<void> switchAccount(OfflineClient offline, String userId) async {
  offline.cache.setPrincipal(userId);
  token = 'token-for-$userId';
  tokenOwner = userId;
  await offline.cache.idle;
}

//README-BLOCK lifecycle
Future<void> onPaused(OfflineClient offline) => offline.flush();

Future<void> onExit(OfflineClient offline) => offline.close();

Future<void> signOutAndErase(OfflineClient offline) async {
  await offline.signOut();
  token = null;
  tokenOwner = null;
}

Future<void> signOutAndKeep(OfflineClient offline) => offline.signOut(erase: false);

//README-BLOCK resets
StreamSubscription<StorageReset> reportResets(OfflineClient offline) {
  offline.currentResets.forEach(tellUserAboutReset);
  return offline.resets.listen(tellUserAboutReset);
}

//README-BLOCK key-unavailable
Future<OfflineClient?> openOrOfferReset(
  EncryptedSqliteStorage storage,
  ConnectivitySignal connectivity,
  String userId,
) async {
  try {
    return await openOffline(storage, connectivity, userId);
  } on KeyUnavailable {
    if (!await userConfirmsErase()) return null;
    await storage.resetOfflineData();
    return openOffline(storage, connectivity, userId);
  }
}

//README-BLOCK passphrase
EncryptedSqliteStorage passphraseStorage(String directory) {
  final labels = PrincipalLabels(
    FlutterSecretStore(
      secureStorageFor(requireUserPresence: false, useDataProtectionKeychain: true),
    ),
  );
  return encryptedSqliteStorage(
    keys: passphraseKey(
      askForPassphrase,
      salts: platformPassphraseSaltStore(directory: directory),
      labels: labels,
    ),
    labels: labels,
    directory: directory,
  );
}
