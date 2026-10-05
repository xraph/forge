import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

import { QueryCache } from '../src/cache';
import { manualScheduler } from '../src/invalidate';
import { applyFrames } from '../src/live';
import type { StreamFrame } from '../src/live';
import { normalize } from '../src/normalize';
import { dehydrate } from '../src/ssr';
import type { EntityStreamBinding } from '../src/stream';
import type { TagContext } from '../src/tags';
import type { OperationMeta } from '../src/transport';
import { encode } from '../src/wire';
import { fakeTransport } from './harness';
import { schema } from './schema';

/**
 * The fixtures the Dart runtime reads. With FORGE_WRITE_FIXTURES=1 this
 * suite writes them; otherwise it regenerates them in memory and fails on
 * any byte that differs from the file.
 */
const root = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'client-fixtures');
const writing = process.env['FORGE_WRITE_FIXTURES'] === '1';

/**
 * Rebuild every plain object with its keys sorted, so the file does not depend
 * on the order the runtime happened to insert them in. Arrays keep their order,
 * which is meaningful.
 */
function sortKeys(_key: string, value: unknown): unknown {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return value;

  const source = value as Record<string, unknown>;
  const sorted: Record<string, unknown> = {};

  for (const key of Object.keys(source).sort()) sorted[key] = source[key];

  return sorted;
}

function settle(name: string, value: unknown): void {
  const path = join(root, name);
  const text = `${JSON.stringify(value, sortKeys, 2)}\n`;

  if (writing) {
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, text);

    return;
  }

  expect(existsSync(path), `${name} is missing; run with FORGE_WRITE_FIXTURES=1`).toBe(true);
  expect(readFileSync(path, 'utf8')).toBe(text);
}

interface QuerySpec {
  readonly operation: OperationMeta;
  readonly args?: TagContext;
  readonly response: unknown;
}

const orderList: OperationMeta = {
  method: 'GET',
  path: '/orders',
  entity: 'Order',
  provides: ['Order[]'],
  invalidates: [],
};

const orderGet: OperationMeta = {
  method: 'GET',
  path: '/orders/{id}',
  entity: 'Order',
  provides: ['Order:{path.id}'],
  invalidates: [],
};

const orderSpecs: readonly QuerySpec[] = [
  {
    operation: orderList,
    response: [
      { id: 7, total: 99, customer: { id: 'c-3', name: 'Ada' } },
      { id: 8, total: 1, items: [{ sku: 'A-1', qty: 2 }] },
    ],
  },
  {
    operation: orderGet,
    args: { path: { id: 7 } },
    response: { id: 7, total: 99, invoice: { invoiceNumber: 'INV-1', amount: 99 } },
  },
];

async function snapshotFixture(
  description: string,
  principal: string | number | undefined,
  specs: readonly QuerySpec[],
  mode: 'normalized' | 'denormalized',
): Promise<unknown> {
  const client = new QueryCache({
    transport: fakeTransport((_request, call) => specs[call]?.response),
    entities: schema,
    scheduler: manualScheduler().schedule,
    now: () => 1000,
  });

  client.setPrincipal(principal);

  for (const spec of specs) await client.fetch(spec.operation, spec.args);

  return {
    description,
    principal: principal ?? null,
    schema,
    queries: specs.map((spec) => ({
      operation: spec.operation,
      args: spec.args ?? null,
      response: spec.response,
    })),
    state: dehydrate(client, { principal, mode }),
    reads: specs.map((spec) => ({
      operation: `${spec.operation.method} ${spec.operation.path}`,
      args: spec.args ?? null,
      value: client.getState(spec.operation, spec.args).data,
    })),
  };
}

function codecFixture(description: string, rootType: string, client: unknown): unknown {
  const { skeleton, records } = normalize(client, schema, rootType);
  const wire: Record<string, unknown> = {};

  for (const [key, data] of records) {
    wire[key] = encode(data, { query: 'fixture', entity: key }).value;
  }

  return {
    kind: 'ref-encoding',
    description,
    schema,
    rootType,
    client,
    wire: { skeleton: encode(skeleton, { query: 'fixture' }).value, records: wire },
  };
}

const created: EntityStreamBinding = {
  channel: '/ws/orders',
  message: 'order.created',
  entity: 'Order',
  intent: 'upsert',
  invalidates: ['Order[]'],
};

const updated: EntityStreamBinding = {
  channel: '/ws/orders',
  message: 'order.updated',
  entity: 'Order',
  intent: 'patch',
  invalidates: [],
};

const deleted: EntityStreamBinding = {
  channel: '/ws/orders',
  message: 'order.deleted',
  entity: 'Order',
  intent: 'evict',
  invalidates: ['Order[]'],
};

const customerUpdated: EntityStreamBinding = {
  channel: '/ws/customers',
  message: 'customer.updated',
  entity: 'Customer',
  intent: 'patch',
  invalidates: [],
};

