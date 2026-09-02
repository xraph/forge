import type { FrameCapture } from './types.js';

/** How deep a captured payload is walked before it is truncated. */
const DEPTH = 6;
/** How many array elements are kept at each level. */
const WIDTH = 50;

/**
 * A copy of a frame payload, bounded in both directions.
 *
 * Bounded in depth so a deeply nested graph terminates, and in width so a
 * frame carrying a thousand rows costs the ring one entry rather than a
 * thousand. Copied rather than referenced so that holding it cannot keep a
 * store record alive after the store has let it go.
 */
export function capture(value: unknown, depth = 0): unknown {
  if (depth > DEPTH) return '[deeper]';
  if (value === null || typeof value !== 'object') return value;

  if (Array.isArray(value)) {
    const out: unknown[] = value.slice(0, WIDTH).map((element) => capture(element, depth + 1));

    if (value.length > WIDTH) out.push(`[${String(value.length - WIDTH)} more]`);

    return out;
  }

  const out: Record<string, unknown> = {};

  for (const [key, member] of Object.entries(value as Record<string, unknown>)) {
    out[key] = capture(member, depth + 1);
  }

  return out;
}

/**
 * A fixed ring of captured frames, allocated once.
 *
 * Deliberately not `EventLog`. That ring holds entries bounded by construction
 * and can be kept for the life of a session. This one holds payloads, so it is
 * opt-in, smaller by default, and separate, so that turning it on cannot
 * change what the causal log records.
 */
export class FrameRing {
  readonly capacity: number;

  private readonly ring: (FrameCapture | undefined)[];
  private cursor = 0;
  private filled = 0;

  constructor(capacity: number) {
    this.capacity = Math.max(1, Math.floor(capacity));
    this.ring = new Array<FrameCapture | undefined>(this.capacity);
  }

  push(entry: FrameCapture): void {
    if (this.filled < this.capacity) this.filled++;

    this.ring[this.cursor] = entry;
    this.cursor = (this.cursor + 1) % this.capacity;
  }

  /** Everything held, oldest first. */
  entries(): FrameCapture[] {
    const out: FrameCapture[] = [];
    const start = this.filled === this.capacity ? this.cursor : 0;

    for (let i = 0; i < this.filled; i++) {
      const entry = this.ring[(start + i) % this.capacity];

      if (entry !== undefined) out.push(entry);
    }

    return out;
  }

  clear(): void {
    this.ring.fill(undefined);
    this.cursor = 0;
    this.filled = 0;
  }
}
