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
export 'src/invalidate.dart'
    show ForgeInvalidation, invalidateBinding, refetchBinding;
export 'src/listener.dart' show ForgeListener;
export 'src/mutation_builder.dart' show ForgeMutation, ForgeMutationBuilder;
export 'src/queries_builder.dart' show ForgeCombinedStatus, ForgeQueriesBuilder, ForgeQueriesState;
export 'src/query_builder.dart' show ForgeQueryBuilder;
export 'src/scope.dart' show ForgeContext, ForgeScope;
export 'src/state_equality.dart' show firstState, sameQueryState;
