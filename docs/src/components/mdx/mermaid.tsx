"use client";

import { use, useEffect, useId, useState } from "react";
import { useTheme } from "next-themes";

export function Mermaid({ chart }: { chart: string }) {
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
  }, []);

  if (!mounted) return;
  return <MermaidContent chart={chart} />;
}

const cache = new Map<string, Promise<unknown>>();

function cachePromise<T>(
  key: string,
  setPromise: () => Promise<T>,
): Promise<T> {
  const cached = cache.get(key);
  if (cached) return cached as Promise<T>;

  const promise = setPromise();
  cache.set(key, promise);
  return promise;
}

function MermaidContent({ chart }: { chart: string }) {
  const id = useId();
  const { resolvedTheme } = useTheme();
  const { default: mermaid } = use(
    cachePromise("mermaid", () => import("mermaid")),
  );

  mermaid.initialize({
    startOnLoad: false,
    securityLevel: "loose",
    fontFamily: "inherit",
    themeCSS: "margin: 1.5rem auto 0;",
    theme: resolvedTheme === "dark" ? "dark" : "default",
  });

  const result = use(
    cachePromise(`${chart}-${resolvedTheme}`, async () => {
      try {
        return await mermaid.render(id, chart.replaceAll("\\n", "\n"));
      } catch (e) {
        console.error("Mermaid render error:", e);
        cache.delete(`${chart}-${resolvedTheme}`);
        return null;
      }
    }),
  );

  if (!result) {
    return (
      <pre className="rounded-lg border bg-fd-muted p-4 text-sm text-fd-muted-foreground overflow-x-auto">
        {chart.replaceAll("\\n", "\n")}
      </pre>
    );
  }

  return (
    <div
      ref={(container) => {
        if (container) result.bindFunctions?.(container);
      }}
      dangerouslySetInnerHTML={{ __html: result.svg }}
    />
  );
}
