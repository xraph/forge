import type { LogDraft, LogEntry } from './types';

/**
 * A fixed-size ring of events.
 *
 * **An event log is a memory leak by default**, and this one is not, by
 * construction rather than by policy: the backing array is allocated once at
 * the declared capacity and never grows. When it is full the oldest entry is
 * overwritten -- the log is a rolling window on the recent past, not a
 * recording -- and `dropped` counts how many have gone, so a timeline that
 * begins mid-story says so instead of pretending it begins at the beginning.
 *
 * What is *in* an entry is bounded too, and that is the half that is easy to
 * get wrong. Nothing here retains a response body, an error object or a
 * rehydrated value: tags are resolved and copied at record time, arguments are
 * reduced to a truncated cache key, and an error is reduced to its message.
 * A ring of five hundred entries that each hold a page of orders is the same
 * leak arriving more slowly, and it would additionally keep entities alive that
 * the store's own garbage collection had already let go.
 */
export class EventLog {
  /** How many entries the ring holds before it starts overwriting. */
  readonly capacity: number;

  private readonly ring: (LogEntry | undefined)[];
  private readonly now: () => number;
  private readonly listeners = new Set<(entry: LogEntry) => void>();

  /** Where the next entry goes. */
  private cursor = 0;
  /** How many slots are filled. Stops rising at `capacity`. */
  private filled = 0;
  private overwritten = 0;
  private next = 1;

  constructor(capacity = 500, now: () => number = Date.now) {
    // A capacity below one would make `push` write outside the ring; a
    // fractional one would make the modulo produce a fractional index and every
    // slot a fresh property on a sparse array.
    this.capacity = Math.max(1, Math.floor(capacity));
    this.ring = new Array<LogEntry | undefined>(this.capacity);
    this.now = now;
  }

  /** How many entries have been overwritten and are gone for good. */
  get dropped(): number {
    return this.overwritten;
  }

  /** How many entries the ring is currently holding. */
  get size(): number {
    return this.filled;
  }

  /** The `seq` the next entry will take. */
  get sequence(): number {
    return this.next;
  }

  /**
   * Record one event, stamping it with the sequence, the clock and the session.
   *
   * The listeners run inside a `try`: an overlay that throws while rendering
   * must not take down the application it is inspecting, which would be a
   * spectacular way for a debugging tool to introduce a bug.
   */
  push(event: LogDraft): LogEntry {
    const entry = { ...event, seq: this.next++, at: this.now() } as LogEntry;

    if (this.filled === this.capacity) this.overwritten++;
    else this.filled++;

    this.ring[this.cursor] = entry;
    this.cursor = (this.cursor + 1) % this.capacity;

    for (const listener of [...this.listeners]) {
      try {
        listener(entry);
      } catch {
        // Swallowed on purpose. See above.
      }
    }

    return entry;
  }

  /** Every entry the ring holds, oldest first. A copy. */
  entries(): LogEntry[] {
    const out: LogEntry[] = [];
    const start = this.filled === this.capacity ? this.cursor : 0;

    for (let i = 0; i < this.filled; i++) {
      const entry = this.ring[(start + i) % this.capacity];

      if (entry !== undefined) out.push(entry);
    }

    return out;
  }

  /** The entry with this sequence number, if the ring still holds it. */
  find(seq: number): LogEntry | undefined {
    for (let i = 0; i < this.filled; i++) {
      const entry = this.ring[i];

      if (entry !== undefined && entry.seq === seq) return entry;
    }

    return undefined;
  }

  /**
   * The most recent entry satisfying `match`, searching backwards.
   *
   * Backwards because every question this log is asked -- why did this refetch,
   * what caused that -- is about the recent past, and a forward scan of a full
   * ring to find the last match is five hundred comparisons for an answer that
   * was usually one.
   */
  last(match: (entry: LogEntry) => boolean): LogEntry | undefined {
    const start = this.filled === this.capacity ? this.cursor : 0;

    for (let i = this.filled - 1; i >= 0; i--) {
      const entry = this.ring[(start + i) % this.capacity];

      if (entry !== undefined && match(entry)) return entry;
    }

    return undefined;
  }

  /** Forget everything. The sequence keeps counting; `dropped` resets. */
  clear(): void {
    this.ring.fill(undefined);
    this.cursor = 0;
    this.filled = 0;
    this.overwritten = 0;
  }

  /** Be told about each entry as it is recorded. Returns the unsubscribe. */
  subscribe(listener: (entry: LogEntry) => void): () => void {
    this.listeners.add(listener);

    return () => {
      this.listeners.delete(listener);
    };
  }
}
