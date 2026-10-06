/// The wire between `forge_client` in the app isolate and the forge DevTools
/// extension. Both sides import it through
/// `package:forge_client/devtools_protocol.dart`, so a method name is written
/// once.
library;

/// Method names, the event kind and the paging limits.
abstract final class ForgeDevtoolsProtocol {
  /// Bumped on any incompatible change. The extension refuses a mismatch.
  static const int version = 1;

  /// Protocol version and attached caches.
  static const String hello = 'ext.forge.hello';

  /// Counters and status buckets.
  static const String snapshot = 'ext.forge.snapshot';

  /// A page of query summaries.
  static const String queries = 'ext.forge.queries';

  /// One query in detail.
  static const String query = 'ext.forge.query';

  /// A page of entity rows.
  static const String entities = 'ext.forge.entities';

  /// One entity in detail.
  static const String entity = 'ext.forge.entity';

  /// A page of the tag graph.
  static const String tags = 'ext.forge.tags';

  /// Why a query did or did not refetch.
  static const String explain = 'ext.forge.explain';

  /// Operations the devtools know about.
  static const String operations = 'ext.forge.operations';

  /// What an operation would invalidate.
  static const String wouldInvalidate = 'ext.forge.wouldInvalidate';

  /// A page of the event log.
  static const String log = 'ext.forge.log';

  /// A page of the frame ring.
  static const String frames = 'ext.forge.frames';

  /// Turns frame capture on or off.
  static const String capture = 'ext.forge.capture';

  /// The request log.
  static const String requests = 'ext.forge.requests';

  /// The overlay stack.
  static const String overlays = 'ext.forge.overlays';

  /// A panel action.
  static const String action = 'ext.forge.action';

  /// The network conditions and revalidation toggles.
  static const String control = 'ext.forge.control';

  /// The outbox.
  static const String outbox = 'ext.forge.outbox';

  /// Replay or discard one outbox write.
  static const String outboxAction = 'ext.forge.outboxAction';

  /// Sync sources and per-entity status.
  static const String sync = 'ext.forge.sync';

  /// Every method, in registration order.
  static const List<String> methods = [
    hello,
    snapshot,
    queries,
    query,
    entities,
    entity,
    tags,
    explain,
    operations,
    wouldInvalidate,
    log,
    frames,
    capture,
    requests,
    overlays,
    action,
    control,
    outbox,
    outboxAction,
    sync,
  ];

  /// The `postEvent` kind for batched log entries, and for the lifecycle
  /// events that say a cache attached or detached.
  static const String eventKind = 'forge:event';

  /// The field a lifecycle event carries in place of `entries`. Its value is
  /// [attached] or [detached], and the only other field is `cache`, the id.
  static const String lifecycle = 'lifecycle';

  /// A cache was attached: a new one the panel has not said hello to.
  static const String attached = 'attached';

  /// A cache was detached: disposed, unregistered, or evicted to make room.
  static const String detached = 'detached';

  /// The error code of a call refused because its cache is gone: disposed,
  /// detached, or not attached under that id. The panel says hello again when
  /// it sees it. It sits in the range `dart:developer` keeps for extension
  /// errors.
  static const int cacheGone = -32001;

  /// Page size when none is asked for.
  static const int defaultPage = 100;

  /// The largest page any method returns.
  static const int maxPage = 500;

  /// The longest tag or dependency list a detail carries.
  static const int maxListInDetail = 1000;

  /// The most log entries one event post carries.
  static const int maxEventsPerPost = 200;
}
