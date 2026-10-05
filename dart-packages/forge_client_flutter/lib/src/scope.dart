import 'package:flutter/widgets.dart';
import 'package:forge_client/forge_client.dart';

import 'seams.dart';

/// Supplies a [QueryCache] to a subtree and installs the Flutter seams on it.
///
/// Optional. Without a scope, widgets fall back to the global [getClient], so
/// a generated package configured with `configureClient` works on its own. A
/// scope is for what a global cannot serve: a test that must not leak state,
/// an app talking to two backends, an account switch that swaps the cache.
///
/// While mounted it keeps [installFlutterSeams] installed on [client]. An app
/// that uses only the global client calls `installFlutterSeams(getClient())`
/// once at startup instead.
///
/// Changing [client], [installSeams], [focus] or [connectivity] reinstalls
/// the seams. Pass signals that live as long as the scope (a field, not an
/// object built inside a `build` method), or every rebuild reinstalls them.
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
  }) =>
      client ?? maybeOf(context, listen: listen) ?? getClient();

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
  void Function()? _uninstall;

  @override
  void initState() {
    super.initState();
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
    if (identical(oldWidget.client, widget.client) &&
        oldWidget.installSeams == widget.installSeams &&
        identical(oldWidget.focus, widget.focus) &&
        identical(oldWidget.connectivity, widget.connectivity)) {
      return;
    }
    // Release before installing. Installation is ref-counted per cache, so
    // installing first on the same client would join the old installation and
    // keep its signals, and a swapped signal would never take effect.
    _uninstall?.call();
    _uninstall = _install();
  }

  @override
  void dispose() {
    _uninstall?.call();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) =>
      _ForgeInherited(client: widget.client, child: widget.child);
}

final class _ForgeInherited extends InheritedWidget {
  const _ForgeInherited({required this.client, required super.child});

  final QueryCache client;

  @override
  bool updateShouldNotify(_ForgeInherited oldWidget) =>
      !identical(oldWidget.client, client);
}

/// Resolves the cache from a [BuildContext].
extension ForgeContext on BuildContext {
  /// The cache [ForgeScope.of] resolves for this context, without subscribing
  /// to scope changes. Safe in event handlers.
  QueryCache get forgeClient => ForgeScope.of(this, listen: false);
}
