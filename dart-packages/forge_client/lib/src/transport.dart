import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:http/http.dart' as http;

import 'operation.dart';
import 'ref.dart';

/// What the cache needs of a wire protocol: run this operation and give back
/// the decoded, client-shaped response.
abstract interface class Transport {
  /// Runs [request]. Returns the client-shaped response.
  Future<Object?> execute(TransportRequest request);
}

/// One operation invocation, as the query cache hands it to a transport.
final class TransportRequest {
  /// Creates a request.
  const TransportRequest({
    required this.meta,
    required this.args,
    this.headers = const {},
    this.cancel,
  });

  /// The operation.
  final OperationMeta meta;

  /// Path, query, header and body arguments.
  final TagContext args;

  /// Extra headers for this call.
  final Map<String, String> headers;

  /// Completing this future aborts the request.
  final Future<void>? cancel;
}

/// Where credentials come from, and how they are renewed.
abstract interface class AuthProvider {
  /// An [AuthProvider] built from two callbacks. A null [refresh] means a 401
  /// is final: it is surfaced without a refresh or a retry.
  factory AuthProvider.callbacks({
    FutureOr<Map<String, String>?> Function(OperationMeta meta)? credentials,
    FutureOr<void> Function()? refresh,
  }) = _CallbackAuth;

  /// Headers to attach to this operation, per its declared security. Null
  /// attaches nothing.
  FutureOr<Map<String, String>?> credentials(OperationMeta meta);

  /// Renews the credential. Called at most once per 401 stampede. Completing
  /// normally means a new credential is available from [credentials]; failing
  /// means the 401 stands.
  FutureOr<void> refresh();
}

final class _CallbackAuth implements AuthProvider {
  _CallbackAuth({this._credentials, this._refresh});

  final FutureOr<Map<String, String>?> Function(OperationMeta meta)?
  _credentials;
  final FutureOr<void> Function()? _refresh;

  @override
  FutureOr<Map<String, String>?> credentials(OperationMeta meta) =>
      _credentials?.call(meta);

  @override
  FutureOr<void> refresh() => _refresh?.call();
}

/// Reads the wall clock, in milliseconds. Injected so nothing in this package
/// waits on real time under test.
abstract interface class Clock {
  /// Milliseconds since an arbitrary epoch.
  int now();
}

final class _RealClock implements Clock {
  const _RealClock();

  @override
  int now() => DateTime.now().millisecondsSinceEpoch;
}

/// The real wall clock.
const Clock realClock = _RealClock();

/// How a delay is taken. Injected so retry backoff is testable without timers.
typedef Sleep = Future<void> Function(Duration duration);

/// The default: a real timer.
Future<void> realSleep(Duration duration) => Future<void>.delayed(duration);

final class _Waiter {
  _Waiter(this.at);

  final int at;
  final Completer<void> wake = Completer<void>();
}

/// A clock that moves only when asked, plus a [sleep] that wakes on it.
final class ManualClock implements Clock {
  /// Creates a clock reading [start] milliseconds.
  ManualClock({int start = 0}) : _now = start;

  int _now;
  List<_Waiter> _waiting = <_Waiter>[];

  @override
  int now() => _now;

  /// How many sleeps are waiting.
  int get pending => _waiting.length;

  /// A [Sleep] that completes when [advance] moves past its due time.
  Future<void> sleep(Duration duration) {
    final waiter = _Waiter(_now + math.max(0, duration.inMilliseconds));
    _waiting.add(waiter);

    return waiter.wake.future;
  }

  /// Moves time forward and wakes every sleep that came due. The woken code
  /// runs on the microtask queue: await `pumpEventQueue()` before asserting.
  void advance(Duration duration) {
    _now += duration.inMilliseconds;

    final due = _waiting.where((waiter) => waiter.at <= _now).toList();
    _waiting = _waiting.where((waiter) => waiter.at > _now).toList();

    for (final waiter in due) {
      waiter.wake.complete();
    }
  }
}

/// Retry policy, applied to idempotent methods only. [attempts] counts the
/// first try.
final class RetryPolicy {
  /// Creates a policy.
  const RetryPolicy({
    this.attempts = 3,
    this.baseDelay = const Duration(milliseconds: 200),
    this.maxDelay = const Duration(seconds: 5),
  });

