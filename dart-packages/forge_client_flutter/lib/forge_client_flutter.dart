/// Flutter widgets for forge_client: scopes, query and mutation builders,
/// local state, and the frame, focus and reconnect seams. No state library
/// required.
library;

// Declared in forge_client; re-exported so imports of this package keep working.
export 'package:forge_client/forge_client.dart' show OutboxFailureSource;

export 'src/seams.dart'
    show
        AppLifecycleFocusSignal,
        ConnectivityPlusSignal,
        flutterSeamsInstalled,
        frameCommitScheduler,
        installFlutterSeams;
export 'src/invalidate.dart'
    show ForgeInvalidation, invalidateBinding, refetchBinding;
export 'src/listener.dart' show ForgeListener;
export 'src/local_state.dart'
    show
        ForgeComputed,
        ForgeComputedKey,
        ForgeReader,
        ForgeState,
        ForgeStateKey;
export 'src/mutation_builder.dart' show ForgeMutation, ForgeMutationBuilder;
export 'src/queries_builder.dart'
    show ForgeCombinedStatus, ForgeQueriesBuilder, ForgeQueriesState;
export 'src/outbox_listener.dart' show ForgeOutboxListener;
export 'src/query_builder.dart' show ForgeQueryBuilder;
export 'src/restore_boundary.dart' show ForgeRestoreBoundary;
export 'src/scope.dart' show ForgeContext, ForgeScope;
export 'src/state_equality.dart' show firstState, sameQueryState;
