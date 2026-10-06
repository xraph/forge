/// The forge devtools: why the cache did what it did.
///
/// `configureClient` attaches these automatically in debug and profile
/// builds; import this library to register a cache built with the
/// `QueryCache` constructor, to hand the panel an `OutboxInspector`, or to
/// call the inspector from your own code.
///
/// `configureClient`, [registerForgeServiceExtensions], [unregisterForgeDevtools]
/// and [forgeDevtoolsFor] check the constant [kForgeDevtools] and do nothing
/// when it is false, so a release build attaches nothing and the compiler
/// drops the code only they reach. The rest of this library ([attach],
/// [Devtools], [ControlledTransport] and the other types) is not gated: an app
/// that calls it directly keeps what it calls in a release build too, so guard
/// those calls with [kForgeDevtools] yourself.
library;

export 'src/devtools/actions.dart' show DevtoolsActions;
export 'src/devtools/control.dart';
export 'src/devtools/devtools.dart' show Devtools, FrameOptions, attach;
export 'src/devtools/explain.dart'
    show MissCause, OperationCause, TagsCause, argsKey;
export 'src/devtools/inspect.dart' show EntityFilter;
export 'src/devtools/release.dart';
export 'src/devtools/requests.dart';
export 'src/devtools/service_extensions.dart'
    show
        forgeDevtoolsFor,
        registerForgeServiceExtensions,
        unregisterForgeDevtools;
export 'src/devtools/tag.dart';
export 'src/devtools/types.dart';
export 'src/observe.dart' show DevtoolsInspectable, OutboxInspector;
