/**
 * @forge-go/astro-plugin
 *
 * Astro integration for Forge dashboard contributors.
 * Configures Astro for fragment-friendly output compatible with
 * the Forge dashboard shell.
 */

import type { AstroIntegration } from "astro";

export interface ForgeAstroOptions {
  /**
   * The contributor name (must match forge.contributor.yaml).
   */
  name: string;

  /**
   * Build mode: "static" for embedded or "ssr" for server-side rendering.
   * Default: "static"
   */
  mode?: "static" | "ssr";

  /**
   * Base path for asset URLs. The dashboard shell mounts assets at
   * /dashboard/ext/{name}/assets/. Set automatically based on the name.
   */
  assetBase?: string;
}

/**
 * Astro integration for Forge dashboard contributors.
 *
 * @example
 * ```js
 * // astro.config.mjs
 * import { defineConfig } from 'astro/config';
 * import forge from '@forge-go/astro-plugin';
 *
 * export default defineConfig({
 *   integrations: [forge({ name: 'auth' })],
 * });
 * ```
 */
export default function forgeAstroPlugin(
  options: ForgeAstroOptions
): AstroIntegration {
  const mode = options.mode ?? "static";
  const assetBase =
    options.assetBase ?? `/dashboard/ext/${options.name}/assets`;

  return {
    name: "@forge-go/astro-plugin",
    hooks: {
      "astro:config:setup": ({ updateConfig }) => {
        updateConfig({
          output: mode === "static" ? "static" : "server",
          build: {
            format: "directory",
          },
          vite: {
            build: {
              assetsDir: "assets",
              rollupOptions: {
                output: {
                  assetFileNames: `assets/[name].[hash][extname]`,
                  chunkFileNames: `assets/[name].[hash].js`,
                  entryFileNames: `assets/[name].[hash].js`,
                },
              },
            },
          },
          // Base path for production asset references
          ...(mode === "static" ? { base: assetBase } : {}),
        });
      },

      "astro:build:done": ({ dir }) => {
        console.log(
          `[forge] Build complete for contributor "${options.name}" (${mode} mode)`
        );
        console.log(`[forge] Output: ${dir}`);
      },
    },
  };
}

// Re-export bridge client for convenience
export { ForgeBridge, bridge } from "@forge-go/bridge-client";
