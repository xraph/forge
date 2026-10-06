import 'dart:async';

import 'package:forge_client/forge_client.dart';
import 'package:grove_crdt/grove_crdt.dart';
import 'package:meta/meta.dart';

/// How one Grove-backed entity maps between its replica and the store.
@immutable
final class GroveEntity {
  /// Describes an entity. [codec] is its generated wire codec.
  const GroveEntity({
    required this.codec,
    this.columns = const {},
    this.types = const {},
  });

  /// Converts between the server's JSON and client-shaped records.
  final WireCodec codec;

  /// Replica column to server JSON key, where they differ.
  final Map<String, String> columns;

  /// Server JSON key to CRDT type; LWW when absent.
  final Map<String, CrdtType> types;
}

/// One dataset an entity syncs.
@immutable
final class GroveDataset {
  /// A dataset. [table] is the server's table when it is known up front
  /// (foundry's `ds_<name>`); otherwise it is learned from the first pull.
  const GroveDataset(this.id, {this.table});

  /// The value substituted for the declaration's dataset placeholder.
  final String id;

  /// The server table, when known.
  final String? table;
}

/// The datasets to sync for one declaration.
typedef GroveDatasets = FutureOr<List<GroveDataset>> Function(
  SyncDeclaration declaration,
);

/// How remote changes reach the device.
enum LiveChannel {
  /// SSE when declared, else WebSocket when declared and the table is known,
  /// else polling.
  auto,

  /// The SSE change stream.
  sse,

  /// The multiplexed WebSocket.
  websocket,

  /// Periodic pull only.
  poll,
}

/// Concrete paths for one dataset.
@immutable
final class GroveEndpoints {
  /// Creates the endpoint set.
  const GroveEndpoints({
    required this.pull,
    required this.push,
    this.stream,
    this.socket,
  });

  /// Pull path.
  final String pull;

  /// Push path.
  final String push;

  /// SSE path, when declared.
  final String? stream;

  /// WebSocket path, when declared.
  final String? socket;
}

/// The path parameter a declaration's dataset placeholder names.
String? datasetParam(SyncDeclaration d) {
  final ds = d.dataset;
  if (ds == null || ds.isEmpty) return null;
  return ds.startsWith('{') && ds.endsWith('}')
      ? ds.substring(1, ds.length - 1)
      : ds;
}

/// The replica namespace key of a dataset.
String replicaKey({required String datasetId, required String pullPath}) =>
    datasetId.isEmpty
    ? '~${Uri.encodeComponent(pullPath)}'
    : Uri.encodeComponent(datasetId);

/// The store id of row [pk] of dataset [datasetId]. Row primary keys are only
/// unique per Grove table, and each dataset is its own table, so a dataset
/// prefix keeps two datasets' rows apart in the shared entity store. The
/// dataset id is percent-encoded, so the first `:` always ends it.
String compositeId(String datasetId, String pk) =>
    '${Uri.encodeComponent(datasetId)}:$pk';

/// Splits a [compositeId]; null when [id] carries no dataset prefix.
({String datasetId, String pk})? splitCompositeId(String id) {
  final i = id.indexOf(':');
  if (i < 0) return null;
  return (
    datasetId: Uri.decodeComponent(id.substring(0, i)),
    pk: id.substring(i + 1),
  );
}

/// The declared table, or null when the declaration leaves it to each
/// dataset (foundry's `ds_<name>`).
String? declaredTable(SyncDeclaration d) {
  final t = d.table;
  return t == null || t.isEmpty ? null : t;
}

/// Resolves a declaration's paths for [datasetId].
///
/// Throws [ArgumentError] for an empty [datasetId] on a declaration that has a
/// dataset placeholder, which would otherwise yield paths such as
/// `/datasets//sync/pull`.
GroveEndpoints resolveEndpoints(SyncDeclaration d, String datasetId) {
  final placeholder = d.dataset;

  if (placeholder != null && placeholder.isNotEmpty && datasetId.isEmpty) {
    throw ArgumentError.value(
      datasetId,
      'datasetId',
      'must not be empty: ${d.entity} syncs per dataset ($placeholder)',
    );
  }
  String sub(String p) => placeholder == null || placeholder.isEmpty
      ? p
      : p.replaceAll(placeholder, Uri.encodeComponent(datasetId));
  return GroveEndpoints(
    pull: sub(d.pull),
    push: sub(d.push),
    stream: d.stream == null ? null : sub(d.stream!),
    socket: d.socket == null ? null : sub(d.socket!),
  );
}

/// One change the server refused.
@immutable
final class GroveRejectedChange {
  /// Describes a rejected change.
  const GroveRejectedChange({
    required this.key,
    required this.entity,
    required this.id,
    required this.field,
    required this.kind,
    required this.reason,
  });

  /// The pending key, for retry and discard.
  final String key;

  /// The entity typename.
  final String entity;

  /// The record id.
  final String id;

  /// The replica column, or `''` for a record delete.
  final String field;

  /// `hook`, `validation`, `drift`, `bad_request` or `server`.
  final String kind;

  /// The server's text.
  final String reason;

  @override
  bool operator ==(Object other) =>
      other is GroveRejectedChange &&
      other.key == key &&
      other.entity == entity &&
      other.id == id &&
      other.field == field &&
      other.kind == kind &&
      other.reason == reason;

  @override
  int get hashCode => Object.hash(key, entity, id, field, kind, reason);
}

/// The error inside `SyncFailed` when the server refused changes.
@immutable
final class GroveChangeRejected implements Exception {
  /// Wraps the refused changes.
  const GroveChangeRejected(this.changes);

  /// Every refused change of the entity.
  final List<GroveRejectedChange> changes;

  /// The first change's reason.
  String get reason => changes.first.reason;

  /// Equal when both hold equal changes in the same order, so a status that
  /// did not change compares equal and is not announced again.
  @override
  bool operator ==(Object other) {
    if (other is! GroveChangeRejected ||
        other.changes.length != changes.length) {
      return false;
    }

    for (var i = 0; i < changes.length; i++) {
      if (other.changes[i] != changes[i]) return false;
    }

    return true;
  }

  @override
  int get hashCode => Object.hashAll(changes);

  @override
  String toString() => 'GroveChangeRejected: $reason';
}

/// The error inside `SyncFailed` when the dataset is gone (404 or 410).
@immutable
final class GroveDatasetGone implements Exception {
  /// Describes the missing dataset.
  const GroveDatasetGone(this.datasetId, this.message);

  /// The dataset id.
  final String datasetId;

  /// The server's text.
  final String message;

  @override
  bool operator ==(Object other) =>
      other is GroveDatasetGone &&
      other.datasetId == datasetId &&
      other.message == message;

  @override
  int get hashCode => Object.hash(GroveDatasetGone, datasetId, message);

  @override
  String toString() => 'GroveDatasetGone($datasetId): $message';
}

/// The error inside `SyncFailed` when the server refused credentials.
@immutable
final class GroveUnauthorized implements Exception {
  /// Describes the refusal.
  const GroveUnauthorized(this.datasetId);

  /// The dataset id.
  final String datasetId;

  @override
  bool operator ==(Object other) =>
      other is GroveUnauthorized && other.datasetId == datasetId;

  @override
  int get hashCode => Object.hash(GroveUnauthorized, datasetId);

  @override
  String toString() => 'GroveUnauthorized($datasetId)';
}
