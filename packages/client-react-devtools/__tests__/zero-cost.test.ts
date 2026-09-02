// @vitest-environment node
//
// esbuild refuses to run under jsdom, whose `TextEncoder` does not produce a
// real `Uint8Array`. This file bundles things; nothing in it touches a DOM.
import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
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
 *
 * The marker checks below are not enough on their own, and this file does not
 * rely on them alone. Two independent mechanisms are meant to keep the
 * inspector out of a production build: the `exports` map routing to
 * `dist/noop.js`, and `dev.ts`'s own bare `process.env.NODE_ENV !==
 * 'production'` guard around the body that would attach it. Built directly
 * against `dist/dev.js` under `NODE_ENV=production` conditions (bypassing the
 * exports map the way a broken `default` key would), that internal guard
 * dead-code-eliminates the same block the exports map was supposed to keep
 * unreached -- so a marker-only check cannot tell the two mechanisms apart,
 * and passes whether the exports map is correct or pointing at the wrong
 * file entirely. Confirmed directly: building `react-guarded.tsx` with an
 * `alias` forcing `@forge-go/client-react-devtools` to `dist/dev.js` under
 * production conditions still produced a bundle with none of the markers,
 * because `dev.ts`'s own guard removed the marker-carrying code anyway. The
 * `exports` map assertion and the size control below are what actually catch
 * that: the map assertion names the file directly, and the size control is
 * mechanism-independent, since `dist/dev.js` still carries `attached`,
 * `listeners`, `subscribe`, `notify`, `acquire`, `release` and
 * `useForgeDevtools` outside that guard, and that alone was enough to blow
 * past the budget below.
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

let production: Bundle;
let reactProduction: Bundle;
let reactDevelopment: Bundle;

beforeAll(async () => {
  // Against the built output, not the source. `dist/` is what the fixture
  // imports and what a consumer would install, and it needs both `dev.js`
  // and `noop.js` present for the two builds below to differ at all.
  execFileSync('npx', ['tsc'], { cwd: root, stdio: 'pipe' });

  expect(existsSync(resolve(root, 'dist/dev.js'))).toBe(true);
  expect(existsSync(resolve(root, 'dist/noop.js'))).toBe(true);

  // The control: the same application, minus the `ForgeDevtools` element and
  // its import. The size delta below is taken against this, not against zero
  // -- `reactProduction` also bundles React itself, which `production.ts`
  // alone does not, and that overhead is not the devtools' to answer for.
  production = await bundle('production.ts');
  reactProduction = await bundle('react-guarded.tsx');
  reactDevelopment = await bundle('react-guarded.tsx', ['development']);
}, 120_000);

describe("the package's exports map", () => {
  it('routes "." to the noop by default and to dev.js only under development', () => {
    // Direct and mechanism-independent: names the exact file, in the exact
    // place a future edit to `package.json` would break it. Read fresh from
    // disk rather than hardcoded twice, so this fails the moment the map on
    // disk stops matching what the test expects, not just when this file's
    // own copy of the values drifts.
    const packageJson = JSON.parse(readFileSync(resolve(root, 'package.json'), 'utf8')) as {
      exports: { '.': { default: string; development: string } };
    };

    expect(packageJson.exports['.'].default).toBe('./dist/noop.js');
    expect(packageJson.exports['.'].development).toBe('./dist/dev.js');
  });
});

describe('the React entry point', () => {
  it('resolves to the noop under a production build, carrying no devtools', () => {
    // Markers first, since a marker match is the more specific failure to
    // read. Not sufficient alone -- see the file comment above -- which is
    // why the size control and the exports-map check exist beside it.
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

  it('costs the production bundle only the weight of React itself, not the devtools', () => {
    // The mechanism-independent check. `reactProduction` necessarily costs
    // more than the bare `production` control, since it also bundles React
    // and the `ForgeDevtools` noop element -- measured at 3125 B gzipped
    // over the control on the code as shipped. Built instead against
    // `dist/dev.js` under production conditions (see the file comment), that
    // delta measured 4274 B: `dev.ts`'s own guard eliminates the attach body,
    // but `attached`, `listeners`, `subscribe`, `notify`, `acquire`,
    // `release` and `useForgeDevtools` are not inside it and stay in the
    // bundle regardless. The budget sits between the two, with headroom
    // below the true value for normal drift in React's own bundled size, and
    // well under what a wrong file would cost.
    expect(reactProduction.gzipped - production.gzipped).toBeLessThan(4_000);
  });
});

describe('sizes', () => {
  it('reports what each bundle costs', () => {
    const report = {
      production: `${String(production.bytes)} B raw / ${String(production.gzipped)} B gzipped`,
      reactProduction: `${String(reactProduction.bytes)} B raw / ${String(
        reactProduction.gzipped,
      )} B gzipped`,
      reactDevelopment: `${String(reactDevelopment.bytes)} B raw / ${String(
        reactDevelopment.gzipped,
      )} B gzipped`,
      reactCost: `${String(reactProduction.gzipped - production.gzipped)} B gzipped`,
    };

    // Printed so a CI log carries the numbers the report quotes.
    // eslint-disable-next-line no-console
    console.log('[zero-cost]', JSON.stringify(report, null, 2));

    expect(report.reactProduction).toBeTruthy();
  });
});
