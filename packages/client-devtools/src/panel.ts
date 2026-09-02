import type { Devtools } from './devtools.js';
import type {
  LogEntry,
  MissReport,
  QueryDetail,
  RecordSnapshot,
  RefetchReport,
} from './types.js';

/**
 * The panel: everything the inspection API knows, with somewhere to click.
 *
 * `./overlay` is the lean one, six read-only tables and a filter box, and it
 * stays that way. This is the other trade: a detail pane, the actions, the
 * stream and frame views, and a budget of its own. You import whichever you
 * want, and neither can bloat the other.
 *
 * Still `document.createElement` in a shadow root. A React panel forces React
 * on a Vue application, and a Vue one forces Vue on an Angular application.
 */
export interface PanelOptions {
  /** Where to attach. Defaults to `document.body`. */
  readonly parent?: Element;
  /** Start with the panel open. Defaults to false -- a button in the corner. */
  readonly open?: boolean;
}

type Tab = 'queries' | 'entities' | 'tags' | 'sockets' | 'streams' | 'frames' | 'log' | 'explain';

const TABS: readonly Tab[] = [
  'queries',
  'entities',
  'tags',
  'sockets',
  'streams',
  'frames',
  'log',
  'explain',
];

const CSS = `
:host { all: initial; }
.root {
  position: fixed; right: 12px; bottom: 12px; z-index: 2147483000;
  font: 12px/1.45 ui-monospace, SFMono-Regular, Menlo, monospace; color: #e6e6e6;
}
button { font: inherit; color: inherit; background: #2c2c34; border: 1px solid #45454f;
  border-radius: 4px; padding: 3px 8px; cursor: pointer; }
button:hover { background: #3a3a44; }
button[aria-selected="true"] { background: #4b5bd6; border-color: #4b5bd6; }
.panel { width: min(1080px, 96vw); height: min(620px, 84vh); background: #1c1c22;
  border: 1px solid #45454f; border-radius: 6px; display: flex; flex-direction: column;
  box-shadow: 0 10px 40px rgba(0,0,0,.5); overflow: hidden; }
.bar { display: flex; gap: 4px; padding: 6px; border-bottom: 1px solid #35353d;
  align-items: center; flex-wrap: wrap; }
.bar .spacer { flex: 1; }
.split { display: flex; flex: 1; min-height: 0; }
.list { flex: 1 1 55%; overflow: auto; padding: 8px; border-right: 1px solid #35353d; }
.detail { flex: 1 1 45%; overflow: auto; padding: 8px; }
table { border-collapse: collapse; width: 100%; }
th, td { text-align: left; padding: 3px 6px; border-bottom: 1px solid #2b2b33;
  vertical-align: top; word-break: break-word; }
th { color: #9a9aa8; font-weight: normal; position: sticky; top: -8px; background: #1c1c22; }
tr.row { cursor: pointer; }
tr.row[aria-selected="true"] td { background: #2a2233; }
code { color: #9fd0ff; }
.dim { color: #8a8a98; }
.warn { color: #ffb86b; }
.good { color: #8ce99a; }
.bad { color: #ff8f8f; }
h4 { margin: 10px 0 4px; font-size: 12px; color: #9a9aa8; font-weight: normal; }
summary { cursor: pointer; color: #9a9aa8; }
ul { margin: 4px 0; padding-left: 18px; }
li { margin: 3px 0; }
input { font: inherit; color: inherit; background: #14141a; border: 1px solid #45454f;
  border-radius: 4px; padding: 3px 6px; min-width: 240px; }
.pill { display: inline-block; padding: 0 5px; border-radius: 3px; background: #2c2c34;
  margin: 0 3px 3px 0; }
`;

/**
 * Mount the panel. Returns the unmount.
 *
 * Refreshes on log activity, coalesced to one repaint per animation frame: a
 * channel at 200 messages a second must not repaint a table 200 times, and an
 * inspector that makes the application it is inspecting janky is measuring
 * itself.
 */
