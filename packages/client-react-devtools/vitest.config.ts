import { defineConfig } from 'vitest/config';

export default defineConfig({
  // This repo has no npm workspaces: `@forge-go/client-react` is a
  // `file:../client-react` symlink to a directory with its own, separately
  // installed `react`/`react-dom`. Without this, a bare `import 'react'`
  // inside this package's own test resolves to *our* copy while the same
  // bare import inside client-react's shipped `dist/context.js` resolves to
  // *its* copy -- two physically distinct module instances, which breaks
  // hooks and element identity even when both report the same version. Vite
  // resolves `file:` symlinks to their realpath and then applies
  // `resolve.dedupe` against that, so this forces every `react`/`react-dom`
  // import reached through the module graph -- ours or client-react's -- to
  // the one copy under this package's own node_modules.
  resolve: {
    dedupe: ['react', 'react-dom'],
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./__tests__/setup.ts'],
  },
});