  /// Total attempts, including the first.
  final int attempts;

  /// The first backoff window.
  final Duration baseDelay;

  /// The cap on any one backoff window.
  final Duration maxDelay;
}

const Set<String> _idempotent = {'GET', 'HEAD', 'PUT', 'DELETE'};

/// Whether [error] is worth another attempt for [meta].
///
/// Only idempotent methods (`GET`, `HEAD`, `PUT`, `DELETE`) are retried: a
/// client cannot tell a request the server never saw from one it processed
/// and failed to acknowledge. A failure with no status is retried; an abort is
/// not; of the 4xx, only 408 and 429 are.
bool retryable(OperationMeta meta, Object error) =>
    _idempotent.contains(meta.method.toUpperCase()) && _retryableError(error);

bool _retryableError(Object error) {
  if (error is http.RequestAbortedException) return false;
  if (error is MissingPathParamsError) return false;

  final status = statusOf(error);

  if (status == null) return true;
  if (status < 400) return false;
  if (status < 500) return status == 408 || status == 429;

  return true;
}

/// The HTTP status an error carries, if any.
int? statusOf(Object error) => error is HttpStatusError ? error.status : null;

/// A non-2xx answer. The generated package maps it to its sealed `ApiError`
/// hierarchy at the `RestClient` boundary.
final class HttpStatusError implements Exception {
  /// Creates an error.
  const HttpStatusError(this.status, this.body, {this.headers = const {}});

  /// The HTTP status.
  final int status;

  /// The decoded JSON body when there was one, else the body text.
  final Object? body;

  /// The response headers.
  final Map<String, String> headers;

  @override
  String toString() => 'HTTP $status';
}

/// A path placeholder had no usable argument, so no request was sent.
///
/// A bug in the calling code rather than an answer from a server, and it
/// carries no status so [statusOf] can never mistake it for one.
final class MissingPathParamsError implements Exception {
  /// Creates the error.
  const MissingPathParamsError(this.operation, this.missing);

  /// `GET /orders/{id}`, or the bare path template when no method is known.
  final String operation;

  /// Every placeholder that resolved to nothing, in order.
  final List<String> missing;

  @override
  String toString() =>
      '$operation: no value for path parameter${missing.length > 1 ? 's' : ''} '
      '${missing.map((name) => '{$name}').join(', ')}. No request was sent.';
}

final RegExp _placeholder = RegExp(r'\{([^{}]*)\}');

/// A content type without its parameters, trimmed and lower-cased.
String _essence(String? contentType) =>
    (contentType ?? '').split(';').first.trim().toLowerCase();

const String _formType = 'application/x-www-form-urlencoded';

bool _isJson(String essence) =>
    essence == 'application/json' ||
    essence == 'text/json' ||
    essence.endsWith('+json');

const Set<String> _textApplicationTypes = {
  'application/ecmascript',
  'application/graphql',
  'application/javascript',
  'application/jsonl',
  'application/sql',
  'application/x-javascript',
  'application/x-ndjson',
  'application/x-www-form-urlencoded',
  'application/x-yaml',
  'application/xml',
  'application/yaml',
};

bool _isText(String essence) =>
    essence.startsWith('text/') ||
    essence.endsWith('+xml') ||
    essence.endsWith('+yaml') ||
    _textApplicationTypes.contains(essence);

final RegExp _charset = RegExp(
  r'charset\s*=\s*"?([^";\s]+)',
  caseSensitive: false,
);

/// Builds one operation's URL from its path template and arguments.
///
/// Query parameters are emitted in sorted key order and encoded exactly as the
/// browser's `URLSearchParams` encodes them, so a Dart URL and a TypeScript URL
/// for the same call are byte-identical. A path placeholder that renders to
/// nothing (null, missing, or the empty string) throws
/// [MissingPathParamsError] before anything is returned; a null query value is
/// skipped.
String operationUrl(String path, TagContext args, [String? operation]) {
  final missing = <String>[];

  var url = path.replaceAllMapped(_placeholder, (match) {
    final name = match.group(1)!.trim();
    final value = args.path[name];
    final rendered = value == null ? '' : Uri.encodeComponent(jsString(value));

    if (rendered.isEmpty) missing.add(name);

    return rendered;
  });

  if (missing.isNotEmpty) {
    throw MissingPathParamsError(operation ?? path, missing);
  }

  final parts = <String>[];

  for (final key in args.query.keys.toList()..sort()) {
    final value = args.query[key];

    if (value == null) continue;

    if (value is List<Object?>) {
      for (final item in value) {
        if (item != null) {
          parts.add('${_formEncode(key)}=${_formEncode(jsString(item))}');
        }
      }

      continue;
    }

    parts.add('${_formEncode(key)}=${_formEncode(jsString(value))}');
  }

  if (parts.isNotEmpty) {
    url += '${url.contains('?') ? '&' : '?'}${parts.join('&')}';
  }

  return url;
}