export function mountPanel(devtools: Devtools, options: PanelOptions = {}): () => void {
  const doc = globalThis.document as Document | undefined;

  if (doc === undefined) {
    throw new Error('[forge] mountPanel needs a DOM; there is no document here');
  }

  const parent = options.parent ?? doc.body;
  const host = doc.createElement('div');
  const shadow = host.attachShadow({ mode: 'open' });
  const style = doc.createElement('style');

  style.textContent = CSS;
  shadow.append(style);

  const root = doc.createElement('div');
  root.className = 'root';
  shadow.append(root);
  parent.append(host);

  let open = options.open ?? false;
  let tab: Tab = 'queries';
  let filter = '';
  let scheduled = false;

  /** The row selected in the `queries` or `entities` list. Task 10 fills the pane it feeds. */
  let selected: string | undefined;

  /** Which column the list is ordered by, and which way. */
  let sortBy: number | undefined;
  let descending = false;

  const el = (tag: string, className?: string, text?: string): HTMLElement => {
    const node = doc.createElement(tag);

    if (className !== undefined) node.className = className;
    if (text !== undefined) node.textContent = text;

    return node;
  };

  const table = (headers: readonly string[], rows: readonly (readonly string[])[]): HTMLElement => {
    const node = doc.createElement('table');
    const head = doc.createElement('tr');

    for (const header of headers) head.append(el('th', undefined, header));
    node.append(head);

    for (const row of rows) {
      const tr = doc.createElement('tr');

      for (const cell of row) tr.append(el('td', undefined, cell));
      node.append(tr);
    }

    if (rows.length === 0) {
      const tr = doc.createElement('tr');
      const td = el('td', 'dim', 'nothing here');

      td.setAttribute('colspan', String(headers.length));
      tr.append(td);
      node.append(tr);
    }

    return node;
  };

  /**
   * Like `table()`, but for the `queries` and `entities` tabs: rows carry a
   * key, are clickable, and the headers sort the list.
   */
  const rows = (
    headers: readonly string[],
    items: readonly { readonly key: string; readonly cells: readonly string[] }[],
  ): HTMLElement => {
    const node = doc.createElement('table');
    const head = doc.createElement('tr');

    for (const [index, header] of headers.entries()) {
      const th = el('th', undefined, header);

      th.style.cursor = 'pointer';
      th.addEventListener('click', () => {
        // A second click on the column already sorted reverses it; a click on
        // any other column starts that one ascending, which is what every
        // table anyone has used does.
        descending = sortBy === index ? !descending : false;
        sortBy = index;
        render();
      });
      head.append(th);
    }

    node.append(head);

    const column = sortBy;
    const ordered =
      column === undefined
        ? items
        : [...items].sort((left, right) => {
            const a = left.cells[column] ?? '';
            const b = right.cells[column] ?? '';

            // Localeless and numeric, so `10` sorts after `9` in a mounts
            // column rather than before it.
            return a.localeCompare(b, undefined, { numeric: true });
          });

    for (const item of descending ? [...ordered].reverse() : ordered) {
      const tr = doc.createElement('tr');

      tr.className = 'row';
      tr.setAttribute('aria-selected', String(item.key === selected));
      tr.addEventListener('click', () => {
        selected = item.key;
        render();
      });

      for (const cell of item.cells) tr.append(el('td', undefined, cell));
      node.append(tr);
    }

    if (items.length === 0) {
      const tr = doc.createElement('tr');
      const td = el('td', 'dim', 'nothing here');

      td.setAttribute('colspan', String(headers.length));
      tr.append(td);
      node.append(tr);
    }

    return node;
  };

  const matches = (text: string): boolean =>
    filter === '' || text.toLowerCase().includes(filter.toLowerCase());

  /**
   * The header buckets: `fresh N · stale N · fetching N · error N · unmounted N`.
   *
   * `mounts`, `stale` and `settled` live on the registry entry, which
   * `devtools.queries()` hands back in one read. `fetching` and `status` live
   * only on the record, and `devtools.records()` hands *those* back in one
   * read. Two linear passes and a map join.
   *
   * It used to be `devtools.detail(query.key)` inside this loop, which is a
   * fresh scan of `cache.tracked()` per query *and* a bounded deep copy of
   * that query's last settled response, allocated and thrown away, once per
   * query, at up to sixty repaints a second. `records()` exists so that this
   * line does not have to.
   */
  const buckets = (): string => {
    let fresh = 0;
    let stale = 0;
    let fetching = 0;
    let error = 0;
    let unmounted = 0;

    const tracked = new Map<string, RecordSnapshot>();

    for (const record of devtools.records()) tracked.set(record.key, record);

    for (const query of devtools.queries()) {
      const record = tracked.get(query.key);

      if (record?.fetching === true) fetching++;
      if (record?.status === 'error') error++;
      if (query.mounts === 0) unmounted++;
      if (query.stale) stale++;
      else if (query.settled) fresh++;
    }

    return `fresh ${String(fresh)} · stale ${String(stale)} · fetching ${String(
      fetching,
    )} · error ${String(error)} · unmounted ${String(unmounted)}`;
  };

  const describe = (entry: LogEntry): string => {
    switch (entry.kind) {
      case 'mutation':
        return `${entry.operation} -> ${entry.tags.join(', ') || 'no tags'}${
          entry.unresolved.length > 0 ? ` (skipped ${entry.unresolved.join(', ')})` : ''
        }`;
      case 'frames':
        return `${String(entry.frames)} frame(s) -> ${entry.tags.join(', ') || 'no tags'}`;
      case 'invalidated':
        return `${entry.query} hit by ${entry.matched.join(', ')}`;
      case 'placed':
        return `${entry.query} answered by placement`;
      case 'fetch':
        return `${entry.query} (${entry.reason})`;
      case 'settle':
        return `${entry.query} at store v${String(entry.version)}`;
      case 'error':
        return `${entry.query}: ${entry.message}`;
      case 'principal':
        return 'identity changed; the cache was dropped';
      case 'action':
        return `${entry.action} ${entry.target}`;
    }
  };

  const pills = (values: readonly string[], label: string): HTMLElement => {
    const wrap = el('div');

    wrap.append(el('span', 'dim', `${label}: `));

    if (values.length === 0) wrap.append(el('span', 'dim', 'none'));
    else for (const value of values) wrap.append(el('span', 'pill', value));

    return wrap;
  };

  const renderMiss = (body: HTMLElement, report: MissReport): void => {
    const tone =
      report.outcome === 'missed' ? 'bad' : report.outcome === 'refetched' ? 'good' : 'warn';

    body.append(el('h4', undefined, `outcome: ${report.outcome}`));
    body.append(el('p', tone, report.reason));
    body.append(el('h4', undefined, `cause: ${report.cause.label}`));
    body.append(pills(report.invalidated, 'invalidated'));
    body.append(pills(report.carried, 'carried'));
    body.append(pills(report.matched, 'matched'));

    if (report.cause.unresolved.length > 0) {
      body.append(pills(report.cause.unresolved, 'unresolved templates'));
    }

    if (report.nearest.length > 0) {
      body.append(el('h4', undefined, 'where they nearly meet'));
      const list = doc.createElement('ul');

      for (const miss of report.nearest) {
        list.append(
          el('li', undefined, `${miss.invalidated} vs ${miss.carried} (${miss.relation})`),
        );
      }

      body.append(list);
    }

    if (report.suggestions.length > 0) {
      body.append(el('h4', undefined, 'what to change'));
      const list = doc.createElement('ul');

      for (const suggestion of report.suggestions) list.append(el('li', undefined, suggestion));

      body.append(list);
    }
  };

  const renderRefetch = (body: HTMLElement, report: RefetchReport): void => {
    body.append(el('h4', undefined, `refetched (${report.reason})`));
    body.append(el('p', 'good', report.summary));

    if (report.cause !== undefined) {
      body.append(pills(report.cause.tags, `cause: ${report.cause.label} raised`));
    }

    body.append(pills(report.matched, 'matched'));
  };

  const renderExplain = (body: HTMLElement): void => {
    if (filter === '') {
      body.append(
        el(
          'p',
          'dim',
          'Type a query key above and press Enter. The key is what queries() lists: ' +
            'operation plus arguments.',
        ),
      );

      return;
    }

    const report = devtools.explain(filter);

    if ('outcome' in report) renderMiss(body, report);
    else renderRefetch(body, report);
  };

  const renderList = (body: HTMLElement): void => {
    switch (tab) {
      case 'queries': {
        const items = devtools
          .queries()
          .filter((entry) => matches(entry.key))
          .map((entry) => ({
            key: entry.key,
            cells: [
              entry.key,
              String(entry.mounts),
              entry.stale ? 'stale' : entry.settled ? 'fresh' : 'empty',
              entry.tags.join(' '),
            ],
          }));

        body.append(rows(['key', 'mounts', 'state', 'tags'], items));
        break;
      }

      case 'entities': {
        const items = devtools
          .entities({ limit: 300 })
          .filter((record) => matches(record.key))
          .map((record) => ({
            key: record.key,
            cells: [
              record.key,
              `v${String(record.version)}`,
              record.frameAt > 0 ? `frame ${String(record.frameAt)}` : '',
              Object.keys(record.fields).join(' '),
            ],
          }));

        body.append(rows(['entity', 'version', 'frame', 'fields'], items));
        break;
      }

      case 'tags': {
        const tagRows = devtools
          .tags()
          .filter((row) => matches(row.tag))
          .map((row) => [
            row.tag,
            String(row.mounted.length),
            String(row.carriers.length),
            row.carriers.join(' '),
          ]);

        body.append(table(['tag', 'mounted', 'carriers', 'queries'], tagRows));
        break;
      }

      case 'sockets': {
        const socketRows = devtools
          .sockets()
          .filter((socket) => matches(socket.endpoint))
          .map((socket) => [
            socket.endpoint,
            socket.connected ? 'open' : socket.reconnecting ? 'reconnecting' : 'closed',
            String(socket.refs),
            String(socket.opens),
            socket.channels.map((c) => `${c.channel}(${String(c.handlers)})`).join(' '),
          ]);

        body.append(table(['endpoint', 'state', 'refs', 'opens', 'channels'], socketRows));
        break;
      }

      case 'streams': {
        const view = devtools.streams();

        if (view === undefined) {
          body.append(
            el(
              'p',
              'dim',
              'no stream runtime is attached to this cache. Pass `binder` to attach(), or ' +
                'wire a StreamBinder, and this tab fills in.',
            ),
          );

          break;
        }

        if (view.recovering.length > 0) {
          body.append(
            el(
              'p',
              'warn',
              `recovering after a reconnect: ${view.recovering.join(', ')}. Frames were ` +
                'missed while the socket was down.',
            ),
          );
        }

        body.append(el('h4', undefined, `bindings (${String(view.queued)} frame(s) queued)`));
        body.append(
          table(
            ['channel', 'message', 'entity', 'intent', 'invalidates'],
            view.channels.flatMap((channel) =>
              channel.bindings
                .filter((binding) => matches(`${channel.channel} ${binding.message}`))
                .map((binding) => [
                  channel.channel,
                  binding.message,
                  binding.entity,
                  binding.intent,
                  binding.invalidates.join(' '),
                ]),
            ),
          ),
        );

        body.append(el('h4', undefined, 'live queries'));
        body.append(
          table(
            ['channel', 'query', 'refs'],
            view.live
              .filter((entry) => matches(entry.key))
              .map((entry) => [entry.channel, entry.key, String(entry.refs)]),
          ),
        );

        break;
      }

      case 'frames': {
        if (!devtools.capturing) {
          body.append(
            el(
              'p',
              'dim',
              'frame capture is off. It retains payloads, which nothing else here does, so ' +
                'you have to ask: attach(client, { frames: { limit: 200 } }).',
            ),
          );

          break;
        }

        const captured = devtools
          .frames()
          .filter((frame) => matches(`${frame.channel} ${frame.message}`));

        body.append(
          table(
            ['#', 'channel', 'message', 'intent', 'entity'],
            captured
              .slice(-300)
              .reverse()
              .map((frame) => [
                String(frame.seq),
                frame.channel,
                frame.message,
                frame.intent,
                frame.entity,
              ]),
          ),
        );

        for (const frame of captured.slice(-20).reverse()) {
          body.append(explorer(frame.payload, `${frame.message} #${String(frame.seq)}`));
        }

        break;
      }

      case 'log': {
        const entries = devtools.log().filter((entry) => matches(describe(entry)));
        const logRows = entries
          .slice(-300)
          .reverse()
          .map((entry) => [String(entry.seq), entry.kind, describe(entry)]);

        if (devtools.dropped > 0) {
          body.append(
            el(
              'p',
              'dim',
              `${String(devtools.dropped)} earlier event(s) have been overwritten; the log holds ` +
                `the most recent ${String(devtools.capacity)}.`,
            ),
          );
        }

        body.append(table(['#', 'kind', 'what'], logRows));
        break;
      }

      case 'explain':
        renderExplain(body);
        break;
    }
  };

  const field = (label: string, value: string): HTMLElement => {
    const row = el('div');

    row.append(el('span', 'dim', `${label}: `));
    row.append(el('span', undefined, value));

    return row;
  };

  /**
   * The explorer, which is a `<details>` tree and nothing cleverer.
   *
   * The value arrives already bounded -- see `capped` in `inspect.ts` -- so
   * this walks it without a depth guard of its own and cannot be handed a
   * cycle.
   */
  const explorer = (value: unknown, label: string): HTMLElement => {
    if (value === null || typeof value !== 'object') return field(label, String(value));

    const node = doc.createElement('details');
    const summary = doc.createElement('summary');

    summary.textContent = Array.isArray(value)
      ? `${label} [${String(value.length)}]`
      : `${label} {${String(Object.keys(value as object).length)}}`;
    node.append(summary);

    for (const [key, member] of Object.entries(value as Record<string, unknown>)) {
      node.append(explorer(member, key));
    }

    return node;
  };

  /**
   * The only part of this file that writes.
   *
   * The refetch rejection is swallowed on purpose: a failing refetch is a
   * normal thing to be looking at, and an unhandled rejection raised by the
   * panel would be reported as though the application had one.
   */
  const actionBar = (detail: QueryDetail): HTMLElement => {
    const bar = el('div', 'bar');

    const button = (label: string, run: () => void): void => {
      const node = el('button', undefined, label);

      node.addEventListener('click', () => {
        run();
        render();
      });
      bar.append(node);
    };

    button('refetch', () => {
      void devtools.actions.refetch(detail.key).catch(() => undefined);
    });
    button('invalidate', () => {
      devtools.actions.invalidate(detail.key);
    });
    button('drop', () => {
      devtools.actions.drop(detail.key);
    });

    return bar;
  };

  const renderDetail = (body: HTMLElement): void => {
    if (selected === undefined) {
      body.append(el('p', 'dim', 'Pick a query on the left.'));

      return;
    }

    const detail = devtools.detail(selected);

    if (detail === undefined) {
      body.append(el('p', 'dim', `${selected} is no longer tracked.`));

      return;
    }

    body.append(el('h4', undefined, detail.key));
    body.append(actionBar(detail));
    body.append(field('operation', detail.operation));
    body.append(field('status', detail.status));
    body.append(field('fetching', String(detail.fetching)));
    body.append(field('mounts', String(detail.mounts)));
    body.append(field('stale', String(detail.stale)));
    body.append(field('settledAt', String(detail.settledAt)));
    body.append(field('restarts', String(detail.frameRestarts)));

    if (detail.error !== undefined) body.append(field('error', detail.error));

    body.append(pills(detail.provides, 'provides'));
    body.append(pills(detail.tags, 'tags'));
    body.append(pills(detail.deps, 'deps'));
    body.append(explorer(detail.value, 'value'));
  };

  const render = (): void => {
    root.replaceChildren();

    if (!open) {
      const button = el('button', undefined, 'forge');

      button.addEventListener('click', () => {
        open = true;
        render();
      });
      root.append(button);

      return;
    }

    const panel = el('div', 'panel');
    const bar = el('div', 'bar');

    for (const name of TABS) {
      const button = el('button', undefined, name);

      button.setAttribute('aria-selected', String(name === tab));
      button.addEventListener('click', () => {
        tab = name;
        render();
      });
      bar.append(button);
    }

    const search = doc.createElement('input');

    search.value = filter;
    search.placeholder = tab === 'explain' ? 'query key, then Enter' : 'filter';
    search.addEventListener('change', () => {
      filter = search.value;
      render();
    });
    bar.append(search);

    const spacer = el('div', 'spacer');
    bar.append(spacer);

    bar.append(el('span', 'dim', buckets()));

    const clearCache = el('button', undefined, 'clear cache');

    clearCache.addEventListener('click', () => {
      devtools.actions.clear();
      selected = undefined;
      render();
    });
    bar.append(clearCache);

    const clearLog = el('button', undefined, 'clear log');

    clearLog.addEventListener('click', () => {
      devtools.clear();
      render();
    });
    bar.append(clearLog);

    const close = el('button', undefined, 'x');
    close.addEventListener('click', () => {
      open = false;
      render();
    });
    bar.append(close);

    const split = el('div', 'split');
    const list = el('div', 'list');
    const detail = el('div', 'detail');

    renderList(list);
    renderDetail(detail);

    split.append(list, detail);
    panel.append(bar, split);
    root.append(panel);
  };

  const schedule = (): void => {
    if (scheduled || !open) return;

    scheduled = true;

    const raf = (globalThis as { requestAnimationFrame?: (cb: () => void) => unknown })
      .requestAnimationFrame;
    const run = (): void => {
      scheduled = false;
      render();
    };

    if (typeof raf === 'function') raf(run);
    else void Promise.resolve().then(run);
  };

  const unsubscribe = devtools.subscribe(schedule);

  render();

  return () => {
    unsubscribe();
    host.remove();
  };
}
