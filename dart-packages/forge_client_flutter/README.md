# forge_client_flutter

Flutter widgets over `forge_client`. An app needs no state library to use Forge well.

```dart
ForgeScope(
  client: configureClient(transport: transport, entities: entities, commitScheduler: frameCommitScheduler()),
  child: ForgeQueryBuilder(
    query: getOrder(const GetOrderArgs(id: '7')),
    builder: (context, state) => switch (state) {
      QueryIdle() || QueryLoading() => const CircularProgressIndicator(),
      QuerySuccess(:final data) => Text(data.note ?? ''),
      QueryFailure(:final error) => Text('$error'),
    },
  ),
)
```

## Framework features used

Checked against the Flutter 3.38 to 3.47 release notes and breaking-changes pages. None of the breaking changes in those releases touches an API listed here.

- `AppLifecycleListener` (Flutter 3.13) for focus revalidation, never `WidgetsBindingObserver`. Flutter 3.38 moved iOS apps to the UIScene lifecycle; scene events still arrive as `AppLifecycleState`, so the listener needs no change, but an iOS app built against the Xcode 27 SDK must adopt UIScene.
- `SchedulerBinding.scheduleFrameCallback` and `SchedulerBinding.framesEnabled` for per-frame commits.
- `InheritedWidget`, with `dependOnInheritedWidgetOfExactType` for builders and the non-subscribing `getInheritedWidgetOfExactType` for event handlers.
- `ValueNotifier`, `ChangeNotifier` and `ValueListenable` for local state, consumed with `ValueListenableBuilder` or `ListenableBuilder`.
- Dart: sealed classes and exhaustive switches (3.0), null-aware map entries (3.8), dot shorthands (3.10, shipped with Flutter 3.38).
- Tests drive the lifecycle through `SystemChannels.lifecycle` on `TestDefaultBinaryMessenger`, which generates the intermediate states the engine would.
