/// The forge devtools: why the cache did what it did.
///
/// `configureClient` attaches these automatically in debug and profile
/// builds; import this library to register a cache built with the
/// `QueryCache` constructor, to hand the panel an `OutboxInspector`, or to
/// call the inspector from your own code. Release builds compile all of it
/// out, because every entry point checks the constant [kForgeDevtools].
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