/// `application/x-www-form-urlencoded`, as `URLSearchParams.toString()`
/// writes it: alphanumerics and `*-._` stay, a space becomes `+`, everything
/// else is percent-encoded UTF-8 with upper-case hex.
String _formEncode(String value) {
  final out = StringBuffer();

  for (final byte in utf8.encode(value)) {
    final keep =
        (byte >= 0x30 && byte <= 0x39) ||
        (byte >= 0x41 && byte <= 0x5a) ||
        (byte >= 0x61 && byte <= 0x7a) ||
        byte == 0x2a ||
        byte == 0x2d ||
        byte == 0x2e ||
        byte == 0x5f;

    if (keep) {
      out.writeCharCode(byte);
    } else if (byte == 0x20) {
      out.write('+');
    } else {
      out.write('%${byte.toRadixString(16).toUpperCase().padLeft(2, '0')}');
    }
  }

  return out.toString();
}

/// One request, as TypeScript's `RequestReport` names it. Every
/// [RequestEvent] copies its [id], [meta] and [args]. [id] is per transport
/// and monotonic, so two identical concurrent calls stay two requests.
final class RequestReport {
  /// Creates a report.
  const RequestReport({
    required this.id,
    required this.meta,
    required this.args,
  });

  /// Names one request across all of its events.
  final int id;

  /// The operation.
  final OperationMeta meta;

  /// The arguments.
  final TagContext args;
}

/// What the transport did, reported as it happens: every TypeScript
/// `RequestEvent` variant, each carrying the request's [id], [meta] and [args]
/// and the clock reading [at] which it happened. Devtools reads refresh timing
/// from a [RequestRefresh] and the [RequestRefreshed] that follows it.
sealed class RequestEvent {
  /// Creates an event.
  RequestEvent(RequestReport request, this.at)
    : id = request.id,
      meta = request.meta,
      args = request.args;

  /// Names one request across all of its events.
  final int id;

  /// The operation.
  final OperationMeta meta;

  /// The arguments.
  final TagContext args;

  /// The transport clock reading, in milliseconds.
  final int at;
}

/// Dispatched. [limit] is how many attempts this method is allowed at all:
/// 1 for a method that is not idempotent.
final class RequestStarted extends RequestEvent {
  /// Creates the event.
  RequestStarted(super.request, super.at, {required this.limit});

  /// Attempts allowed.
  final int limit;
}

/// One attempt is going out. Zero-based.
final class RequestAttempt extends RequestEvent {
  /// Creates the event.
  RequestAttempt(super.request, super.at, {required this.attempt});

  /// The attempt number, from 0.
  final int attempt;
}

/// A 401 sent this request to the credential refresh. [joined] is true when
/// another request's refresh was already running.
final class RequestRefresh extends RequestEvent {
  /// Creates the event.
  RequestRefresh(super.request, super.at, {required this.joined});

  /// Whether this request adopted a refresh already in flight.
  final bool joined;
}

/// The refresh this request waited on has finished, either way.
final class RequestRefreshed extends RequestEvent {
  /// Creates the event.
  RequestRefreshed(super.request, super.at, {required this.ok});

  /// Whether the refresh succeeded.
  final bool ok;
}

/// An attempt failed retryably and the next one waits [delay].
final class RequestRetried extends RequestEvent {
  /// Creates the event.
  RequestRetried(
    super.request,
    super.at, {
    required this.attempt,
    required this.delay,
    this.status,
  });

  /// The attempt that failed.
  final int attempt;

  /// The backoff before the next attempt.
  final Duration delay;

  /// The failed attempt's status, if it had one.
  final int? status;
}

