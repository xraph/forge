/**
 * @forge-go/nextjs-plugin
 *
 * Next.js integration for Forge dashboard contributors.
 * Provides configuration helpers, layout components, and bridge hooks.
 */

// Re-export config helper
export { withForge, type ForgeNextOptions } from "./config.js";

// Re-export bridge client for convenience
export { ForgeBridge, bridge } from "@forge-go/bridge-client";
export { useForgeBridge, useBridgeQuery } from "@forge-go/bridge-client/react";
