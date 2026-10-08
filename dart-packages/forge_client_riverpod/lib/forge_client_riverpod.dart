/// Riverpod 3 providers for forge_client: query families that yield
/// `AsyncValue`, mutation notifiers, and the same frame, focus and reconnect
/// seams as forge_client_flutter. No codegen.
library;

// Declared in forge_client_flutter; re-exported for the code an app writes
// against this package, so it does not import forge_client_flutter itself.
export 'package:forge_client_flutter/forge_client_flutter.dart'
    show
        ConnectivityPlusSignal,
        frameCommitScheduler,
        invalidateBinding,
        refetchBinding;

export 'src/client_provider.dart'
    show
        forgeClientProvider,
        forgeConnectivitySignalProvider,
        forgeFocusSignalProvider,
        forgeInstalledClientProvider;
export 'src/mutation_provider.dart'
    show ForgeMutationNotifier, mutationProvider;
export 'src/query_provider.dart'
    show
        ForgeQueryFamily,
        ForgeQueryParams,
        ForgeQueryStateNotifier,
        queryProvider;