/// The request is done. [status] is the failure's status, if any.
final class RequestSettled extends RequestEvent {
  /// Creates the event.
  RequestSettled(super.request, super.at, {required this.ok, this.status});

  /// Whether it succeeded.
  final bool ok;

  /// The failure's status, if it had one.
  final int? status;
}

/// The debug seam. One slot, unset in production; an unwatched transport
/// allocates no event objects.
typedef RequestObserver = void Function(RequestEvent event);

/// The REST transport, on `package:http`.
///
/// Retries idempotent methods only, with exponential backoff and jitter.
/// Refreshes a credential once per 401 stampede rather than once per request.
/// Applies the operation's wire codecs: the request body is encoded and the
/// response decoded, so the cache sees client-shaped values.
final class RestTransport implements Transport {
  /// Creates a transport.
  ///
  /// [random] is the jitter source in `[0, 1)`; tests pin it. [clock] stamps
  /// observer events. [timeout] bounds each attempt.
  RestTransport({
    required Uri baseUrl,
    http.Client? client,
    this._auth,
    this._retry = const RetryPolicy(),
    this._clock = realClock,
    this._sleep = realSleep,
    this._timeout,
    this._headers = const {},
    this._observer,
    double Function()? random,
  }) : _base = baseUrl.toString().endsWith('/')
           ? baseUrl.toString().substring(0, baseUrl.toString().length - 1)
           : baseUrl.toString(),
       _client = client ?? http.Client(),
       _ownsClient = client == null,
       _random = random ?? math.Random().nextDouble;

  final String _base;
  final http.Client _client;
  final bool _ownsClient;
  final AuthProvider? _auth;
  final RetryPolicy _retry;
  final Clock _clock;
  final Sleep _sleep;
  final Duration? _timeout;
  final Map<String, String> _headers;
  final RequestObserver? _observer;
  final double Function() _random;

  Future<void>? _refreshing;
  int _generation = 0;
  int _requests = 0;

  /// Releases the HTTP client this transport created. A client passed in is
  /// left open: whoever made it closes it.
  void close() {
    if (_ownsClient) _client.close();
  }

  bool get _canRefresh => switch (_auth) {
    null => false,
    _CallbackAuth(:final _refresh) => _refresh != null,
    _ => true,
  };

  @override
  Future<Object?> execute(TransportRequest request) async {
    final meta = request.meta;
    final method = meta.method.toUpperCase();
    // Throws before anything is sent, outside the retry loop.
    final url = Uri.parse(
      _base + operationUrl(meta.path, request.args, '$method ${meta.path}'),
    );
    final limit = _idempotent.contains(method) ? _retry.attempts : 1;
    final report = _observer == null
        ? null
        : RequestReport(id: ++_requests, meta: meta, args: request.args);

    var cancelled = false;
    unawaited(request.cancel?.whenComplete(() => cancelled = true));

    _emit(report, (at) => RequestStarted(report!, at, limit: limit));

    for (var attempt = 0; ; attempt++) {
      _emit(report, (at) => RequestAttempt(report!, at, attempt: attempt));

      try {
        if (cancelled) throw http.RequestAbortedException(url);

        final value = await _send(method, url, request, report);

        _emit(report, (at) => RequestSettled(report!, at, ok: true));

        return value;
      } on Object catch (error) {
        final status = statusOf(error);

        if (attempt + 1 >= limit || !_retryableError(error)) {
          _emit(
            report,
            (at) => RequestSettled(report!, at, ok: false, status: status),
          );

          rethrow;
        }

        final delay = _backoff(attempt);

        _emit(
          report,
          (at) => RequestRetried(
            report!,
            at,
            attempt: attempt,
            delay: delay,
            status: status,
          ),
        );

        await _sleep(delay);
      }
    }
  }

  void _emit(RequestReport? report, RequestEvent Function(int at) build) {
    final observer = _observer;

    if (observer == null || report == null) return;

    try {
      observer(build(_clock.now()));
    } on Object {
      // The observer is a debug seam: it must never fail a request, turn a
      // success into a retry, or replace a request's own error. The transport
      // has no error channel of its own to send this to, so it is dropped.
    }
  }

