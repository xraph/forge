# forge_client_riverpod

Riverpod 3 providers over `forge_client`.

```dart
final getOrderProvider = queryProvider(getOrder);

ProviderScope(
  overrides: [forgeClientProvider.overrideWithValue(client)],
  child: const App(),
);

final order = ref.watch(getOrderProvider(const GetOrderArgs(id: '7')));
```
