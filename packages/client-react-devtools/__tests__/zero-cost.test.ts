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
 * The binding constraint for this package, checked against the **built
 * output**, in the shape `client-devtools/__tests__/zero-cost.test.ts` uses.
 *
 * One fixture, `react-guarded.tsx`, bundled twice from the same source:
 *
 * - With no conditions, `@forge-go/client-react-devtools` resolves through
 *   its `exports` map's `default` key to `dist/noop.js`. That is what a
 *   production build sees, and the bundle it produces must contain none of
 *   the devtools and no request for them.
 * - With the `development` condition, it resolves to `dist/dev.js` instead,
 *   and the markers must all be present -- the presence half is what makes
 *   the absence half mean something, rather than passing vacuously because
 *   the fixture never reached the package at all.
 *
 * This package cannot import fixtures out of `client-devtools/fixtures`
 * (that would be the circular `file:` dependency the controller ruling
 * rejected), so `fixtures/production.ts` here is its own small copy of the
 * same control, and `MARKERS`/`PANEL_MARKERS` below are copied from
 * `client-devtools/__tests__/zero-cost.test.ts` rather than imported --
 * belt-and-suspenders against a bundler that resolves `development` even
 * when the condition was never asked for, which would otherwise only be
 * caught by the plain `'client-devtools'` substring check.
 */

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, '..');

/** Copied from `client-devtools/__tests__/zero-cost.test.ts`. See the file comment above. */
const MARKERS = [
  'instance-vs-collection',
  'stale-while-unmounted',
  "` to the operation's Invalidates",
  'differ only in case',
  'never heard of',
  'the most common cause of an invalidation',
];

/** Copied from `client-devtools/__tests__/zero-cost.test.ts`. See the file comment above. */
const PANEL_MARKERS = ['no stream runtime is attached to this cache', 'frame capture is off'];

/**
 * A string that exists only in this package's dev build.
 *
 * `dev.ts` itself raises no such message; this is `[forge] nothing is
 * tracking ${key}`, `client-devtools/src/actions.ts`'s error for an
 * untracked key. `dev.ts` dynamically imports `@forge-go/client-devtools`,
 * and esbuild inlines a reachable dynamic import into the same output file
 * rather than deferring it to a separate chunk (verified directly against
 * this fixture, and against a minimal reproduction, before relying on it) --
 * so this string reaching the bundle is exactly what proves the whole
 * inspector, not just `dev.ts`'s own few lines, is live in a development
 * build.
 */
const REACT_MARKERS = ['nothing is tracking'];

interface Bundle {
  readonly code: string;
  readonly bytes: number;
  readonly gzipped: number;
}

async function bundle(entry: string, conditions: string[] = []): Promise<Bundle> {
  const result = await build({
    entryPoints: [resolve(root, 'fixtures', entry)],
    bundle: true,
    minify: true,
    format: 'esm',
    platform: 'browser',
    target: 'es2020',
    conditions,
    // What a production build does, and the whole mechanism the `default`
    // export condition relies on -- except when bundling under the
    // `development` condition, which is what proves the presence half of
    // the claim.
    define: {
      'process.env.NODE_ENV': conditions.includes('development')
        ? '"development"'
        : '"production"',
    },
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

let reactProduction: Bundle;
let reactDevelopment: Bundle;

beforeAll(async () => {
  // Against the built output, not the source. `dist/` is what the fixture
  // imports and what a consumer would install, and it needs both `dev.js`
  // and `noop.js` present for the two builds below to differ at all.
  execFileSync('npx', ['tsc'], { cwd: root, stdio: 'pipe' });

  expect(existsSync(resolve(root, 'dist/dev.js'))).toBe(true);
  expect(existsSync(resolve(root, 'dist/noop.js'))).toBe(true);

  reactProduction = await bundle('react-guarded.tsx');
  reactDevelopment = await bundle('react-guarded.tsx', ['development']);
}, 120_000);

describe('the React entry point', () => {
  it('resolves to the noop under a production build, carrying no devtools', () => {
    for (const marker of [...MARKERS, ...PANEL_MARKERS, ...REACT_MARKERS]) {
      expect(reactProduction.code).not.toContain(marker);
    }

    expect(reactProduction.code).not.toContain('client-devtools');
  });

  it('carries it under a development build, which is what makes the absence mean something', () => {
    for (const marker of REACT_MARKERS) {
      expect(reactDevelopment.code).toContain(marker);
    }
  });
});

describe('sizes', () => {
  it('reports what each bundle costs', () => {
    const report = {
      reactProduction: `${String(reactProduction.bytes)} B raw / ${String(
        reactProduction.gzipped,
      )} B gzipped`,
      reactDevelopment: `${String(reactDevelopment.bytes)} B raw / ${String(
        reactDevelopment.gzipped,
      )} B gzipped`,
    };

    // Printed so a CI log carries the numbers the report quotes.
    // eslint-disable-next-line no-console
    console.log('[zero-cost]', JSON.stringify(report, null, 2));

    expect(report.reactProduction).toBeTruthy();
  });
});
