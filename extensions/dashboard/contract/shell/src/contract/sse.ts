import type { StreamEvent } from "./types";

export interface SubscriptionMuxOptions {
  baseURL?: string;
  eventSource?: typeof EventSource;
  fetcher?: typeof fetch;
}

interface PendingSub {
  contributor: string;
  intent: string;
  params: Record<string, unknown>;
  subscriptionID: string;
  handler: (ev: StreamEvent) => void;
}

export class SubscriptionMux {
  private readonly baseURL: string;
  private readonly EventSourceCtor: typeof EventSource;
  private readonly fetcher: typeof fetch;
  private es: EventSource | null = null;
  private streamID: string | null = null;
  private pending: PendingSub[] = [];
  private active = new Map<string, PendingSub>();

  constructor(opts: SubscriptionMuxOptions = {}) {
    this.baseURL = opts.baseURL ?? "/api/dashboard/v1";
    this.EventSourceCtor = opts.eventSource ?? globalThis.EventSource;
    this.fetcher = opts.fetcher ?? globalThis.fetch;
  }

  async subscribe(
    contributor: string,
    intent: string,
    params: Record<string, unknown>,
    handler: (ev: StreamEvent) => void,
    opts: { subscriptionID?: string } = {},
  ): Promise<() => void> {
    const subscriptionID = opts.subscriptionID ?? crypto.randomUUID();
    const sub: PendingSub = { contributor, intent, params, subscriptionID, handler };

    if (!this.es) {
      this.openStream();
    }
    this.attachSubscriptionListener(sub);

    if (this.streamID) {
      await this.sendControl({
        op: "subscribe",
        subscriptionID,
        contributor,
        intent,
        params,
      });
      this.active.set(subscriptionID, sub);
    } else {
      this.pending.push(sub);
    }

    return () => this.unsubscribe(subscriptionID);
  }

  private openStream(): void {
    this.es = new this.EventSourceCtor(`${this.baseURL}/stream`);
    this.es.addEventListener("hello", (ev: MessageEvent) => {
      const { streamID } = JSON.parse(ev.data) as { streamID: string };
      this.streamID = streamID;
      const drain = this.pending.splice(0);
      void Promise.all(
        drain.map((sub) => {
          this.active.set(sub.subscriptionID, sub);
          return this.sendControl({
            op: "subscribe",
            subscriptionID: sub.subscriptionID,
            contributor: sub.contributor,
            intent: sub.intent,
            params: sub.params,
          });
        }),
      );
    });
  }

  private attachSubscriptionListener(sub: PendingSub): void {
    if (!this.es) return;
    this.es.addEventListener(sub.subscriptionID, (ev: MessageEvent) => {
      try {
        const parsed = JSON.parse(ev.data) as StreamEvent;
        sub.handler(parsed);
      } catch {
        // Drop malformed events.
      }
    });
  }

  private async unsubscribe(subscriptionID: string): Promise<void> {
    this.active.delete(subscriptionID);
    if (!this.streamID) return;
    await this.sendControl({ op: "unsubscribe", subscriptionID });
  }

  private async sendControl(msg: Record<string, unknown>): Promise<void> {
    if (!this.streamID) return;
    await this.fetcher(`${this.baseURL}/stream/control`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ streamID: this.streamID, ...msg }),
      credentials: "include",
    });
  }

  close(): void {
    this.es?.close();
    this.es = null;
    this.streamID = null;
    this.active.clear();
    this.pending = [];
  }
}
