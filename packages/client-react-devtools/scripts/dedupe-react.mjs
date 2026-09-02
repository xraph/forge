#!/usr/bin/env node
/**
 * Point this package's own `react` and `react-dom` at the copies
 * `@forge-go/client-react` already resolves, instead of the separate ones
 * `npm install` just gave us.
 *
 * This repo has no npm workspaces: every package is its own independent
 * `npm install`, and a `file:../x` dependency is a plain symlink to a
 * directory that already has its own `node_modules`. Nothing hoists or
 * dedupes across that boundary. That is harmless for a dependency with no
 * shared mutable state, but React is exactly the wrong kind of dependency for
 * it: two separately installed copies -- even the identical version -- are
 * two separate module instances, each with its own hook dispatcher and its
 * own element marker symbol (`react.element` in 18, `react.transitional.element`
 * in 19). A tree built by one copy's `createElement` and rendered by another
 * copy's `createRoot` fails outright: React 18/19 rendering React 19/18
 * elements throws "Objects are not valid as a React child", and calling a
 * hook through the *other* copy's dispatcher throws "Invalid hook call" even
 * when both copies claim the same version number.
 *
 * `@forge-go/client-react` is a devDependency here purely so its tests can
 * mount `<ClientProvider>`, and its own already-installed `react` /
 * `react-dom` are exactly the copies its compiled `dist/context.js` will
 * load -- Node resolves `require('react')` from a file's own directory
 * upward, never sideways into a sibling package, so nothing this package
 * does can make `client-react` load a different copy. The only fix is the
 * other direction: make our own `node_modules/react` and
 * `node_modules/react-dom` the same physical modules `client-react` uses.
 *
 * Scoped to this package's own `node_modules` only -- nothing under
 * `client-react` is read for anything but locating its own install, let
 * alone written.
 */
import { existsSync, lstatSync, realpathSync, rmSync, symlinkSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const root = dirname(here);
const sibling = join(root, '..', 'client-react', 'node_modules');

function relink(name) {
  const ours = join(root, 'node_modules', name);
  const theirs = join(sibling, name);

  if (!existsSync(theirs)) {
    // client-react has no local copy of its own (e.g. it was installed
    // differently) -- nothing to align to, leave our own install alone.
    return;
  }

  if (existsSync(ours) && realpathSync(ours) === realpathSync(theirs)) {
    return; // already the same physical module
  }

  if (lstatSync(ours, { throwIfNoEntry: false }) !== undefined) {
    rmSync(ours, { recursive: true, force: true });
  }

  symlinkSync(join('..', '..', 'client-react', 'node_modules', name), ours);
  console.log(`client-react-devtools: ${name} -> client-react's copy`);
}

relink('react');
relink('react-dom');