  /// One attempt, including the 401 path. The refresh retry applies to every
  /// method, because a 401 says the server rejected the request before acting
  /// on it, and it is strictly one retry.
  Future<Object?> _send(
    String method,
    Uri url,
    TransportRequest request,
    RequestReport? report,
  ) async {
    // Read after the credentials, so a refresh landing while they are fetched
    // does not make this request look older than its credential.
    final credentials = await _auth?.credentials(request.meta);
    final generation = _generation;

    try {
      return await _request(method, url, request, credentials);
    } on Object catch (error, stack) {
      if (!_canRefresh || statusOf(error) != 401) rethrow;

      // Behind the current reading: someone else's refresh already landed.
      if (generation == _generation) {
        try {
          await _refresh(report);
          _emit(report, (at) => RequestRefreshed(report!, at, ok: true));
        } on Object {
          _emit(report, (at) => RequestRefreshed(report!, at, ok: false));

          // The 401 stands; the refresh's own error answers a question the
          // caller did not ask.
          Error.throwWithStackTrace(error, stack);
        }
      }

      return _request(
        method,
        url,
        request,
        await _auth?.credentials(request.meta),
      );
    }
  }

  /// One refresh, however many callers arrive while it runs.
  Future<void> _refresh(RequestReport? report) {
    _emit(
      report,
      (at) => RequestRefresh(report!, at, joined: _refreshing != null),
    );

    final running = _refreshing;

    if (running != null) return running;

    final auth = _auth!;

    Future<void> flight() async {
      // Yield first, so `_refreshing` is assigned before anything below can
      // clear it, even when `refresh` throws synchronously.
      await Future<void>.value();

      try {
        await auth.refresh();
        _generation++;
      } finally {
        // Cleared before any waiter resumes, so a 401 after this refresh
        // landed starts a new one.
        _refreshing = null;
      }
    }

    return _refreshing = flight();
  }

  Future<Object?> _request(
    String method,
    Uri url,
    TransportRequest request,
    Map<String, String>? credentials,
  ) async {
    final abortSources = <Future<void>>[];
    Completer<void>? timeoutAbort;
    if (_timeout != null) {
      timeoutAbort = Completer<void>();
      abortSources.add(timeoutAbort.future);
    }
    if (request.cancel != null) {
      abortSources.add(request.cancel!);
    }
    final abortTrigger = abortSources.isEmpty ? null : Future.any(abortSources);

    try {
      final outgoing =
          http.AbortableRequest(method, url, abortTrigger: abortTrigger)
            ..headers.addAll(_headers)
            ..headers.addAll(request.args.headers)
            ..headers.addAll(request.headers);

      if (credentials != null) outgoing.headers.addAll(credentials);

      final body = request.args.body;

      if (body != null) _writeBody(outgoing, request.meta, body);

      Future<http.Response> sendAndRead() async {
        final sending = _client.send(outgoing);
        final streamed = await sending;
        return await http.Response.fromStream(streamed);
      }

      final timeout = _timeout;
      final Future<http.Response> baseResponseFuture = timeout == null
          ? sendAndRead()
          : sendAndRead().timeout(timeout);

      // Race against abort trigger to throw immediately if cancelled
      final response = abortTrigger == null
          ? await baseResponseFuture
          : await Future.any<http.Response>([
              baseResponseFuture,
              abortTrigger.then((_) => throw http.RequestAbortedException(url)),
            ]);

      if (response.statusCode < 200 || response.statusCode >= 300) {
        throw HttpStatusError(
          response.statusCode,
          _decodeErrorBody(response),
          headers: response.headers,
        );
      }

      if (response.bodyBytes.isEmpty) return null;

      final decoded = _decode(response);
      final codec = request.meta.responseCodec;

      return codec == null ? decoded : codec.decode(decoded);
    } on TimeoutException {
      if (timeoutAbort != null && !timeoutAbort.isCompleted) {
        timeoutAbort.complete();
      }
      rethrow;
    }
  }

