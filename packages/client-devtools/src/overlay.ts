import type { Devtools } from './devtools.js';
import type { LogEntry, MissReport, RefetchReport } from './types.js';

/**
 * A panel, in the DOM, over the inspection API.
 *
 * **Deliberately not a component.** A React devtools panel forces React on a
 * Vue application; a Vue one forces Vue on an Angular application. This is
 * `document.createElement` and a shadow root, so it costs whichever framework
 * is present exactly nothing and works in an application that has none.
 *
 * The shadow root is not decoration either: an overlay whose styles leak into
 * the application it is inspecting, or whose styles are overridden by it, is a
 * debugging tool that introduces its own bugs. Everything inside is scoped, and
 * nothing outside is touched.
 *
 * It is also, on purpose, thinner than the API beneath it. A superb inspection
 * API with a plain panel is more useful than a beautiful panel over a thin one:
 * the API is what a developer calls from the console at three in the morning,
 * what a test asserts on, and what somebody else's panel is built from.
 */
export interface OverlayOptions {
  /** Where to attach. Defaults to `document.body`. */
  readonly parent?: Element;
  /** Start with the panel open. Defaults to false -- a button in the corner. */
  readonly open?: boolean;
}

type Tab = 'queries' | 'entities' | 'tags' | 'sockets' | 'log' | 'explain';

const TABS: readonly Tab[] = ['queries', 'entities', 'tags', 'sockets', 'log', 'explain'];

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
.panel { width: min(720px, 92vw); height: min(460px, 70vh); background: #1c1c22;
  border: 1px solid #45454f; border-radius: 6px; display: flex; flex-direction: column;
  box-shadow: 0 10px 40px rgba(0,0,0,.5); overflow: hidden; }
.bar { display: flex; gap: 4px; padding: 6px; border-bottom: 1px solid #35353d;
  align-items: center; flex-wrap: wrap; }
.bar .spacer { flex: 1; }
.body { overflow: auto; padding: 8px; flex: 1; }
table { border-collapse: collapse; width: 100%; }
th, td { text-align: left; padding: 3px 6px; border-bottom: 1px solid #2b2b33;
  vertical-align: top; word-break: break-word; }
th { color: #9a9aa8; font-weight: normal; position: sticky; top: -8px; background: #1c1c22; }
tr.hot td { background: #2a2233; }
code { color: #9fd0ff; }
.dim { color: #8a8a98; }
.warn { color: #ffb86b; }
.good { color: #8ce99a; }
.bad { color: #ff8f8f; }
h4 { margin: 10px 0 4px; font-size: 12px; color: #9a9aa8; font-weight: normal; }
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
export function mountOverlay(devtools: Devtools, options: OverlayOptions = {}): () => void {
  const doc = globalThis.document as Document | undefined;

  if (doc === undefined) {
    throw new Error('[forge] mountOverlay needs a DOM; there is no document here');
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

  const matches = (text: string): boolean =>
    filter === '' || text.toLowerCase().includes(filter.toLowerCase());

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

  const pills = (values: readonly string[], label: string): HTMLElement => {
    const wrap = el('div');

    wrap.append(el('span', 'dim', `${label}: `));

    if (values.length === 0) wrap.append(el('span', 'dim', 'none'));
    else for (const value of values) wrap.append(el('span', 'pill', value));

    return wrap;
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

  const renderBody = (body: HTMLElement): void => {
    switch (tab) {
      case 'queries': {
        const rows = devtools
          .queries()
          .filter((entry) => matches(entry.key))
          .map((entry) => [
            entry.key,
            String(entry.mounts),
            entry.stale ? 'stale' : entry.settled ? 'fresh' : 'empty',
            entry.tags.join(' '),
          ]);

        body.append(table(['key', 'mounts', 'state', 'tags'], rows));
        break;
      }

      case 'entities': {
        const rows = devtools
          .entities({ limit: 300 })
          .filter((record) => matches(record.key))
          .map((record) => [
            record.key,
            `v${String(record.version)}`,
            record.frameAt > 0 ? `frame ${String(record.frameAt)}` : '',
            Object.keys(record.fields).join(' '),
          ]);

        body.append(table(['entity', 'version', 'frame', 'fields'], rows));
        break;
      }

      case 'tags': {
        const rows = devtools
          .tags()
          .filter((row) => matches(row.tag))
          .map((row) => [
            row.tag,
            String(row.mounted.length),
            String(row.carriers.length),
            row.carriers.join(' '),
          ]);

        body.append(table(['tag', 'mounted', 'carriers', 'queries'], rows));
        break;
      }

      case 'sockets': {
        const rows = devtools
          .sockets()
          .filter((socket) => matches(socket.endpoint))
          .map((socket) => [
            socket.endpoint,
            socket.connected ? 'open' : socket.reconnecting ? 'reconnecting' : 'closed',
            String(socket.refs),
            String(socket.opens),
            socket.channels.map((c) => `${c.channel}(${String(c.handlers)})`).join(' '),
          ]);

        body.append(table(['endpoint', 'state', 'refs', 'opens', 'channels'], rows));
        break;
      }

      case 'log': {
        const entries = devtools.log().filter((entry) => matches(describe(entry)));
        const rows = entries
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

        body.append(table(['#', 'kind', 'what'], rows));
        break;
      }

      case 'explain':
        renderExplain(body);
        break;
    }
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

    const counts = devtools.store();
    bar.append(
      el(
        'span',
        'dim',
        `${String(counts.records)} entities / ${String(counts.mounted)} mounted / v${String(
          counts.version,
        )}`,
      ),
    );

    const close = el('button', undefined, 'x');
    close.addEventListener('click', () => {
      open = false;
      render();
    });
    bar.append(close);

    const body = el('div', 'body');

    renderBody(body);
    panel.append(bar, body);
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
