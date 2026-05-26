import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery as useRQ } from "@tanstack/react-query";
import { ContractClient, type GraphResult } from "./client";
import { SubscriptionMux } from "./sse";
import type { StreamEvent } from "./types";

const sharedClient = new ContractClient();
const sharedMux = new SubscriptionMux();

export function useContractGraph(contributor: string, route: string) {
  return useRQ<GraphResult>({
    queryKey: ["graph", contributor, route],
    queryFn: () => sharedClient.graph(contributor, route),
  });
}

export function useContractQuery<T = unknown>(
  contributor: string,
  intent: string,
  payload?: unknown,
  params?: Record<string, unknown>,
) {
  // Guard against empty intent: react-query would still cache an entry
  // for ["query", contributor, "", ...] and our queryFn would POST an
  // empty-intent envelope, which the dashboard transport rejects as
  // "intent  not registered" (double-space — the intent name is the
  // empty string). The hook is sometimes called with a missing intent
  // by components that try-both-paths (see metric.counter); `enabled:
  // false` skips the fetch and returns the same shape so callers can
  // branch on .data without crashing.
  return useRQ<T>({
    queryKey: ["query", contributor, intent, payload, params],
    queryFn: () => sharedClient.query<T>(contributor, intent, payload, params),
    enabled: intent !== "",
  });
}

export function useContractCommand<TPayload = unknown, TResponse = unknown>(
  contributor: string,
  intent: string,
) {
  return useMutation<TResponse, Error, TPayload>({
    mutationFn: (payload) => sharedClient.command<TResponse>(contributor, intent, payload),
  });
}

export function useSubscription<T = unknown>(
  contributor: string,
  intent: string,
  params: Record<string, unknown> = {},
) {
  const [latest, setLatest] = useState<StreamEvent<T> | null>(null);
  const handlerRef = useRef((ev: StreamEvent<T>) => setLatest(ev));

  useEffect(() => {
    // Skip when no intent is bound. Components that branch between
    // query and subscription paths (metric.counter) pass intent="" on the
    // unused path; firing a subscribe with empty intent would trip the
    // upstream's "intent not a subscription" rejection (which then shows
    // up in the network panel as a parade of 400s).
    if (!intent) return;
    let unsub: (() => void) | null = null;
    let cancelled = false;
    void sharedMux
      .subscribe(contributor, intent, params, (ev) => handlerRef.current(ev as StreamEvent<T>))
      .then((u) => {
        if (cancelled) {
          u();
          return;
        }
        unsub = u;
      });
    return () => {
      cancelled = true;
      unsub?.();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [contributor, intent, JSON.stringify(params)]);

  return latest;
}
