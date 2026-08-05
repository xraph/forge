import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  // Relative, so the built bundle is relocatable.
  //
  // An absolute base ("/dashboard/ui/static/") gets baked into two places: the
  // asset URLs in index.html, and Vite's preload resolver for lazily imported
  // chunks. Both are then correct only when the dashboard is mounted at the
  // default /dashboard -- a deployment using WithBasePath("/_forge/dashboard")
  // served its HTML from the new path while the browser still asked for
  // /dashboard/ui/static/assets/*, and got 404s for every script and stylesheet.
  //
  // With a relative base, the preload resolver becomes importer-relative
  // (new URL(dep, importerUrl)), so code-split chunks resolve correctly from
  // whatever path the shell is actually served at. index.html's own "./assets/"
  // references are document-relative, which would break on deep links, so the
  // Go SPA handler rewrites those to an absolute URL under the configured base
  // -- see makeShellSPAHandler in extensions/dashboard/extension.go.
  base: "./",
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: true,
    target: "es2022",
    rollupOptions: {
      output: {
        manualChunks: {
          "react-vendor": ["react", "react-dom", "react-router-dom"],
          "query-vendor": ["@tanstack/react-query"],
          "recharts-vendor": ["recharts"],
          "dnd-vendor": ["@dnd-kit/core"],
          // @monaco-editor/react is intentionally NOT listed — it sits
          // behind React.lazy() in organism.code-editor so Vite auto-emits
          // a separate chunk that's fetched on first editor mount.
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api/dashboard": {
        target: "http://localhost:8080",
        changeOrigin: false,
      },
      "/dashboard/ui/static": {
        target: "http://localhost:5173",
        bypass: () => "/index.html",
      },
    },
  },
});
