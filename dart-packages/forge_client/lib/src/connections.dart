/// The stream connections for the platform this code runs on: native
/// (`connections_io.dart`) or browser (`connections_web.dart`).
library;

export 'connections_io.dart'
    if (dart.library.js_interop) 'connections_web.dart';
