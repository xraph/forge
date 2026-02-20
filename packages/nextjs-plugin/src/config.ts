/**
 * @forge-go/nextjs-plugin/config
 *
 * Next.js configuration wrapper for Forge dashboard contributors.
 * Configures Next.js for fragment-friendly output.
 */

import type { NextConfig } from "next";

export interface ForgeNextOptions {
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
   * /dashboard/ext/{name}/assets/.
   */
  assetBase?: string;
}

/**
 * Wrap a Next.js config with Forge contributor settings.
 *
 * @example
 * ```js
 * // next.config.mjs
 * import { withForge } from '@forge-go/nextjs-plugin/config';
 *
 * export default withForge({
 *   name: 'analytics',
 *   mode: 'static',
 * });
 * ```
 */
export function withForge(
  options: ForgeNextOptions,
  nextConfig: NextConfig = {}
): NextConfig {
  const mode = options.mode ?? "static";
  const assetBase =
    options.assetBase ?? `/dashboard/ext/${options.name}/assets`;

  const forgeConfig: NextConfig = {
    ...nextConfig,
    // Disable image optimization (not available in embedded mode)
    images: {
      ...nextConfig.images,
      unoptimized: true,
    },
  };

  if (mode === "static") {
    forgeConfig.output = "export";
    forgeConfig.distDir = "out";
    forgeConfig.basePath = assetBase;
    forgeConfig.assetPrefix = assetBase;
  } else {
    forgeConfig.output = "standalone";
  }

  return forgeConfig;
}
