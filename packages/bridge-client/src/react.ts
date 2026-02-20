/**
 * React hooks for the Forge Go↔JS bridge.
 */

import { useState, useCallback, useRef, useEffect } from "react";
import { ForgeBridge, type BridgeCallOptions } from "./index.js";

const defaultBridge = new ForgeBridge();

/**
 * React hook for calling Go bridge functions.
 *
 * @example
 * ```tsx
 * function StatsPanel() {
 *   const { data, loading, error, call } = useForgeBridge<Stats>();
 *
 *   useEffect(() => {
 *     call("auth.getStats");
 *   }, [call]);
 *
 *   if (loading) return <p>Loading...</p>;
 *   if (error) return <p>Error: {error.message}</p>;
 *   return <pre>{JSON.stringify(data, null, 2)}</pre>;
 * }
 * ```
 */
export function useForgeBridge<T = unknown>(bridgeInstance?: ForgeBridge) {
  const b = bridgeInstance ?? defaultBridge;
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  const call = useCallback(
    async (
      method: string,
      params?: Record<string, unknown>,
      options?: BridgeCallOptions
    ) => {
      // Cancel any in-flight request
      abortRef.current?.abort();
      const controller = new AbortController();
      abortRef.current = controller;

      setLoading(true);
      setError(null);

      try {
        const result = await b.call<T>(method, params, {
          ...options,
          signal: controller.signal,
        });
        setData(result);
        return result;
      } catch (err) {
        const e = err instanceof Error ? err : new Error(String(err));
        if (e.message !== "Bridge call aborted") {
          setError(e);
        }
        return null;
      } finally {
        setLoading(false);
      }
    },
    [b]
  );

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  return { data, loading, error, call, isAvailable: b.isAvailable() };
}

/**
 * React hook that calls a bridge function once on mount.
 *
 * @example
 * ```tsx
 * function Overview() {
 *   const { data, loading } = useBridgeQuery<Overview>("auth.getOverview");
 *   if (loading) return <Spinner />;
 *   return <div>{data?.activeUsers} active users</div>;
 * }
 * ```
 */
export function useBridgeQuery<T = unknown>(
  method: string,
  params?: Record<string, unknown>,
  options?: { enabled?: boolean; bridge?: ForgeBridge }
) {
  const { data, loading, error, call, isAvailable } = useForgeBridge<T>(
    options?.bridge
  );
  const enabled = options?.enabled ?? true;

  useEffect(() => {
    if (enabled && isAvailable) {
      call(method, params);
    }
  }, [method, enabled, isAvailable]); // eslint-disable-line react-hooks/exhaustive-deps

  return { data, loading, error, refetch: () => call(method, params) };
}
