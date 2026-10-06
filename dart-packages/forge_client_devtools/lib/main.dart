import 'package:devtools_extensions/devtools_extensions.dart';
import 'package:flutter/material.dart';

import 'src/backend/vm_backend.dart';
import 'src/ui/app.dart';

void main() => runApp(const ForgeDevtoolsExtension());

/// The extension root: DevTools' chrome around the forge panel.
class ForgeDevtoolsExtension extends StatefulWidget {
  /// Creates the root.
  const ForgeDevtoolsExtension({super.key});

  @override
  State<ForgeDevtoolsExtension> createState() => _ForgeDevtoolsExtensionState();
}

class _ForgeDevtoolsExtensionState extends State<ForgeDevtoolsExtension> {
  VmForgeBackend? _backend;

  // The backend is built inside DevToolsExtension's subtree, after it has set
  // up `serviceManager`.
  @override
  Widget build(BuildContext context) => DevToolsExtension(
    child: Builder(
      builder: (context) =>
          ForgeDevtoolsPanel(backend: _backend ??= VmForgeBackend()),
    ),
  );
}