function framesFixture(
  description: string,
  initial: { readonly rootType: string; readonly value: unknown },
  batches: readonly (readonly StreamFrame[])[],
): unknown {
  const cache = new QueryCache({ transport: fakeTransport(() => undefined), entities: schema });

  cache.store.write(initial.value, schema, initial.rootType);

  for (const batch of batches) applyFrames(cache, batch);

  const records: Record<string, unknown> = {};

  for (const key of [...cache.store.keys()].sort()) {
    records[key] = encode(cache.store.getRecord(key)?.data, { query: 'fixture', entity: key }).value;
  }

  return {
    description,
    entities: schema,
    initial,
    frames: batches.map((batch) =>
      batch.map((frame) => ({ binding: frame.binding, payload: frame.payload })),
    ),
    expected: { records },
  };
}

describe('cross-runtime fixtures', () => {
  it('writes or verifies the snapshot fixtures', async () => {
    settle(
      'snapshot/orders-normalized.json',
      await snapshotFixture(
        'Two queries sharing Order:7, nested Customer, LineItem and Invoice, owned by u-1',
        'u-1',
        orderSpecs,
        'normalized',
      ),
    );
    settle(
      'snapshot/orders-denormalized.json',
      await snapshotFixture('The same queries, denormalized', 'u-1', orderSpecs, 'denormalized'),
    );
    settle(
      'snapshot/reference-shaped-data.json',
      await snapshotFixture(
        'Response data shaped like a reference, which must be escaped, with no principal',
        undefined,
        [
          {
            operation: orderList,
            response: [{ id: 7, meta: { __ref: 'not a reference', ___ref: 'x' } }],
          },
        ],
        'normalized',
      ),
    );
    settle(
      'snapshot/numeric-principal.json',
      await snapshotFixture(
        'A snapshot owned by the number 42. TS accepts a numeric principal; Dart refuses it by design',
        42,
        [orderSpecs[0] as QuerySpec],
        'normalized',
      ),
    );
    settle(
      'snapshot/null-header-value.json',
      await snapshotFixture(
        'A query whose args.headers map holds a null value beside a string one, owned by u-1',
        'u-1',
        [
          {
            operation: orderGet,
            // `TagContext` does not type `headers`; `args` is stored and
            // serialized whole, so the extra key reaches the payload.
            args: {
              path: { id: 7 },
              headers: { 'x-region': 'eu', 'x-trace': null },
            } as TagContext,
            response: { id: 7, total: 99 },
          },
        ],
        'normalized',
      ),
    );
  });

  it('writes or verifies the codec fixtures', () => {
    settle(
      'codec/nested-entities.json',
      codecFixture('Orders with nested entities of every identity kind', 'Order', [
        {
          id: 7,
          total: 99,
          customer: { id: 'c-3', name: 'Ada' },
          items: [{ sku: 'A-1', qty: 2 }],
          invoice: { invoiceNumber: 'INV-1', amount: 99 },
        },
      ]),
    );
    settle(
      'codec/envelope.json',
      codecFixture('An envelope with no identity routing to entities', 'Envelope', {
        items: [{ id: 1, total: 3 }],
        total: 1,
        wrapper: { data: { id: 2, total: 4 } },
      }),
    );
    settle(
      'codec/reference-shaped-keys.json',
      codecFixture('Keys that collide with the reference marker or its escapes', 'Order', {
        id: 7,
        meta: { __ref: 'x' },
        ___ref: 1,
        __refs: 2,
      }),
    );
  });

  it('writes or verifies the frame fixtures', () => {
    settle(
      'frames/upsert-patch-evict.json',
      framesFixture(
        'Upsert, patch, evict by record and evict by bare id, in three batches',
        { rootType: 'Order', value: [{ id: 7, total: 1 }, { id: 8, total: 2 }] },
        [
          [{ binding: created, payload: { id: 9, total: 5 } }],
          [
            { binding: updated, payload: { id: 7, total: 100 } },
            { binding: deleted, payload: { id: 8 } },
          ],
          [{ binding: deleted, payload: 9 }],
        ],
      ),
    );
    settle(
      'frames/nested-patch.json',
      framesFixture(
        'A patch on a nested entity and on its parent in one batch',
        { rootType: 'Order', value: [{ id: 7, total: 1, customer: { id: 'c-3', name: 'Ada' } }] },
        [
          [
            { binding: customerUpdated, payload: { id: 'c-3', name: 'Grace' } },
            { binding: updated, payload: { id: 7, note: 'rush' } },
          ],
        ],
      ),
    );
    settle(
      'frames/coalesced-batch.json',
      framesFixture(
        'Three patches to one entity coalesced into one batch; the last wins',
        { rootType: 'Order', value: [{ id: 7, total: 0 }] },
        [
          [
            { binding: updated, payload: { id: 7, total: 1 } },
            { binding: updated, payload: { id: 7, total: 2 } },
            { binding: updated, payload: { id: 7, total: 3 } },
          ],
        ],
      ),
    );
  });
});
