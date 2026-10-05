/// Flutter widgets for forge_client: scopes, query and mutation builders,
/// local state, and the frame, focus and reconnect seams. No state library
/// required.
library;

export 'src/seams.dart'
    show
        AppLifecycleFocusSignal,
        ConnectivityPlusSignal,
        flutterSeamsInstalled,
        frameCommitScheduler,
        installFlutterSeams;
export 'src/scope.dart' show ForgeContext, ForgeScope;
