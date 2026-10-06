import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:isolate';
import 'dart:math';

import 'package:grove_crdt/grove_crdt.dart';
import 'package:http/http.dart' as http;

/// Environment for every Go command and for the server itself, on top of the
/// inherited one: no workspace file, no network, and `go.mod` may be updated
/// from the module cache only.
const _goEnvironment = {
  'GOWORK': 'off',
  'GOPROXY': 'off',
  'GOFLAGS': '-mod=mod',
};

/// Runs grove_crdt's Go conformance server, found next to the resolved
/// `grove_crdt` package (the pinned git checkout or the local override).
///
/// The server binds 127.0.0.1 only. Every instance is a child process that
/// [stop] kills through its own [Process] and waits for, so nothing outlives a
/// test run. The child's stdin stays open for its whole life and the server
/// exits when it ends, so a runner killed before [stop] (outside tearDown)
/// cannot leave a server behind: its end of the pipe closes with it.
final class GroveServer {
  GroveServer._(this._process, this.baseUrl);

  final Process _process;

  /// Server root, e.g. `http://127.0.0.1:53412`.
  final Uri baseUrl;

  static String? _binary;

  static Future<String> _toolDir() async {
    final lib = await Isolate.resolvePackageUri(
      Uri.parse('package:grove_crdt/grove_crdt.dart'),
    );

    if (lib == null) throw StateError('grove_crdt is not resolvable');

    return File.fromUri(lib).parent.parent.uri
        .resolve('tool/conformance_server/')
        .toFilePath();
  }

  /// A skip reason when Go is not installed, else null.
  static String? skipReason() {
    try {
      return Process.runSync('go', ['version']).exitCode == 0
          ? null
          : 'go is not available';
    } on ProcessException {
      return 'go is not on PATH';
    }
  }

  /// Builds the binary once per test isolate and starts a fresh server.
  static Future<GroveServer> start({
    List<String> tables = const ['notes', 'tasks'],
  }) async {
    _binary ??= await _build();

    final process = await Process.start(_binary!, [
      '-addr',
      '127.0.0.1:0',
      '-tables',
      tables.join(','),
    ], environment: _goEnvironment);

    unawaited(process.stderr.drain<void>());

    try {
      final line = await process.stdout
          .transform(utf8.decoder)
          .transform(const LineSplitter())
          .firstWhere((l) => l.startsWith('LISTENING '))
          .timeout(const Duration(seconds: 30));

      return GroveServer._(
        process,
        Uri.parse(line.substring('LISTENING '.length)),
      );
    } on Object {
      process.kill();
      await process.exitCode;
      rethrow;
    }
  }

  /// Builds into a private file (process id plus a random suffix) and renames
  /// it into place, so test files building at the same time never write the
  /// same path. A server that is already running keeps the old file's inode.
  static Future<String> _build() async {
    final dir = Directory('${Directory.current.path}/.dart_tool');

    await dir.create(recursive: true);

    final out = '${dir.path}/grove_conformance_server';
    final tmp =
        '$out.${pid}_${Random.secure().nextInt(1 << 32).toRadixString(16)}.tmp';
    final r = await Process.run(
      'go',
      ['build', '-o', tmp, '.'],
      workingDirectory: await _toolDir(),
      environment: _goEnvironment,
    );

    if (r.exitCode != 0) {
      if (File(tmp).existsSync()) File(tmp).deleteSync();

      throw StateError('go build failed:\n${r.stderr}');
    }

    await File(tmp).rename(out);

    return out;
  }

  Uri _admin(String path, [Map<String, String>? query]) =>
      baseUrl.replace(path: path, queryParameters: query);

  static void _ok(http.Response r, String what) {
    if (r.statusCode >= 300) {
      throw StateError('$what failed: ${r.statusCode} ${r.body}');
    }
  }

  /// Makes the server's sync hook refuse changes to [field] (null clears it).
  Future<void> rejectField(String? field) async => _ok(
    await http.post(_admin('/admin/reject', {'field': field ?? ''})),
    'reject',
  );

  /// The server's merged state of one record, or null when it has none.
  Future<DocumentState?> state(String table, String pk) async {
    final r = await http.get(
      _admin('/admin/state', {'table': table, 'pk': pk}),
    );

    if (r.statusCode != 200) return null;

    return DocumentState.fromJson(jsonDecode(r.body));
  }

  /// Stops the server: closes its stdin, which ends it, then kills this
  /// child by PID in case it is still running, and waits for it to exit.
  Future<void> stop() async {
    try {
      await _process.stdin.close();
    } on Object {
      // Already gone: the pipe is broken, and the kill below is a no-op.
    }

    _process.kill();
    await _process.exitCode.timeout(
      const Duration(seconds: 10),
      onTimeout: () {
        _process.kill(ProcessSignal.sigkill);

        return _process.exitCode;
      },
    );
  }
}