  /// Encodes [body] onto [outgoing] by the row's request content type, under
  /// the shared rule (see [OperationMeta.requestContentType]). A content-type
  /// header the caller already set wins over the declared one. Throws
  /// [ArgumentError] before anything is sent when the body cannot be sent as
  /// its declared kind.
  static void _writeBody(
    http.Request outgoing,
    OperationMeta meta,
    Object body,
  ) {
    final declared = meta.requestContentType;

    if (declared == null || _isJson(_essence(declared))) {
      final codec = meta.bodyCodec;
      final wire = codec == null ? body : codec.encode(body);

      if (wire is Uint8List && declared == null) {
        outgoing.headers.putIfAbsent(
          'content-type',
          () => 'application/octet-stream',
        );
        outgoing.bodyBytes = wire;

        return;
      }

      outgoing.headers.putIfAbsent(
        'content-type',
        () => declared ?? 'application/json',
      );
      outgoing.body = jsonEncode(wire);

      return;
    }

    outgoing.headers.putIfAbsent('content-type', () => declared);

    switch (body) {
      case final Uint8List bytes:
        outgoing.bodyBytes = bytes;
      case final String text when _isText(_essence(declared)):
        outgoing.body = text;
      case final Map<Object?, Object?> fields
          when _essence(declared) == _formType:
        // An Iterable value repeats its key once per non-null item, as the
        // query string does (`tag=a&tag=b`), rather than going out as the
        // list's toString.
        outgoing.body = Uri(
          queryParameters: {
            for (final MapEntry(:key, :value) in fields.entries)
              if (value is Iterable<Object?>)
                '$key': [
                  for (final item in value)
                    if (item != null) '$item',
                ]
              else if (value != null)
                '$key': '$value',
          },
        ).query;
      default:
        throw ArgumentError.value(
          body,
          'body',
          '${meta.id} sends $declared, which takes '
              '${_essence(declared) == _formType
                  ? 'a Map of fields'
                  : _isText(_essence(declared))
                  ? 'a String'
                  : 'a Uint8List'}, not a ${body.runtimeType}',
        );
    }
  }

  /// Decodes by the one content-type rule every Forge Dart client applies:
  /// JSON is `application/json`, `text/json` or any `+json` type (always read
  /// as UTF-8); text is `text/*`, `+xml`, `+yaml` or one of a short list of
  /// textual `application/*` types (read in the charset it declares, else
  /// UTF-8); everything else is binary and comes back as the raw bytes,
  /// because decoding an image or a PDF as text would corrupt it. A response
  /// with no content type is JSON when it parses, else text. Throws
  /// FormatException if JSON parsing fails.
  Object? _decode(http.Response response) {
    if (response.bodyBytes.isEmpty) return null;

    final type = _essence(response.headers['content-type']);

    if (_isJson(type)) return jsonDecode(_text(response));

    if (type.isEmpty) {
      final text = _text(response);

      try {
        return jsonDecode(text);
      } on FormatException {
        return text;
      }
    }

    if (_isText(type)) return _text(response);

    return response.bodyBytes;
  }

  /// Decodes a response body, falling back to text if JSON parsing fails.
  /// Used only for error responses, whose body is JSON or text: a binary
  /// error body is read as text rather than handed over as bytes, so the
  /// status and a printable message survive.
  Object? _decodeErrorBody(http.Response response) {
    try {
      final decoded = _decode(response);

      return decoded is Uint8List ? _text(response) : decoded;
    } on FormatException {
      return _text(response);
    }
  }

  /// The body as text. A text body is read in the charset its content type
  /// declares; JSON, binary and a text body that declares none, one this
  /// platform has no codec for or one the bytes do not fit, is read as UTF-8.
  /// Malformed UTF-8 is replaced rather than thrown on.
  String _text(http.Response response) {
    final type = response.headers['content-type'];
    final essence = _essence(type);
    final bytes = response.bodyBytes;
    final charset = _isText(essence) && !_isJson(essence)
        ? _charset.firstMatch(type ?? '')?.group(1)
        : null;
    final encoding = charset == null ? null : Encoding.getByName(charset);

    if (encoding != null && encoding != utf8) {
      try {
        return encoding.decode(bytes);
      } on FormatException {
        // Fall through to UTF-8.
      }
    }

    return utf8.decode(bytes, allowMalformed: true);
  }

  /// Exponential backoff with jitter: half the window fixed, half random, so
  /// a herd of clients that lost one connection disperses.
  Duration _backoff(int attempt) {
    final window = math.min(
      _retry.maxDelay.inMicroseconds,
      _retry.baseDelay.inMicroseconds * math.pow(2, attempt).toInt(),
    );

    return Duration(
      microseconds: (window / 2 + _random() * (window / 2)).round(),
    );
  }
}
