// @vitest-environment node
//
// esbuild refuses to run under jsdom, whose `TextEncoder` does not produce a
// real `Uint8Array`. This file bundles things; nothing in it touches a DOM.
import { execFileSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { gzipSync } from 'node:zlib';
import { build } from 'esbuild';
import { beforeAll, describe, expect, it } from 'vitest';

/**
 * The binding constraint, checked against the **built output**.
 *
 * A claim about tree-shaking that is argued from the source is not a claim, it
 * is a hope. What matters is what a bundler emits, so this compiles the package
 * with `tsc`, bundles three fixture applications with esbuild exactly as a real
 * build would, and reads the bytes.
 *
 * Three fixtures, three different things proved:
 *
 * - `production.ts` uses the runtime and never mentions the devtools. If any
 *   emit site in the core dragged the package in behind it, the markers would
 *   be here.
 * - `instrumented.ts` imports it statically. The difference between the two is
 *   the entire cost of the tool, and it is a cost only this bundle pays.
 * - `guarded.ts` is the pattern applications are told to use: a dynamic import
 *   behind a `NODE_ENV` check. Built for production, the branch folds away, the
 *   `import()` goes with it, and no chunk is emitted -- so the bundle does not
 *   contain the inspector, does not request it, and does not know it exists.
 */

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, '..');

/**
 * Strings that exist only in this package.
 *
 * Deliberately a mix of identifiers and literals. A minifier renames local
 * bindings, so an identifier-only list would pass vacuously; the string
 * literals below are the hint text and the relation names, which no minifier
 * touches and which nothing else in the runtime contains.
 */
const MARKERS = [
  'instance-vs-collection',
  'stale-while-unmounted',
  // Composed at runtime around the typename, so the literal in the bundle is
  // the tail of the sentence rather than the whole of it.
  "` to the operation's Invalidates",
  'differ only in case',
  'never heard of',
  'the most common cause of an invalidation',
];

interface Bundle {
  readonly code: string;
  readonly bytes: number;
  readonly gzipped: number;
}

async function bundle(entry: string): Promise<Bundle> {
  const result = await build({
    entryPoints: [resolve(root, 'fixtures', entry)],
    bundle: true,
    minify: true,
    format: 'esm',
    platform: 'browser',
    target: 'es2020',
    // What a production build does, and the whole mechanism `guarded.ts`
    // relies on.
    define: { 'process.env.NODE_ENV': '"production"' },
    write: false,
    logLevel: 'silent',
  });

  const output = result.outputFiles[0];

  if (output === undefined) throw new Error(`esbuild produced nothing for ${entry}`);

  return {
    code: output.text,
    bytes: output.contents.byteLength,
    gzipped: gzipSync(output.contents).byteLength,
  };
}

let production: Bundle;
let instrumented: Bundle;
let guarded: Bundle;

beforeAll(async () => {
  // Against the built output, not the source. `dist/` is what the fixtures
  // import and what a consumer would install.
  execFileSync('npx', ['tsc'], { cwd: root, stdio: 'pipe' });

  expect(existsSync(resolve(root, 'dist/index.js'))).toBe(true);

  production = await bundle('production.ts');
  instrumented = await bundle('instrumented.ts');
  guarded = await bundle('guarded.ts');
}, 120_000);

describe('a production bundle that never imports the devtools', () => {
  it('contains not one byte of it', () => {
    for (const marker of MARKERS) {
      expect(production.code).not.toContain(marker);
    }

    // The identifiers too, for the ones a minifier preserves because they are
    // reachable through the package's exports.
    expect(production.code).not.toContain('whyNotRefetched');
    expect(production.code).not.toContain('nearMisses');
  });

  it('does contain the core, so the check is not passing vacuously', () => {
    // If the fixture had failed to pull the runtime in at all, every assertion
    // above would pass and prove nothing.
    expect(production.code).toContain('no client configured');
    expect(production.bytes).toBeGreaterThan(4000);
  });

  it('does not carry the socket inspector either, because nothing imports it', () => {
    // `socketSnapshot` is a free function rather than a method on
    // `SubscriptionManager` precisely so that this is true: a class method
    // cannot be tree-shaken and would have been paid for by every live
    // application.
    expect(production.code).not.toContain('reconnecting:');
  });
});

describe('the instrumented bundle', () => {
  it('contains all of it, which is what makes the absence above meaningful', () => {
    for (const marker of MARKERS) {
      expect(instrumented.code).toContain(marker);
    }
  });

  it('is larger by the whole size of the tool', () => {
    // Reported rather than merely asserted; the exact figure is in the chunk
    // report. The floor is here so that a future change which accidentally
    // tree-shakes the analysis *out of the instrumented build* -- making the
    // tool useless while making this file pass -- is caught.
    expect(instrumented.gzipped - production.gzipped).toBeGreaterThan(2000);
  });
});

describe('the dynamic-import pattern', () => {
  it('emits no chunk and no request when built for production', () => {
    for (const marker of MARKERS) {
      expect(guarded.code).not.toContain(marker);
    }

    // The branch folded, so the import call is gone rather than deferred.
    expect(guarded.code).not.toContain('import(');
    expect(guarded.code).not.toContain('client-devtools');
  });

  it('costs the production bundle nothing at all against the control', () => {
    // Byte for byte. The guarded fixture differs from the control only by the
    // branch that was removed.
    expect(guarded.gzipped).toBeLessThanOrEqual(production.gzipped);
  });
});

describe('sizes', () => {
  it('reports what each bundle costs', () => {
    const report = {
      production: `${String(production.bytes)} B raw / ${String(production.gzipped)} B gzipped`,
      guarded: `${String(guarded.bytes)} B raw / ${String(guarded.gzipped)} B gzipped`,
      instrumented: `${String(instrumented.bytes)} B raw / ${String(
        instrumented.gzipped,
      )} B gzipped`,
      devtoolsCost: `${String(instrumented.gzipped - production.gzipped)} B gzipped`,
    };

    // Printed so a CI log carries the numbers the report quotes.
    // eslint-disable-next-line no-console
    console.log('[zero-cost]', JSON.stringify(report, null, 2));

    expect(report.devtoolsCost).toBeTruthy();
  });
});
