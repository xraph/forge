import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:forge_client_devtools/forge_client_devtools.dart';

import 'fake_backend.dart';

/// Pumps the panel inside what `DevToolsExtension` provides: a MaterialApp
/// and a Scaffold.
Future<FakeForgeBackend> pumpPanel(
  WidgetTester tester, [
  FakeForgeBackend? backend,
]) async {
  final fake = backend ?? FakeForgeBackend();
  await tester.binding.setSurfaceSize(const Size(1600, 1000));
  addTearDown(() => tester.binding.setSurfaceSize(null));
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(body: ForgeDevtoolsPanel(backend: fake)),
    ),
  );
  await tester.pumpAndSettle();
  return fake;
}

/// Taps the tab labelled [label] and waits for the panel to load.
Future<void> openTab(WidgetTester tester, String label) async {
  await tester.tap(find.widgetWithText(Tab, label));
  await tester.pumpAndSettle();
}
