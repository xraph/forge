import 'package:flutter/widgets.dart';
import 'package:forge_client/forge_client.dart';

import 'local_state.dart';
import 'seams.dart';

/// Supplies a [QueryCache] to a subtree, installs the Flutter seams on it,
/// and owns the subtree's [ForgeState] and [ForgeComputed] values.
///
/// Optional for queries and mutations. Without a scope, widgets fall back to
/// the global [getClient], so a generated package configured with
/// `configureClient` works on its own. A scope is for what a global cannot
/// serve: a test that must not leak state, an app talking to two backends,
/// an account switch that swaps the cache, and local state that has to be
/// disposed with a screen.
///
/// While mounted it keeps [installFlutterSeams] installed on [client]. An app
/// that uses only the global client calls `installFlutterSeams(getClient())`
/// once at startup instead.
///
/// Changing [client], [installSeams], [focus] or [connectivity] reinstalls
/// the seams. Pass signals that live as long as the scope (a field, not an
/// object built inside a `build` method), or every rebuild reinstalls them.
/// When several scopes share one client, the first install's signals stay in
/// effect until every holder releases.
///
/// Changing [client] also moves every computed value's queries to the new
/// client. States keep their values.
final class ForgeScope extends StatefulWidget {
  /// Creates a scope over [client].
  const ForgeScope({
    super.key,
    required this.client,
    required this.child,
    this.focus,
    this.connectivity,
    this.installSeams = true,
  });

  /// The cache this subtree reads from.
  final QueryCache client;

  /// The subtree.
  final Widget child;

  /// The focus signal to install; [AppLifecycleFocusSignal] when null.
  final FocusSignal? focus;

  /// The connectivity signal to install; [ConnectivityPlusSignal] when null.
  final ConnectivitySignal? connectivity;

  /// Whether this scope installs focus and reconnect revalidation on [client].
  final bool installSeams;

  /// Resolves the cache for [context]: [client] when given, then the nearest
  /// scope, then [getClient], which throws a `StateError` when nothing was
  /// configured.
  ///
  /// With [listen] true the caller rebuilds when the nearest scope's client
  /// changes. Event handlers pass false.
  static QueryCache of(
    BuildContext context, {
    QueryCache? client,
    bool listen = true,
  }) => client ?? maybeOf(context, listen: listen) ?? getClient();

  /// The nearest scope's cache, or null when no scope is above [context].
  static QueryCache? maybeOf(BuildContext context, {bool listen = true}) {
    final scope = listen
        ? context.dependOnInheritedWidgetOfExactType<_ForgeInherited>()
        : context.getInheritedWidgetOfExactType<_ForgeInherited>();
    return scope?.client;
  }

  @override
  State<ForgeScope> createState() => _ForgeScopeState();
}

final class _ForgeScopeState extends State<ForgeScope> {
  late final ForgeScopeOwner _owner;
  void Function()? _uninstall;

  @override
  void initState() {
    super.initState();
    _owner = ForgeScopeOwner(widget.client);
    _uninstall = _install();
  }

  void Function()? _install() => widget.installSeams
      ? installFlutterSeams(
          widget.client,
          focus: widget.focus,
          connectivity: widget.connectivity,
        )
      : null;

  @override
  void didUpdateWidget(ForgeScope oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.client, widget.client) ||
        oldWidget.installSeams != widget.installSeams ||
        !identical(oldWidget.focus, widget.focus) ||
        !identical(oldWidget.connectivity, widget.connectivity)) {
      // Release before installing. Installation is ref-counted per cache, so
      // installing first on the same client would join the old installation
      // and keep its signals, and a swapped signal would never take effect.
      _uninstall?.call();
      _uninstall = _install();
    }
    // A no-op unless the client changed.
    _owner.client = widget.client;
  }

  @override
  void dispose() {
    _owner.dispose();
    _uninstall?.call();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => _ForgeInherited(
    client: widget.client,
    owner: _owner,
    child: widget.child,
  );
}

final class _ForgeInherited extends InheritedWidget {
  const _ForgeInherited({
    required this.client,
    required this.owner,
    required super.child,
  });

  final QueryCache client;
  final ForgeScopeOwner owner;

  @override
  bool updateShouldNotify(_ForgeInherited oldWidget) =>
      !identical(oldWidget.client, client);
}

/// The nearest scope's owner of local state. Throws a `StateError` when no
/// scope is above [context], because local state needs an owner to be
/// disposed with.
ForgeScopeOwner scopeOwnerOf(BuildContext context) {
  final scope = context.getInheritedWidgetOfExactType<_ForgeInherited>();
  if (scope == null) {
    throw StateError(
      'No ForgeScope above this context. ForgeState and ForgeComputed are '
      'owned by a scope, so wrap the subtree in a ForgeScope.',
    );
  }
  return scope.owner;
}

/// Resolves the cache and the scope's local state from a [BuildContext].
extension ForgeContext on BuildContext {
  /// The cache [ForgeScope.of] resolves for this context, without subscribing
  /// to scope changes. Safe in event handlers.
  QueryCache get forgeClient => ForgeScope.of(this, listen: false);

  /// The nearest scope's state for [key], created on first read. Listen to
  /// it with `ValueListenableBuilder`; reading it does not subscribe.
  ForgeState<T> forgeState<T>(ForgeStateKey<T> key) =>
      scopeOwnerOf(this).state(key);

  /// The nearest scope's computed value for [key], created on first read.
  /// Listen to it with `ValueListenableBuilder`; reading it does not
  /// subscribe.
  ForgeComputed<T> forgeComputed<T>(ForgeComputedKey<T> key) =>
      scopeOwnerOf(this).computed(key);
}
