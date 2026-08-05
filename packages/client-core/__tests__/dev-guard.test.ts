// @vitest-environment node
//
// esbuild refuses to run under jsdom, whose `TextEncoder` does not produce a
// real `Uint8Array`. This file bundles things; nothing in it touches a DOM.
import { execFileSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createContext, runInNewContext } from 'node:vm';
import { build } from 'esbuild';
import type { Platform } from 'esbuild';
import { beforeAll, describe, expect, it } from 'vitest';

/**
 * What the development-only warning in `live.ts` costs a production build,
 * checked against the **built output**.
 *
 * A claim about a bundler folding a branch away is not settled by reading the
 * source. The previous version of this guard came with a comment asserting an
 * elimination that measurement showed did not happen, in code whose emitted
 * form -- `typeof process>"u"||!1` -- was *true* in a browser and warned at end
 * users. So this compiles the package with `tsc`, bundles `dist/` with esbuild
 * as an application would, and reads the bytes.
 *
 * Three properties, failing in different directions:
 *
 * - Built for the browser, the warn path is **gone**, not merely unreachable.
 *   The strings are the proof: a minifier renames bindings but does not touch
 *   string literals, so their absence means the branch and everything below it
 *   was dropped.
 * - Built for development, it is **still there and still fires**, so the
 *   absence above is a fold rather than a deletion.
 * - With nothing substituted, the runtime read decides -- and with no `process`
 *   to read, it throws. That is the stated cost of a foldable guard rather than
 *   an oversight, and the last case below pins it so it stays a decision.
 */

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, '..');

/**
 * Text that exists only on the warn path.
 *
 * String literals rather than identifiers on purpose: `warnUnknown`, `slot` and
 * `warned` are local bindings a minifier renames, so asserting on their names
 * would pass vacuously.
 */
const WARN_STRINGS = [
  'no stream binding for',
  'unbound stream message types; further warnings suppressed',
];

interface Bundle {
  readonly code: string;
  readonly bytes: number;
}

/**
 * Bundles the fixture below against the built `dist/`.
 *
 * `platform` is the whole experiment. For `browser`, esbuild substitutes
 * `process.env.NODE_ENV` on its own -- `"production"` when minifying,
 * `"development"` otherwise -- with no `define` configured, which is why the
 * bare spelling matters and why no application has to opt in. For `neutral` it
 * substitutes nothing, leaving the read for the runtime, which is the shape of
 * an unbundled load.
 */
async function bundle(platform: Platform, minify: boolean): Promise<Bundle> {
  const result = await build({
    stdin: { contents: FIXTURE, resolveDir: root, loader: 'js' },
    bundle: true,
    minify,
    format: 'iife',
    platform,
    target: 'es2020',
    write: false,
    logLevel: 'silent',
  });

  const output = result.outputFiles[0];

  if (output === undefined) throw new Error('esbuild produced nothing');

  return { code: output.text, bytes: output.contents.byteLength };
}

/**
 * Reaches the default `onUnknown` the shortest way there is.
 *
 * A binding whose intent is not one this client can act on is reported at
 * wiring time, straight from the `StreamBinder` constructor -- no socket, no
 * frame, no clock. Everything passed in is the least the constructors accept;
 * the report happens before any of it is used for anything else.
 */
const FIXTURE = `
import { QueryCache, SubscriptionManager, StreamBinder } from './dist/index.js';

globalThis.boot = function boot() {
  const cache = new QueryCache({
    transport: { request: () => Promise.resolve(null) },
    entities: {},
  });

  const manager = new SubscriptionManager({
    connect: () => ({ close() {}, send() {}, addEventListener() {}, removeEventListener() {} }),
  });

  new StreamBinder({
    cache,
    manager,
    streams: [
      {
        channel: '/ws/orders',
        message: 'order.created',
        entity: 'Order',
        intent: 'not-an-intent',
        invalidates: [],
      },
    ],
  });
};
`;

interface Run {
  readonly warnings: string[];
  readonly threw: unknown;
}

/**
 * Runs a bundle in a fresh realm and reports what its console saw.
 *
 * `createContext` over a bare object is the point: the realm has no ambient
 * Node globals, which is the shape of a browser and the case the old spelling
 * got backwards. `env` is the only way a `process` appears at all.
 */
