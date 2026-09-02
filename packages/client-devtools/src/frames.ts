import type { FrameCapture } from './types.js';

/** How deep a bounded copy is walked before it is truncated. */
const DEPTH = 6;
/** How many array elements, and how many object keys, a frame capture keeps. */
const WIDTH = 50;

/** The key a truncated object's marker is filed under. See `bounded`. */
const MORE = '[more]';

/**
 * A bounded copy of an arbitrary value: capped in depth, and capped in width
 * on an array's length *and* an object's key count.
 *
 * The object half is the one that is easy to forget, and it is not a corner
 * case: a payload of one flat object with ten thousand fields costs exactly
 * what a payload of ten thousand rows costs, and a walker that caps only
 * arrays copies the first whole while congratulating itself about the second.
 * Both branches now stop at `width` and leave the same `[N more]` marker,
 * filed under `[more]` for the object one, because an object has nowhere to
 * push it.
 *
 * Copied rather than referenced, so that holding the result cannot keep a
 * store record alive after the store has let it go, and so that a panel
 * writing into what it was handed cannot move anything real.
 *
 * The one shared walker behind every bounded copy this package makes: frame
 * payloads and an entity's fields through `capture` below, and a query's last
 * settled response through `capped` in `inspect.ts`. They differ only in how
 * wide they are allowed to be, which is a parameter rather than a reason to
 * keep two of these.
 */
export function bounded(value: unknown, width: number, depth = 0): unknown {
  if (depth > DEPTH) return '[deeper]';
  if (value === null || typeof value !== 'object') return value;

  if (Array.isArray(value)) {
    const out: unknown[] = value
      .slice(0, width)
      .map((element) => bounded(element, width, depth + 1));

    if (value.length > width) out.push(`[${String(value.length - width)} more]`);

    return out;
  }

  const source = value as Record<string, unknown>;
  const keys = Object.keys(source);
  const out: Record<string, unknown> = {};

  for (const key of keys.slice(0, width)) out[key] = bounded(source[key], width, depth + 1);

  if (keys.length > width) out[MORE] = `[${String(keys.length - width)} more]`;

  return out;
}

/**
 * A copy of a frame payload, bounded in both directions.
 *
 * Bounded in depth so a deeply nested graph terminates, and in width so a
 * frame carrying a thousand rows costs the ring one entry rather than a
 * thousand.
 */
export function capture(value: unknown, depth = 0): unknown {
  return bounded(value, WIDTH, depth);
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
