/// Whether the forge devtools are compiled into this build.
///
/// False in release builds (`dart.vm.product`, which Flutter defines for every
/// release target, web included, and which AOT executables always carry) and
/// when the app passes `--dart-define=forge.devtools=false`. Because it is
/// `const`, dart2js and the AOT compiler remove every branch it guards and
/// everything only that branch reaches.
const bool kForgeDevtools =
    !bool.fromEnvironment('dart.vm.product') &&
    bool.fromEnvironment('forge.devtools', defaultValue: true);
