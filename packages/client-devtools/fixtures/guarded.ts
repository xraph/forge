import { client, run } from './production';

/**
 * The pattern an application is meant to use, and the one the zero-cost claim
 * is really about.
 *
 * A dynamic import behind a bare `process.env.NODE_ENV` comparison. Every
 * bundler substitutes that expression textually, folds the comparison, and
 * drops the dead branch -- and with the branch goes the `import()` call, so no
 * chunk is emitted at all. The claim is not "the inspector is small in
 * production"; it is that a production build does not contain it, does not
 * request it, and does not know it exists.
 *
 * **Bare on purpose.** Written defensively as
 * `typeof process === 'undefined' || process?.env?.NODE_ENV !== 'production'`,
 * the second half still folds but the first does not -- `typeof process` is not
 * statically knowable for a browser target -- so the disjunction survives, the
 * branch is kept, and the whole package ships. Measured, not assumed: the test
 * beside this file fails on that spelling. `import.meta.env.DEV` under Vite has
 * the same effect as the bare form.
 */
declare const process: { env: { NODE_ENV: string } };

export function boot(): void {
  run();

  if (process.env.NODE_ENV !== 'production') {
    void import('../dist/index.js').then((devtools) => {
      (globalThis as Record<string, unknown>)['forge'] = devtools.attach(client);
    });
  }
}
