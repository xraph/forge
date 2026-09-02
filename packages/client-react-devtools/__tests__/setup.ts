export {}; // A top-level `await` and a `declare global` both require a module.

/**
 * React only permits `act()` when it is told it is in a test environment, and
 * it warns on every update that is not wrapped when it is not.
 */
declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

/**
 * Pre-warm the modules `src/dev.ts` loads with a dynamic `import()`.
 *
 * `mount.test.tsx`'s `settle()` only drains the microtask queue -- it never
 * yields to Node's event loop, so it can never observe a dynamic import that
 * is still doing real file I/O. That is fine for the import a component
 * effect triggers *after* the module graph is already warm: a repeat
 * `import()` of an already-loaded ES module resolves off the module cache in
 * a handful of microtasks, no I/O involved. It is not fine for the *first*
 * touch, which is exactly what a fresh test process gives you. Doing that
 * first touch here, with a genuine (non-looping) `await` that lets the event
 * loop actually run, means every test's `settle()` only ever meets the fast
 * path.
 */
await import('@forge-go/client-devtools');
await import('@forge-go/client-devtools/panel');
await import('@forge-go/client-devtools/overlay');
