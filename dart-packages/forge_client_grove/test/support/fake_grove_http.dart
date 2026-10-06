import 'dart:async';
import 'dart:convert';

import 'package:grove_crdt/grove_crdt.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// The headers every fake response carries. The `date` matches the harness
/// clock, so no clock correction kicks in.
const fakeHeaders = {
  'content-type': 'application/json',
  'date': 'Sun, 04 Oct 2026 12:00:00 GMT',
};

/// One request the fake server received.
final class FakeRequest {
  FakeRequest(this.path, this.headers, this.body);

  /// The URL path.
  final String path;

  /// The request headers, keys lower-case.
  final Map<String, String> headers;

  /// The decoded JSON body.
  final Object? body;

  /// The `authorization` header, or null.
  String? get authorization => headers['authorization'];

  /// Whether this is a push.
  bool get isPush => path.endsWith('/push');

  /// Whether this is a pull.
  bool get isPull => path.endsWith('/pull');

  /// The `node_id` the body names.
  String? get nodeId => (body as Map<String, Object?>?)?['node_id'] as String?;

  /// The changes a push carries.
  List<ChangeRecord> get changes =>
      isPush ? PushRequest.fromJson(body).changes : const <ChangeRecord>[];
}

/// Grove's native pull and push over MockClient, one change log per account
/// and dataset (path `/d/<id>/...`; any other path is dataset `''`), with the
/// Go cursor semantics and scriptable failures.
///
/// The account is the request's `authorization` header (`''` without one), so
/// what one principal pushes is only ever pulled back by that principal, as a
/// real server scopes it.
final class FakeGroveHttp {
  final Map<String, List<ChangeRecord>> logs = {};
  final List<String> paths = [];
  final List<FakeRequest> requests = [];
  final Set<String> goneDatasets = {};
  bool gone = false;
  String? rejectField;

  /// Called with every request before it is answered. A non-null response
  /// (or a future of one) answers it instead; null answers it normally. Use
  /// it to hold a request open across a principal switch.
  FutureOr<http.Response?> Function(FakeRequest request)? intercept;

  /// The log of [dataset] in [account].
  List<ChangeRecord> logOf(String dataset, {String account = ''}) =>
      logs.putIfAbsent('$account|$dataset', () => []);

  /// The log of dataset `''` without auth.
  List<ChangeRecord> get log => logOf('');

  static final _dataset = RegExp(r'^/d/([^/]+)/');

  late final http.Client client = MockClient((r) async {
    paths.add(r.url.path);

    final request = FakeRequest(r.url.path, {
      for (final e in r.headers.entries) e.key.toLowerCase(): e.value,
    }, r.body.isEmpty ? null : jsonDecode(r.body));

    requests.add(request);

    final hook = intercept;

    if (hook != null) {
      final answer = await hook(request);

      if (answer != null) return answer;
    }

    return answer(request);
  });

  /// The normal answer to [request].
  http.Response answer(FakeRequest request) {
    final m = _dataset.firstMatch(request.path);
    final dataset = m == null ? '' : Uri.decodeComponent(m.group(1)!);

    if (gone || goneDatasets.contains(dataset)) {
      return http.Response(
        '{"code":404,"message":"dataset $dataset is not collaborative or not '
        'provisioned"}',
        404,
        headers: fakeHeaders,
      );
    }

    final log = logOf(dataset, account: request.authorization ?? '');

    if (request.isPull) {
      final req = PullRequest.fromJson(request.body);
      final since = req.since;
      final out =
          log
              .where(
                (c) =>
                    c.hlc.ts > since.ts ||
                    (c.hlc.ts == since.ts && c.hlc.c > since.c),
              )
              .where(
                (c) =>
                    req.filter == null ||
                    req.filter!.pkFilter.isEmpty ||
                    req.filter!.pkFilter.contains(c.pk),
              )
              .toList()
            ..sort((a, b) => a.hlc.compareTo(b.hlc));

      return pullResponse(out);
    }

    final req = PushRequest.fromJson(request.body);
    var merged = 0;

    for (final c in req.changes) {
      if (c.field == rejectField) {
        return http.Response(
          '{"error":"crdt: inbound change hook: ${c.field} is locked"}',
          500,
          headers: fakeHeaders,
        );
      }

      log.add(c);
      merged++;
    }

    return http.Response(
      encodeWire(PushResponse(merged: merged, latestHlc: HLC.zero).toJson()),
      200,
      headers: fakeHeaders,
    );
  }

  /// A pull answer carrying [changes].
  static http.Response pullResponse(List<ChangeRecord> changes) =>
      http.Response(
        encodeWire(
          PullResponse(
            changes: changes,
            latestHlc: changes.isEmpty ? HLC.zero : changes.last.hlc,
          ).toJson(),
        ),
        200,
        headers: fakeHeaders,
      );

  /// A change authored by another device.
  static ChangeRecord change(
    String pk,
    String field,
    Object? value,
    int ts, {
    String table = 'notes',
  }) => ChangeRecord(
    table: table,
    pk: pk,
    field: field,
    crdtType: CrdtType.lww,
    hlc: HLC(BigInt.from(ts), 0, 'other'),
    nodeId: 'other',
    value: JsonValue(value),
  );

  /// Adds a change authored by another device to a log.
  void remote(
    String pk,
    String field,
    Object? value,
    int ts, {
    String dataset = '',
    String table = 'notes',
    String account = '',
  }) => logOf(
    dataset,
    account: account,
  ).add(change(pk, field, value, ts, table: table));
}
