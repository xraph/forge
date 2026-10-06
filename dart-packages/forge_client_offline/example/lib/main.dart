import 'package:flutter/material.dart';

void main() => runApp(const ExampleApp());

/// A placeholder app. The real work is in integration_test/keystore_test.dart,
/// which runs inside this app so it can reach the macOS Keychain.
class ExampleApp extends StatelessWidget {
  const ExampleApp({super.key});

  @override
  Widget build(BuildContext context) => const MaterialApp(
    home: Scaffold(body: Center(child: Text('forge_client_offline example'))),
  );
}
