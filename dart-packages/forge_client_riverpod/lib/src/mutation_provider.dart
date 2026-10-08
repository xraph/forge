import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_riverpod/misc.dart' show ProviderException;
import 'package:forge_client/forge_client.dart';

import 'client_provider.dart';
import 'internal.dart';

/// Turns a mutation binding into an auto-dispose notifier provider. Declare
/// it once, at the top level:
///
/// ```dart
/// final updateOrderProvider = mutationProvider(updateOrder);
///
/// final status = ref.watch(updateOrderProvider); // MutationState<Order>
/// ref.read(updateOrderProvider.notifier).mutate(UpdateOrderArgs(id: id, note: Assign(note)));
/// ```
///
/// One provider is one status, shared by everything that watches it: two
/// widgets watching `updateOrderProvider` see the same pending, success or
/// failure. Declare a second provider, or use `ForgeMutationBuilder`, for
/// independent statuses. The provider auto-disposes, so watch it for as long
/// as you want its status; a call whose provider was disposed still runs to
/// its end and resolves for its caller, and records nothing.
///
/// The status belongs to the client and principal the call ran for. A
/// `setPrincipal` or a new `forgeClientProvider` returns it to idle, and a
/// call still in flight across the change is not recorded when it lands.
NotifierProvider<ForgeMutationNotifier<R, A, E>, MutationState<R>>
mutationProvider<R, A extends OperationArgs, E>(
  MutationBinding<R, A, E> binding, {
  String? name,
}) =>
    NotifierProvider.autoDispose<
      ForgeMutationNotifier<R, A, E>,
      MutationState<R>
    >(
      () => ForgeMutationNotifier<R, A, E>(binding),
      name: name,
      retry: noRetry,
    );

/// Runs one mutation binding and holds its [MutationState], for
/// [mutationProvider].
final class ForgeMutationNotifier<R, A extends OperationArgs, E>
    extends Notifier<MutationState<R>> {
  /// Creates a notifier for [binding].
  ForgeMutationNotifier(this.binding);

  /// The binding this notifier calls.
  final MutationBinding<R, A, E> binding;

  // Distinguishes overlapping calls, so the first response landing after
  // the second does not overwrite it. reset bumps it too.
  int _seq = 0;

  // The current build's inbox, which holds a state written mid-build.
  StateInbox<MutationState<R>>? _inbox;

  @override
  MutationState<R> build() => building(() {
    final inbox = _inbox = StateInbox<MutationState<R>>(
      ref,
      (next) => state = next,
    );
    ref.onDispose(inbox.close);
    // Watched so that a new client or a new principal rebuilds this
    // notifier back to idle, and unmounts the Ref a call in flight holds.
    final client = ref.watch(forgeInstalledClientProvider);
    ref.watch(principalProvider(client));
    return MutationIdle<R>();
  });

  /// Runs the mutation and rethrows on failure, after recording it.
  ///
  /// [optimistic] and [place] are handed to the binding as they are.
  /// [options] carries per-call headers and a cancel future to the transport.
  Future<R> mutateAsync(
    A args, {
    Optimistic<E>? optimistic,
    Map<String, Placement> place = const {},
    RequestOptions options = const RequestOptions(),
  }) async {
    final call = ++_seq;
    Ref? owner;
    try {
      // Everything that can throw is inside the try, so a missing client is
      // recorded as the call's failure rather than escaping mutate, which
      // promises not to throw. Reading state first brings this provider up
      // to date, so the call runs for the current client and principal.
      state;
      owner = ref;
      final client = ref.read(forgeInstalledClientProvider);
      _record(owner, call, MutationPending<R>());
      final data = await binding(
        client,
        args,
        optimistic: optimistic,
        place: place,
        options: options,
      );
      _record(owner, call, MutationSuccess<R>(data));
      return data;
    } on Object catch (error, stackTrace) {
      // A failed build (no client configured) arrives wrapped in Riverpod's
      // ProviderException. Record and throw what the provider threw.
      var cause = error;
      var trace = stackTrace;
      while (cause is ProviderException) {
        trace = cause.stackTrace;
        cause = cause.exception;
      }
      _record(owner ?? ref, call, MutationFailure<R>(cause));
      Error.throwWithStackTrace(cause, trace);
    }
  }

  /// Runs the mutation. Never throws: on failure it records the error and
  /// resolves with null.
  ///
  /// [options] carries per-call headers and a cancel future to the transport.
  Future<R?> mutate(
    A args, {
    Optimistic<E>? optimistic,
    Map<String, Placement> place = const {},
    RequestOptions options = const RequestOptions(),
  }) async {
    try {
      return await mutateAsync(
        args,
        optimistic: optimistic,
        place: place,
        options: options,
      );
    } on Object {
      // Recorded in the state by mutateAsync; this variant leaves it there.
      return null;
    }
  }

  /// Back to idle. Supersedes any call in flight, so its result is not
  /// recorded when it lands.
  void reset() {
    _record(ref, ++_seq, MutationIdle<R>());
  }

  /// Records [next] for [call], unless a later call or a reset superseded it
  /// or the build that [owner] belongs to is gone: the provider was disposed,
  /// or rebuilt for another client or principal.
  void _record(Ref owner, int call, MutationState<R> next) {
    if (!owner.mounted || call != _seq) return;
    // A new client or principal leaves this provider out of date until
    // Riverpod's scheduled refresh, which runs after this result's
    // microtask. Reading state rebuilds it now, which unmounts [owner], so
    // the write cannot land on the previous identity's state.
    try {
      state;
    } on Object {
      // The build failed (no client), so there is no newer build to wait
      // for; the failure is recorded over it.
    }
    if (!owner.mounted || call != _seq) return;
    _inbox?.receive(next);
  }

  @override
  bool updateShouldNotify(MutationState<R> previous, MutationState<R> next) =>
      !identical(previous, next);
}