function run(code: string, env?: Record<string, string>): Run {
  const warnings: string[] = [];
  const sandbox: Record<string, unknown> = {
    console: {
      warn: (text: string) => warnings.push(text),
      error: () => undefined,
      log: () => undefined,
    },
    setTimeout,
    clearTimeout,
    queueMicrotask,
    Promise,
  };

  if (env !== undefined) sandbox['process'] = { env };

  const context = createContext(sandbox);

  let threw: unknown;

  try {
    runInNewContext(code, context);
    runInNewContext('globalThis.boot();', context);
  } catch (error) {
    threw = error;
  }

  return { warnings, threw };
}

/** Browser production: minified, and esbuild substitutes `"production"` itself. */
let production: Bundle;
/** Browser development: unminified, and esbuild substitutes `"development"`. */
let development: Bundle;
/** No substitution at all, so the guard is decided at runtime. */
let runtime: Bundle;

beforeAll(async () => {
  // Against the built output, not the source. `dist/` is what a consumer
  // installs and what the fixture imports.
  execFileSync('npx', ['tsc'], { cwd: root, stdio: 'pipe' });

  expect(existsSync(resolve(root, 'dist/index.js'))).toBe(true);

  production = await bundle('browser', true);
  development = await bundle('browser', false);
  runtime = await bundle('neutral', true);
}, 120_000);

describe('a production browser bundle', () => {
  it('does not contain the warning at all', () => {
    // No `define` was configured for this build. A plain `esbuild --minify`
    // drops the path, which is the property the bare spelling buys and the one
    // the previous spelling did not have.
    for (const text of WARN_STRINGS) {
      expect(production.code).not.toContain(text);
    }
  });

  it('collapses the whole reporter to an empty function', () => {
    // The strongest form of the claim. `warnUnknown` cannot disappear outright
    // -- it is the `??` default for `onUnknown`, so something has to be there
    // to reference -- but its body, its set and its cap can, and this is what
    // is left once they have: a two-parameter shell.
    expect(production.code).toMatch(/function [A-Za-z$_0-9]*\([^)]*\)\{\}/);
  });

  it('does contain the runtime, so the check is not passing vacuously', () => {
    // Without this, a fixture that failed to pull `live.ts` in would satisfy
    // every assertion above and prove nothing.
    expect(production.code).toContain('order.created');
    expect(production.bytes).toBeGreaterThan(4000);
  });

  it('wires a bad binding without warning, in a realm with no process', () => {
    // The realm the old spelling misread. `typeof process === 'undefined'` was
    // true here, the disjunction survived the fold, and this warned at every
    // end user with a console open.
    const { warnings, threw } = run(production.code);

    expect(threw).toBeUndefined();
    expect(warnings).toEqual([]);
  });
});

describe('a development browser bundle', () => {
  it('keeps the warning, so the absence above is a fold and not a deletion', () => {
    for (const text of WARN_STRINGS) {
      expect(development.code).toContain(text);
    }
  });

  it('warns once about the binding it cannot act on', () => {
    const { warnings, threw } = run(development.code);

    expect(threw).toBeUndefined();
    expect(warnings).toHaveLength(1);
    expect(warnings[0]).toContain('no stream binding for');
  });
});

describe('a bundle with nothing substituted, deciding at runtime', () => {
  it('warns when a process says this is development', () => {
    // The other direction: if this goes quiet, the guard has stopped being a
    // guard and the warning is dead in every build.
    const { warnings, threw } = run(runtime.code, { NODE_ENV: 'development' });

    expect(threw).toBeUndefined();
    expect(warnings).toHaveLength(1);
    expect(warnings[0]).toContain('no stream binding for');
  });

  it('goes quiet again when that process says production', () => {
    const { warnings, threw } = run(runtime.code, { NODE_ENV: 'production' });

    expect(threw).toBeUndefined();
    expect(warnings).toEqual([]);
  });

  it('throws with no process at all, which is the price of a foldable guard', () => {
    // Pinned deliberately. `typeof` is the only safe probe for an undeclared
    // identifier, and `typeof` is exactly what a bundler cannot fold -- so a
    // guard that leaves nothing in a production build cannot also survive a
    // load with no build at all. This package is consumed through a bundler,
    // so that is the trade taken; the assertion is here so that taking it stays
    // a decision somebody made rather than a surprise somebody hits.
    const { threw } = run(runtime.code);

    // `instanceof Error` would be false here: the throw comes from the vm's own
    // realm and carries that realm's `Error`, not this one's.
    expect(threw).toBeDefined();
    expect(String(threw)).toContain('process is not defined');
  });
});
