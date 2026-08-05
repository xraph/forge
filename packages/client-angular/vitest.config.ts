import { defineConfig } from 'vitest/config';

export default defineConfig({
  /**
   * Angular's `@Component` is a *legacy* TypeScript decorator, and its JIT
   * compiler reads the metadata the legacy emit leaves behind. Vitest's
   * transform is oxc, which defaults to the TC39 proposal semantics and would
   * otherwise turn every component in `__tests__` into a syntax error.
   *
   * Stated here rather than left to tsconfig discovery: `tsconfig.json` scopes
   * itself to `src` so `tsc` emits the library alone, and a transform that
   * silently depends on which tsconfig a bundler decides is nearest is a
   * transform that breaks when somebody adds one.
   *
   * `emitDecoratorMetadata` stays off. It exists for constructor-parameter
   * injection, and nothing in this package or its tests uses it -- `inject()`
   * is the whole DI story here.
   */
  oxc: { decorator: { legacy: true } },
  test: {
    environment: 'jsdom',
    setupFiles: ['./__tests__/setup.ts'],
  },
});
