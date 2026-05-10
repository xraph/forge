import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  base: "/dashboard/contract/static/",
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
      "/dashboard/contract/static": {
        target: "http://localhost:5173",
        bypass: () => "/index.html",
      },
    },
  },
});
