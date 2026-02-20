/**
 * @forge-go/bridge-client
 *
 * TypeScript client for the Forge Go↔JS bridge. This package provides
 * a typed interface for calling Go functions from JavaScript in dashboard
 * contributor UIs.
 *
 * The bridge works by injecting a global `$go` function into the page context
 * when the dashboard shell renders a contributor page.
 */

/** Result of a bridge function call. */
export interface BridgeResult<T = unknown> {
  data: T;
  error?: string;
}

/** Options for bridge function calls. */
export interface BridgeCallOptions {
  /** Timeout in milliseconds (default: 30000). */
  timeout?: number;
  /** AbortSignal for cancellation. */
  signal?: AbortSignal;
}

/** The global $go function injected by the dashboard shell. */
declare global {
  interface Window {
    $go?: (method: string, params?: Record<string, unknown>) => Promise<BridgeResult>;
  }
}

/**
 * ForgeBridge provides a typed interface for calling Go bridge functions
 * from contributor UI code.
 */
export class ForgeBridge {
  private defaultTimeout: number;

  constructor(options?: { timeout?: number }) {
    this.defaultTimeout = options?.timeout ?? 30000;
  }

  /**
   * Check if the bridge is available (running inside the dashboard shell).
   */
  isAvailable(): boolean {
    return typeof window !== "undefined" && typeof window.$go === "function";
  }

  /**
   * Call a Go bridge function.
   *
   * @param method - The bridge function name (e.g., "auth.getStats")
   * @param params - Optional parameters to pass to the function
   * @param options - Call options (timeout, abort signal)
   * @returns The result from the Go function
   * @throws Error if bridge is not available or call fails
   */
  async call<T = unknown>(
    method: string,
    params?: Record<string, unknown>,
    options?: BridgeCallOptions
  ): Promise<T> {
    if (!this.isAvailable()) {
      throw new Error(
        "Forge bridge is not available. Are you running inside the dashboard?"
      );
    }

    const timeout = options?.timeout ?? this.defaultTimeout;

    const result = await Promise.race([
      window.$go!(method, params),
      this.createTimeout(timeout, options?.signal),
    ]);

    if (result.error) {
      throw new Error(`Bridge call failed: ${result.error}`);
    }

    return result.data as T;
  }

  private createTimeout(
    ms: number,
    signal?: AbortSignal
  ): Promise<BridgeResult> {
    return new Promise((_, reject) => {
      const timer = setTimeout(
        () => reject(new Error(`Bridge call timed out after ${ms}ms`)),
        ms
      );

      signal?.addEventListener("abort", () => {
        clearTimeout(timer);
        reject(new Error("Bridge call aborted"));
      });
    });
  }
}

/** Default bridge instance for convenience. */
export const bridge = new ForgeBridge();
