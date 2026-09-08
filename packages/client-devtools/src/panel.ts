import { capture } from './frames.js';
import { nearMisses } from './tag.js';
import type { NearMiss } from './tag.js';
import type { Devtools } from './devtools.js';
import type {
  EntitySnapshot,
  LogEntry,
  MissReport,
  OverlaySnapshot,
  QueryDetail,
  QuerySnapshot,
  RecordSnapshot,
  RefetchReport,
  TagSnapshot,
} from './types.js';
import type { RequestSnapshot } from './requests.js';
import type { RevalidationSource } from './control.js';

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
  /**
   * How far off the bottom edge to sit, in pixels.
   *
   * Set this and nothing is guessed. Left unset, the launcher looks for a
   * framework's own dev badge in the same corner and lifts itself above one if
   * it finds it, because the bottom right is a crowded address.
   */
  readonly offset?: number;
}

type Tab =
  | 'trace'
  | 'network'
  | 'queries'
  | 'entities'
  | 'overlay'
  | 'tags'
  | 'sockets'
  | 'streams'
  | 'frames'
  | 'explain';

const TABS: readonly Tab[] = [
  'trace',
  'network',
  'queries',
  'entities',
  'overlay',
  'tags',
  'sockets',
  'streams',
  'frames',
  'explain',
];

/**
 * The forge mark, as markup. See the note on the copy in `./overlay`.
 *
 * Duplicated rather than shared, deliberately. The two UIs are chosen rather
 * than layered, and neither imports the other precisely so that neither can
 * bloat the other's budget; a `mark.ts` they both pull in would put the string
 * into the bundle of an application that only wanted one of them.
 */
const MARK =
  '<svg viewBox="0 0 559 552" fill="currentColor" aria-hidden="true" focusable="false">' +
  '<rect x="559" y="0" width="125" height="425" transform="rotate(90 559 0)"/>' +
  '<rect x="425" y="218" width="125" height="291" transform="rotate(90 425 218)"/>' +
  '<path d="M432 342.304V218H558.551L432 342.304Z"/>' +
  '<path d="M0 551.304V136H127V427L0 551.304Z"/>' +
  '<path d="M127 125H0.342773L127 0.137726V125Z"/></svg>';

/** Where the panel sits. `window` is the detached one. */
type Mode = 'bottom' | 'right' | 'full' | 'window';

/**
 * The dock control: mode, what the tooltip says, and the glyph.
 *
 * Icon only, with a tooltip and an `aria-label` carrying the name. Four
 * labelled buttons cost more of the title bar than the breadcrumb beside them,
 * and the tabs below are where words actually earn their room -- those are the
 * navigation, and an icon row you have to learn would be a worse trade there.
 */
const DOCKS: readonly (readonly [Mode, string, string])[] = [
  [
    'bottom',
    'Dock bottom',
    '<rect x="1.6" y="2.2" width="10.8" height="9.6" rx="1.4"/><path d="M1.6 8.6h10.8" stroke-width="2.4"/>',
  ],
  [
    'right',
    'Dock right',
    '<rect x="1.6" y="2.2" width="10.8" height="9.6" rx="1.4"/><path d="M8.6 2.2v9.6" stroke-width="2.4"/>',
  ],
  ['full', 'Fullscreen', '<path d="M2 5.2V2h3.2M12 5.2V2H8.8M2 8.8V12h3.2M12 8.8V12H8.8"/>'],
  [
    'window',
    'Detach to its own window',
    '<path d="M7 2.2H2.4v9.4h9.4V7"/><path d="M8.8 2.2h3v3M11.8 2.2 7.4 6.6"/>',
  ],
];

/**
 * Signal bars at three strengths, and the glyphs the rail needs.
 *
 * Filled rather than stroked for the bars, because three stroked rectangles at
 * 13px read as a smear.
 */
const NETWORK: readonly (readonly ['online' | 'slow' | 'offline', string, string])[] = [
  [
    'online',
    'Online',
    '<g fill="currentColor" stroke="none"><rect x="1" y="9" width="2.6" height="4" rx=".6"/>' +
      '<rect x="5.7" y="6" width="2.6" height="7" rx=".6"/>' +
      '<rect x="10.4" y="2.4" width="2.6" height="10.6" rx=".6"/></g>',
  ],
  [
    'slow',
    'Throttle to a slow connection',
    '<g fill="currentColor" stroke="none"><rect x="1" y="9" width="2.6" height="4" rx=".6"/>' +
      '<rect x="5.7" y="6" width="2.6" height="7" rx=".6" opacity=".26"/>' +
      '<rect x="10.4" y="2.4" width="2.6" height="10.6" rx=".6" opacity=".26"/></g>',
  ],
  [
    'offline',
    'Go offline',
    '<g fill="currentColor" stroke="none" opacity=".26">' +
      '<rect x="1" y="9" width="2.6" height="4" rx=".6"/>' +
      '<rect x="5.7" y="6" width="2.6" height="7" rx=".6"/>' +
      '<rect x="10.4" y="2.4" width="2.6" height="10.6" rx=".6"/></g>' +
      '<path d="M1.7 12.3 12.3 1.7"/>',
  ],
];

const REVALIDATE: Record<RevalidationSource, readonly [string, string]> = {
  focus: [
    'Revalidate on window focus',
    '<circle cx="7" cy="7" r="4.6"/><circle cx="7" cy="7" r="1.5" fill="currentColor" stroke="none"/>',
  ],
  reconnect: [
    'Revalidate on reconnect',
    '<path d="M11.7 7a4.7 4.7 0 1 1-1.5-3.4"/><path d="M11.8 1.5v3H8.9"/>',
  ],
  poll: ['Poll on an interval', '<circle cx="7" cy="7" r="5"/><path d="M7 4.1V7l2.1 1.4"/>'],
};

const ICONS = {
  zap: '<path d="M7.9 1.4 3.1 8.1h3.5l-.6 4.5L10.9 5.9H7.3z"/>',
  snow: '<path d="M7 1.6v10.8M2.3 4.3l9.4 5.4M11.7 4.3 2.3 9.7"/>',
} as const;

/** A stroked 14x14 glyph. Every icon in this file is drawn on the same grid. */
function icon(paths: string): string {
  return (
    '<svg viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.2" ' +
    'stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">' +
    paths +
    '</svg>'
  );
}

const CSS = `
:host { all: initial; }
.root {
  position: fixed; right: 12px; bottom: 12px; z-index: 2147483000;
  --ground: #0e1116; --panel: #14181f; --raised: #1a1f28; --hover: #1f2531;
  --line: #242a35; --line2: #2e3542; --text: #e3e7ee; --dim: #8992a2;
  --faint: #5e6675; --ember: #ff6a2b; --mint: #3fd39c; --amber: #ffb648;
  --coral: #ff6b6b; --violet: #ae8cff; --sky: #62a8ff;
  --ui: "Inter", "Segoe UI", system-ui, -apple-system, sans-serif;
  --mono: ui-monospace, SFMono-Regular, "SF Mono", Menlo, monospace;
  font: 12px/1.5 var(--ui); color: var(--text);
}

/* chrome speaks sans, data speaks mono. The old sheet was mono throughout,
   which flattened labels and values into one texture. */
button { font: 500 12px/1.4 var(--ui); color: var(--dim); background: transparent;
  border: 1px solid transparent; border-radius: 6px; padding: 4px 10px; cursor: pointer; }
button:hover { background: var(--hover); color: var(--text); }
button[aria-selected="true"] { background: var(--raised); border-color: var(--line2);
  color: var(--text); box-shadow: inset 0 -2px 0 var(--ember); }
button:focus-visible { outline: 2px solid var(--ember); outline-offset: 1px; }

.panel { width: min(1100px, 96vw); height: min(660px, 84vh); background: var(--ground);
  border: 1px solid var(--line2); border-radius: 10px; display: flex; flex-direction: column;
  box-shadow: 0 24px 70px rgba(0,0,0,.5); overflow: hidden; }
.panel[data-mode="full"] { width: 96vw; height: 92vh; }
.panel[data-mode="right"] { width: min(560px, 96vw); height: 92vh; }

.bar { display: flex; gap: 2px; padding: 6px 8px; border-bottom: 1px solid var(--line);
  align-items: center; flex-wrap: wrap; background: var(--panel); }
.bar .spacer { flex: 1; }
.bar > span.dim { font: 11px var(--mono); color: var(--faint); padding: 0 6px; }

input { font: 11px var(--mono); color: var(--text); background: var(--ground);
  border: 1px solid var(--line2); border-radius: 6px; padding: 4px 8px; min-width: 200px; }
input::placeholder { color: var(--faint); }
input:focus-visible { outline: none; border-color: var(--ember); }

.split { display: flex; flex: 1; min-height: 0; }
.list { flex: 1 1 55%; overflow: auto; padding: 0; border-right: 1px solid var(--line); }
.list:only-child { flex: 1 1 100%; border-right: 0; }
.detail { flex: 1 1 45%; overflow: auto; padding: 10px 12px; background: var(--panel); }

/* tables. min-content must not collapse to one character: word-break:break-word
   computes to overflow-wrap:anywhere, which does exactly that in a table. */
table { border-collapse: collapse; width: 100%; }
th, td { text-align: left; padding: 6px 10px; border-bottom: 1px solid var(--line);
  vertical-align: top; overflow-wrap: break-word; }
th { position: sticky; top: 0; z-index: 2; background: var(--ground);
  font: 500 9.5px/1.4 var(--ui); letter-spacing: .12em; text-transform: uppercase;
  color: var(--faint); white-space: nowrap; cursor: pointer; }
th:hover { color: var(--dim); }
td { font: 11.5px/1.5 var(--mono); color: var(--dim); font-variant-numeric: tabular-nums; }
td:first-child { color: var(--text); }
tr.row { cursor: pointer; }
tr.row:hover td { background: var(--hover); }
tr.row[aria-selected="true"] td { background: var(--raised); }
tr.row[aria-selected="true"] td:first-child { box-shadow: inset 2px 0 0 var(--ember); }

code { color: var(--sky); font: 11.5px var(--mono); }
.dim { color: var(--faint); }
.warn { color: var(--amber); }
.good { color: var(--mint); }
.bad { color: var(--coral); }
h4 { margin: 14px 0 6px; font: 500 9.5px/1.4 var(--ui); letter-spacing: .13em;
  text-transform: uppercase; color: var(--faint); }
summary { cursor: pointer; color: var(--dim); font: 11px var(--mono); }
ul { margin: 6px 0; padding-left: 18px; }
li { margin: 4px 0; font: 11.5px/1.6 var(--mono); color: var(--dim); }
pre { font: 10.5px/1.6 var(--mono); background: var(--ground); border: 1px solid var(--line);
  border-radius: 5px; padding: 8px 10px; overflow-x: auto; color: var(--dim); margin: 0; }
.pill { display: inline-block; padding: 1px 6px; border-radius: 3px; margin: 0 3px 3px 0;
  font: 10px var(--mono); border: 1px solid var(--line2); color: var(--violet);
  background: rgba(174,140,255,.08); }

/* launcher */
.launcher-dock { display: flex; align-items: center; padding: 5px;
  border: 1px solid transparent; border-radius: 9px; }
.launcher-dock:hover, .launcher-dock:focus-within { background: var(--panel);
  border-color: var(--line2); box-shadow: 0 6px 22px rgba(0,0,0,.34); }
.launcher-dock:hover .launcher, .launcher-dock:focus-within .launcher {
  background: transparent; border-color: transparent; box-shadow: none; }
.launcher { width: 30px; height: 30px; padding: 0; display: grid; place-items: center;
  position: relative; border-radius: 8px; background: var(--panel); border: 1px solid var(--line2);
  box-shadow: 0 6px 22px rgba(0,0,0,.34); color: var(--text); }
.launcher:hover { background: var(--raised); }
.launcher svg { width: 15px; height: 15px; display: block; }
.launcher::after { content: ""; position: absolute; inset: -2px; border-radius: 10px;
  border: 1.5px solid var(--mint); opacity: .55; pointer-events: none; }
.launcher[data-pulse="fetching"]::after { border-color: var(--sky); }
.launcher[data-pulse="pending"]::after { border-color: var(--amber); }
.launcher[data-pulse="error"]::after { border-color: var(--coral); opacity: .9; }
.launcher[data-pulse="offline"]::after { border-color: var(--ember); opacity: .95; }
.launcher[data-pulse="slow"]::after { border-color: var(--ember); opacity: .5; }
.launcher .badge { position: absolute; top: -5px; right: -5px; min-width: 15px; height: 15px;
  border-radius: 8px; background: var(--coral); color: #12151b; font: 600 9.5px var(--ui);
  display: grid; place-items: center; padding: 0 4px; border: 2px solid var(--ground); }
.launcher-vitals { display: none; align-items: center; gap: 4px; padding: 0 8px;
  margin-left: 3px; border-left: 1px solid var(--line); color: var(--dim);
  font: 10.5px var(--mono); white-space: nowrap; }
.launcher-vitals b { color: var(--text); font-weight: 500; }
.launcher-dock:hover .launcher-vitals, .launcher-dock:focus-within .launcher-vitals {
  display: flex; }

/* icon chrome */
.ico { width: 26px; height: 22px; padding: 0; display: grid; place-items: center;
  border-radius: 5px; color: var(--dim); }
.ico:hover { background: var(--hover); color: var(--text); }
.ico[aria-pressed="true"] { background: var(--raised); color: var(--ember); border-color: var(--line2); }
.ico svg { width: 13px; height: 13px; display: block; }
.docks { display: flex; border: 1px solid var(--line2); border-radius: 6px; overflow: hidden; }
.docks .ico { border-radius: 0; border: 0; border-right: 1px solid var(--line2); width: 28px; }
.docks .ico:last-child { border-right: 0; }
.tip { position: relative; }
.tip::after { content: attr(data-tip); position: absolute; top: calc(100% + 7px); left: 50%;
  transform: translateX(-50%); background: var(--raised); color: var(--text);
  border: 1px solid var(--line2); border-radius: 5px; padding: 3px 7px;
  font: 10px var(--mono); white-space: nowrap; pointer-events: none; opacity: 0;
  transition: opacity .12s ease .3s; z-index: 20; }
.tip:hover::after, .tip:focus-visible::after { opacity: 1; }
.tip-r::after { left: auto; right: 0; transform: none; }

/* vitals */
.vitals { display: flex; overflow-x: auto; border-bottom: 1px solid var(--line);
  background: var(--ground); }
.vital { padding: 6px 14px; border-right: 1px solid var(--line); white-space: nowrap; }
.vital .k { display: block; color: var(--faint); font: 500 9.5px/1.4 var(--ui);
  letter-spacing: .12em; text-transform: uppercase; }
.vital .v { color: var(--text); font: 14px var(--mono); font-variant-numeric: tabular-nums; }
.vital .n { color: var(--faint); font: 10px var(--mono); margin-left: 5px; }
.vital[data-vital="pending"] .v { color: var(--amber); }

/* rail */
.rail { display: flex; gap: 7px; align-items: center; flex-wrap: wrap; padding: 5px 8px;
  border-bottom: 1px solid var(--line); background: var(--panel); }
.rail .spacer { flex: 1; }
.rail-label { color: var(--faint); font: 500 9.5px/1.4 var(--ui); letter-spacing: .12em;
  text-transform: uppercase; }
.latency { width: 84px; accent-color: var(--ember); }

/* facets */
.facets { display: flex; gap: 5px; flex-wrap: wrap; padding: 8px 10px 6px; }
.facet { font: 10px var(--mono); padding: 2px 9px; border-radius: 10px;
  border: 1px solid var(--line2); color: var(--dim); }
.facet:hover { color: var(--text); background: var(--hover); }
.facet[aria-pressed="true"] { background: rgba(255,106,43,.12);
  border-color: rgba(255,106,43,.5); color: var(--ember); }
.facet .count { color: var(--faint); margin-left: 5px; }
.facet[aria-pressed="true"] .count { color: var(--ember); }
.facet.clear { border-color: transparent; text-decoration: underline; }

/* trace */
.cause { position: relative; padding: 10px 12px 10px 30px; border-bottom: 1px solid var(--line); }
.cause::before { content: ""; position: absolute; left: 14px; top: 26px; bottom: 10px;
  width: 1px; background: var(--line2); }
.cause::after { content: ""; position: absolute; left: 10px; top: 12px; width: 9px; height: 9px;
  border-radius: 2px; background: var(--faint); }
.cause[data-kind="mutation"]::after { background: var(--violet); }
.cause[data-kind="frames"]::after { background: var(--sky); }
.cause[data-kind="action"]::after { background: var(--ember); }
.cause[data-kind="error"]::after { background: var(--coral); }
.cause-head { display: flex; gap: 8px; align-items: baseline; flex-wrap: wrap;
  font: 11.5px var(--mono); }
.cause-head .seq { color: var(--faint); font-variant-numeric: tabular-nums; }
.cause-head .kind { color: var(--faint); font: 500 9.5px/1.4 var(--ui); letter-spacing: .1em;
  text-transform: uppercase; }
.cause-head .op { color: var(--text); }
.effect { position: relative; margin-top: 7px; padding-left: 14px; font: 11px var(--mono);
  color: var(--dim); }
.effect::before { content: ""; position: absolute; left: -12px; top: 8px; width: 20px;
  height: 1px; background: var(--line2); }
.nearmiss { margin-top: 8px; padding: 6px 9px; border: 1px solid rgba(255,182,72,.28);
  background: rgba(255,182,72,.06); border-radius: 5px; color: var(--amber);
  font: 10.5px/1.5 var(--mono); }
.nearmiss .warn { font: 500 9.5px/1.4 var(--ui); letter-spacing: .1em; text-transform: uppercase;
  margin-right: 6px; }

/* network */
.wf { display: flex; height: 8px; min-width: 90px; border-radius: 2px; background: var(--line);
  overflow: hidden; }
.wf i { display: block; height: 100%; }
.wf .wire { background: var(--sky); }
.wf .auth { background: var(--violet); }
.wf .backoff { background: repeating-linear-gradient(90deg, var(--line2) 0 3px,
  transparent 3px 6px); }
.wf .pending { width: 100%; background: repeating-linear-gradient(90deg, var(--line) 0 4px,
  transparent 4px 8px); }
.curl { white-space: pre-wrap; }

/* overlay stack */
.layer { border: 1px solid var(--line2); border-radius: 7px; padding: 10px 12px;
  margin: 0 12px 8px; background: var(--panel); }
.patch { display: flex; gap: 8px; margin-top: 5px; font: 10.5px var(--mono); }
.patch .kind { min-width: 46px; text-align: right; color: var(--faint); }
.patch .kind.merge { color: var(--sky); }
.patch .kind.create { color: var(--mint); }
.patch .kind.delete { color: var(--coral); }
.patch .key { color: var(--text); word-break: break-all; }
.diff { font: 10.5px/1.6 var(--mono); margin-top: 8px; }
.diff .was { color: var(--coral); }
.diff .now { color: var(--mint); }
.diff .same { color: var(--faint); }
.buttons { display: flex; gap: 5px; flex-wrap: wrap; margin-top: 10px; }
.buttons button { font: 10.5px var(--mono); background: var(--raised);
  border: 1px solid var(--line2); padding: 4px 9px; }
.buttons button:hover { color: var(--text); border-color: var(--faint); }
.buttons input { min-width: 0; width: 92px; }

.held { color: var(--ember); }
.held-banner { border: 1px solid rgba(255,106,43,.45); background: rgba(255,106,43,.08);
  border-radius: 5px; padding: 8px 10px; color: var(--ember); margin-bottom: 10px;
  font: 10.5px/1.55 var(--mono); }
.detail-head { display: flex; align-items: center; gap: 8px; position: sticky; top: 0;
  margin: -10px -12px 10px; padding: 7px 8px 7px 12px; background: var(--panel);
  border-bottom: 1px solid var(--line); z-index: 3; }
.detail-head .what { color: var(--faint); font: 500 9.5px/1.4 var(--ui); letter-spacing: .13em;
  text-transform: uppercase; }
.detail-head .spacer { flex: 1; }
.no-rows { padding: 20px 14px; font: 11px var(--mono); color: var(--faint); }
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

  // Only ever what you asked for. Guessing from an element name got this
  // wrong: a framework whose badge is present but not in this corner, or not
  // rendered at all, still matched, and the launcher lifted itself off the
  // corner for no reason. Sitting where you put it beats a clever guess.
  if (options.offset !== undefined) {
    root.style.bottom = `${String(options.offset)}px`;
    root.setAttribute('data-offset', 'set');
  } else {
    root.setAttribute('data-offset', 'none');
  }

  shadow.append(root);
  parent.append(host);

  let open = options.open ?? false;
  // The first tab, and the same one `1` selects. The trace is what the panel
  // is for; a list of queries is what every other devtools panel opens on.
  let tab: Tab = 'trace';
  let filter = '';
  let scheduled = false;
  let mode: Mode = 'bottom';
  let frozen = false;
  let compact = false;
  /**
   * Queries the panel is holding in a state they did not reach.
   *
   * Panel-local on purpose. Nothing here writes the cache, so the real record
   * is untouched and comes back whole on release; there is no state to unwind
   * and no way for a held query to outlive the panel that held it. Every hold
   * and release goes to the trace as an action, so a spinner you caused never
   * reads as a spinner the application caused.
   */
  const holds = new Map<string, 'loading' | 'error'>();

  /**
   * The row selected on each list tab, and the column each list tab is
   * ordered by. Both keyed by tab, and both used to be one global.
   *
   * A single `selected` string meant clicking `Order:1` on the entities tab
   * and then asking `detail()` about it -- a registry lookup -- which reported
   * a record visibly on screen as no longer tracked. A single `sortBy` meant
   * sorting queries by column 2 and finding entities silently sorted by *its*
   * column 2, where the first click on any other column reversed instead of
   * starting ascending, because the toggle compared only the index.
   *
   * Keying by tab is also how the detail pane knows which kind of key it is
   * holding. A query key and an entity key are both strings, and nothing about
   * their shape reliably separates them.
   */
  const selected = new Map<Tab, string>();
  const sortBy = new Map<Tab, { readonly column: number; readonly descending: boolean }>();
  /** Which facet chips are pressed, per tab. Same reasoning as `sortBy`. */
  const facets = new Map<Tab, Set<string>>();

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
    items: readonly {
      readonly key: string;
      readonly cells: readonly string[];
      /** Drawn into the last cell. For a bar, which is not a string. */
      readonly extra?: HTMLElement;
    }[],
  ): HTMLElement => {
    const node = doc.createElement('table');
    const head = doc.createElement('tr');

    for (const [index, header] of headers.entries()) {
      const th = el('th', undefined, header);

      th.style.cursor = 'pointer';
      th.addEventListener('click', () => {
        const current = sortBy.get(tab);

        // A second click on the column already sorted reverses it; a click on
        // any other column starts that one ascending, which is what every
        // table anyone has used does. `current.column === index` and not a
        // bare index comparison, so the reversal belongs to this tab's column
        // and not to whichever column some other tab happens to be sorted by.
        sortBy.set(tab, {
          column: index,
          descending: current?.column === index ? !current.descending : false,
        });
        render();
      });
      head.append(th);
    }

    node.append(head);

    const order = sortBy.get(tab);
    const column = order?.column;
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

    const picked = selected.get(tab);
    // The tab these rows belong to, captured rather than read at click time:
    // a handler that resolved `tab` later would file the click under whatever
    // tab happened to be current then.
    const here = tab;

    for (const item of order?.descending === true ? [...ordered].reverse() : ordered) {
      const tr = doc.createElement('tr');

      tr.className = 'row';
      tr.setAttribute('aria-selected', String(item.key === picked));
      tr.addEventListener('click', () => {
        selected.set(here, item.key);
        render();
      });

      for (const cell of item.cells) tr.append(el('td', undefined, cell));

      if (item.extra !== undefined) tr.lastElementChild?.append(item.extra);

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

  /**
   * What one row offers the filter box.
   *
   * The operators need structure, not a joined string: `status:4xx` over a
   * concatenation of every cell would match a query whose *key* contained
   * `404`, and `>100ms` cannot be answered by substring at all. Each tab hands
   * over the fields it actually has, and a term the tab cannot answer matches
   * nothing rather than everything -- asking a duration question of the
   * entities tab should return no rows, not all of them.
   */
  interface Matchable {
    readonly text: string;
    readonly status?: number | undefined;
    readonly ms?: number | undefined;
    readonly tags?: readonly string[] | undefined;
  }

  type Term =
    | { readonly kind: 'none' }
    | { readonly kind: 'text'; readonly value: string }
    | { readonly kind: 'status'; readonly klass: number }
    | { readonly kind: 'slower'; readonly ms: number }
    | { readonly kind: 'tag'; readonly value: string };

  /** `status:4xx`, `>100ms`, `tag:Order[]`, or a plain substring. */
  const parseFilter = (): Term => {
    const text = filter.trim();

    if (text === '') return { kind: 'none' };

    const status = /^status:([1-5])xx$/i.exec(text);

    if (status !== null) return { kind: 'status', klass: Number(status[1]) };

    const slower = /^>\s*(\d+)\s*ms$/i.exec(text);

    if (slower !== null) return { kind: 'slower', ms: Number(slower[1]) };

    const tag = /^tag:(.+)$/i.exec(text);

    if (tag !== null) return { kind: 'tag', value: (tag[1] ?? '').toLowerCase() };

    return { kind: 'text', value: text.toLowerCase() };
  };

  const passes = (row: Matchable): boolean => {
    const term = parseFilter();

    switch (term.kind) {
      case 'none':
        return true;
      case 'text':
        return row.text.toLowerCase().includes(term.value);
      case 'status':
        return row.status !== undefined && Math.floor(row.status / 100) === term.klass;
      case 'slower':
        return row.ms !== undefined && row.ms > term.ms;
      case 'tag':
        return (row.tags ?? []).some((one) => one.toLowerCase().includes(term.value));
    }
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

  /**
   * One cause and everything it went on to do.
   *
   * The log is already causal: every `invalidated`, `placed` and `fetch` entry
   * carries the `seq` of the mutation or frame batch responsible. Rendering it
   * as one reversed table throws that away and leaves you reading a timeline
   * backwards, matching sequence numbers by eye. This is the same data, shaped
   * the way it was recorded.
   */
  interface Block {
    readonly entry: LogEntry;
    readonly effects: LogEntry[];
  }

  /** The kinds that can head a block rather than sit inside one. */
  const isCause = (entry: LogEntry): boolean =>
    entry.kind === 'mutation' ||
    entry.kind === 'frames' ||
    entry.kind === 'action' ||
    entry.kind === 'principal';

  const trace = (): Block[] => {
    const blocks = new Map<number, Block>();
    const order: Block[] = [];
    /**
     * The cause each query's current request was attributed to.
     *
     * A `settle` carries no cause of its own and an `error` carries none
     * either, but both are the tail of a `fetch` that did. Without this they
     * would each become a block of their own and the refetch you are reading
     * would be split across three of them.
     */
    const owner = new Map<string, number>();

    const start = (entry: LogEntry): Block => {
      const made: Block = { entry, effects: [] };

      blocks.set(entry.seq, made);
      order.push(made);

      return made;
    };

    for (const entry of devtools.log()) {
      if (isCause(entry)) {
        start(entry);
        continue;
      }

      const named = 'query' in entry ? entry.query : undefined;
      const cause =
        'cause' in entry && entry.cause !== undefined
          ? entry.cause
          : named === undefined
            ? undefined
            : owner.get(named);

      if (named !== undefined && cause !== undefined) owner.set(named, cause);

      const target = cause === undefined ? undefined : blocks.get(cause);

      // An effect whose cause has been overwritten by the ring, and one that
      // never had a cause -- a mount fetch, an error on a query nothing
      // invalidated -- both stand on their own rather than being dropped.
      if (target === undefined) start(entry);
      else target.effects.push(entry);
    }

    return order;
  };

  /**
   * One pending optimistic write.
   *
   * Push order, bottom first, which is the order a fold applies them in and
   * therefore the only order in which a stack of overlapping patches makes
   * sense to read.
   */
  const layerBlock = (one: OverlaySnapshot): HTMLElement => {
    const node = el('div', 'layer');
    const head = el('div', 'cause-head');

    node.setAttribute('data-layer', String(one.id));
    head.append(el('span', 'seq', `#${String(one.id)}`));

    if (one.created !== undefined) head.append(el('span', 'kind', 'create'));
    if (one.places) head.append(el('span', 'kind', 'places'));

    node.append(head);

    for (const patch of one.patches) {
      const row = el('div', 'patch');

      row.append(el('span', `kind ${patch.kind}`, patch.kind));
      row.append(el('span', 'key', patch.key));
      node.append(row);
    }

    if (one.created !== undefined) {
      node.append(pills([one.created], 'minted, never promoted'));
    }

    node.append(pills(one.tags, 'raises when it settles'));

    for (const patch of one.patches) node.append(diffOf(patch.key));

    const act = el('div', 'buttons');
    const rollback = el('button', undefined, 'roll back');

    rollback.setAttribute('data-act', 'rollback');
    rollback.addEventListener('click', (event) => {
      event.stopPropagation();
      devtools.actions.rollback(one.id);
      render();
    });
    const promote = el('button', undefined, 'promote');

    promote.setAttribute('data-act', 'promote');
    promote.addEventListener('click', (event) => {
      event.stopPropagation();
      devtools.actions.promote(one.id);
      render();
    });
    act.append(rollback, promote);
    node.append(act);

    return node;
  };

  /**
   * The base record beside what the stack makes of it.
   *
   * `store.getRecord` is the base and `overlays.effective` is the fold, so this
   * is the actual before and after rather than a re-derivation of the patch.
   * Only changed fields are listed; a record with forty fields and one edit
   * should read as one edit.
   */
  const diffOf = (key: string): HTMLElement => {
    const node = el('div', 'diff');
    const base = devtools.baseRecord(key);
    const folded = devtools.foldedRecord(key);

    node.append(el('div', 'same', key));

    if (folded === undefined) {
      node.append(el('div', 'was', 'deleted by this overlay'));

      return node;
    }

    if (base === undefined) {
      node.append(el('div', 'now', 'created by this overlay, no base record'));

      return node;
    }

    let changed = 0;

    for (const [field, next] of Object.entries(folded)) {
      const before = base[field];

      if (Object.is(before, next)) continue;

      changed += 1;
      node.append(el('div', 'was', `- ${field}: ${String(before)}`));
      node.append(el('div', 'now', `+ ${field}: ${String(next)}`));
    }

    if (changed === 0) node.append(el('div', 'same', 'no field actually moves'));

    return node;
  };

  /** What a block reads as, for the filter box. */
  const blockText = (one: Block): string =>
    [describe(one.entry), ...one.effects.map(describe)].join(' ');

  /** The word in the eyebrow. `action` is rendered as what it is: you. */
  const kindOf = (entry: LogEntry): string =>
    entry.kind === 'action' ? 'you' : entry.kind === 'frames' ? 'frames' : entry.kind;

  /** The tags a cause raised, when it is the sort of thing that raises tags. */
  const raisedBy = (entry: LogEntry): readonly string[] =>
    entry.kind === 'mutation' || entry.kind === 'frames' ? entry.tags : [];

  const causeBlock = (one: Block): HTMLElement => {
    const node = el('div', 'cause');
    const head = el('div', 'cause-head');

    node.setAttribute('data-kind', one.entry.kind);
    head.append(el('span', 'seq', `#${String(one.entry.seq)}`));
    head.append(el('span', 'kind', kindOf(one.entry)));
    head.append(el('span', 'op', describe(one.entry)));
    node.append(head);

    const raised = raisedBy(one.entry);

    if (raised.length > 0) node.append(pills(raised, 'raised'));

    // The single most common cause of an invalidation that silently did not
    // happen, and invisible without this.
    if (one.entry.kind === 'mutation' && one.entry.unresolved.length > 0) {
      node.append(pills(one.entry.unresolved, 'skipped, resolved to nothing'));
    }

    for (const effect of one.effects) node.append(el('div', 'effect', describe(effect)));

    for (const miss of missedBy(one)) node.append(missBanner(miss));

    return node;
  };

  /**
   * The near misses worth interrupting you about, for one cause.
   *
   * Flagged per raised tag, not per cause. A tag that reached no mounted query
   * and closely resembles one somebody carries gets a banner even when the same
   * cause reached something through a different tag, because the tag that
   * missed is still wrong and the one that landed is what hides it. Suppressing
   * on "the cause reached something" would silence exactly the case this is
   * for: a mutation raising two tags where only one of them works.
   *
   * A tag that reached nothing and resembles nothing is not flagged. That is an
   * invalidation for a query nobody has open, which is ordinary.
   */
  const missedBy = (one: Block): readonly NearMiss[] => {
    const raised = raisedBy(one.entry);

    if (raised.length === 0) return [];

    const landed = new Set<string>();

    for (const effect of one.effects) {
      if (effect.kind === 'invalidated') for (const tag of effect.matched) landed.add(tag);
    }

    const missed = raised.filter((tag) => !landed.has(tag));

    if (missed.length === 0) return [];

    // What anything currently carries, mounted or merely remembered: a query
    // that is only remembered still refetches on mount, so a near miss against
    // it is just as much a near miss.
    const carried = devtools.tags().map((row) => row.tag);

    return nearMisses(missed, carried, 3);
  };

  const missBanner = (miss: NearMiss): HTMLElement => {
    const node = el('div', 'nearmiss');

    node.append(el('span', 'warn', 'near miss'));
    node.append(
      el(
        'span',
        undefined,
        ` ${miss.invalidated} reached nothing, and ${miss.carried} is carried ` +
          `(${miss.relation}). ${miss.hint}`,
      ),
    );

    return node;
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
        const source = devtools
          .queries()
          .filter((entry) => passes({ text: entry.key, tags: entry.tags }));

        body.append(chipBar(source, QUERY_FACETS));

        const items = narrow(source, QUERY_FACETS).map((entry) => ({
            key: entry.key,
            cells: [
              entry.key,
              String(entry.mounts),
              holds.get(entry.key) ?? (entry.stale ? 'stale' : entry.settled ? 'fresh' : 'empty'),
              entry.tags.join(' '),
            ],
            ...(holds.has(entry.key) ? { extra: el('span', 'held', ' held') } : {}),
          }));

        body.append(rows(['key', 'mounts', 'state', 'tags'], items));
        break;
      }

      case 'entities': {
        const source = devtools
          .entities({ limit: 300 })
          .filter((record) => passes({ text: record.key }));

        body.append(chipBar(source, ENTITY_FACETS));

        const items = narrow(source, ENTITY_FACETS).map((record) => ({
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
        const tagSource = devtools.tags().filter((row) => passes({ text: row.tag, tags: [row.tag] }));

        body.append(chipBar(tagSource, TAG_FACETS));

        const tagRows = narrow(tagSource, TAG_FACETS).map((row) => [
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

      case 'network': {
        if (!devtools.watchingRequests) {
          body.append(
            el(
              'p',
              'dim',
              'Nothing is recording requests. Import RequestLog from ' +
                '@forge-go/client-devtools/requests, pass log.observer as the observer option ' +
                'on RestTransport, and pass the log to attach({ requests: log }). The seam is ' +
                'at the transport because that is the only place the retries and the credential ' +
                'refresh are visible: they happen inside one execute, so nothing wrapped around ' +
                'the transport can see them.',
            ),
          );

          break;
        }

        if (devtools.requestsDropped > 0) {
          body.append(
            el(
              'p',
              'dim',
              `${String(devtools.requestsDropped)} earlier request(s) have been overwritten.`,
            ),
          );
        }

        const requestSource = devtools
          .requests()
          .filter((one) =>
            passes({ text: `${one.operation} ${one.args}`, status: one.status, ms: one.duration }),
          );

        body.append(chipBar(requestSource, REQUEST_FACETS));

        const requests = narrow(requestSource, REQUEST_FACETS).slice().reverse();

        const slowest = requestSource.reduce((most, one) => Math.max(most, one.duration ?? 0), 1);

        body.append(
          rows(
            ['request', 'status', 'try', 'ms', ''],
            requests.map((one) => ({
              key: String(one.id),
              cells: [
                `${one.operation} ${one.args}`,
                one.outcome === 'pending'
                  ? 'pending'
                  : one.outcome === 'ok'
                    ? String(one.status ?? 'ok')
                    : String(one.status ?? 'failed'),
                `${String(one.attempts)}/${String(one.limit)}`,
                one.duration === undefined ? '' : String(one.duration),
                '',
              ],
              extra: waterfall(one, slowest),
            })),
          ),
        );

        break;
      }

      case 'overlay': {
        const layerSource = devtools
          .overlays()
          .filter((one) =>
            passes({
              text: [...one.patches.map((patch) => patch.key), ...one.tags].join(' '),
              tags: one.tags,
            }),
          );

        body.append(chipBar(layerSource, OVERLAY_FACETS));

        const layers = narrow(layerSource, OVERLAY_FACETS);

        if (layers.length === 0) {
          body.append(
            el(
              'p',
              'dim',
              'No optimistic write is pending. Anything here has been folded over the store ' +
                'but not written to it, and rolling one back is removing it rather than ' +
                'applying an inverse.',
            ),
          );

          break;
        }

        for (const one of layers) body.append(layerBlock(one));

        break;
      }

      case 'trace': {
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

        const blocks = trace()
          .filter((one) => matches(blockText(one)))
          .slice(-200)
          .reverse();

        if (blocks.length === 0) body.append(el('p', 'dim', 'nothing here'));
        else for (const one of blocks) body.append(causeBlock(one));

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
   * Every value reaching it is bounded first, and there are three: a query's
   * last settled response, capped by `capped` in `inspect.ts`; a captured
   * frame's payload, capped by `capture` in `frames.ts`; and an entity's
   * fields, which `entity()` copies one level only, so the entity pane runs
   * them through `capture` itself before handing them over. All three go
   * through the same walker and stop at the same depth, which is why this
   * needs no depth guard of its own and cannot be given a cycle.
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
   * A row of buttons, each of which runs one `devtools.actions` call.
   *
   * Three write sites in this file, and these are two of them: the query bar
   * and the entity bar, both built from here. The third is the `clear cache`
   * button in `render()`. Everything else in this file reads.
   */
  const buttonBar = (buttons: readonly (readonly [string, () => void])[]): HTMLElement => {
    const node = el('div', 'bar');

    for (const [label, run] of buttons) {
      const button = el('button', undefined, label);

      button.addEventListener('click', () => {
        run();
        render();
      });
      node.append(button);
    }

    return node;
  };

  /**
   * The query actions.
   *
   * The refetch rejection is swallowed on purpose: a failing refetch is a
   * normal thing to be looking at, and an unhandled rejection raised by the
   * panel would be reported as though the application had one.
   */
  const actionBar = (detail: QueryDetail): HTMLElement =>
    buttonBar([
      [
        'refetch',
        () => {
          void devtools.actions.refetch(detail.key).catch(() => undefined);
        },
      ],
      [
        'invalidate',
        () => {
          devtools.actions.invalidate(detail.key);
        },
      ],
      [
        'drop',
        () => {
          devtools.actions.drop(detail.key);
        },
      ],
    ]);

  /**
   * The entity actions, which are one.
   *
   * `evict` has no other way in, and it had no way in at all until this pane
   * existed. Of the action layer's six calls the panel now wires five:
   * `refetch`, `invalidate` and `drop` hang off a query, `evict` off an
   * entity, `clear` off the global bar. `invalidateTag` is the one left to the
   * console, because it takes a tag rather than a row.
   */
  /**
   * The two forced states, and the one rule that makes them safe.
   *
   * A faked state has to be impossible to mistake for a real one and reversible
   * without the cache having to unwind anything. Holding it in a register the
   * panel owns satisfies both: the row says held, the pane says held, the trace
   * says you did it, and release is a map delete.
   */
  /**
   * Straight to the explanation for this query.
   *
   * `explain` used to need an exact query key typed from memory into the
   * filter box, which is a strange thing to ask of the one tab that exists to
   * answer a question you already have.
   */
  const whyBar = (key: string): HTMLElement => {
    const bar = el('div', 'buttons');
    const why = el('button', undefined, 'why did this not refetch?');

    why.setAttribute('data-act', 'why');
    why.addEventListener('click', () => {
      filter = key;
      tab = 'explain';
      render();
    });
    bar.append(why);

    return bar;
  };

  const holdBar = (key: string): HTMLElement => {
    const bar = el('div', 'buttons');

    const stale = el('button', undefined, 'force stale');

    stale.setAttribute('data-act', 'force-stale');
    stale.addEventListener('click', () => {
      devtools.actions.forceStale(key);
      render();
    });
    bar.append(stale);

    for (const state of ['loading', 'error'] as const) {
      const button = el('button', undefined, `hold ${state}`);

      button.setAttribute('data-act', `hold-${state}`);
      button.addEventListener('click', () => {
        holds.set(key, state);
        devtools.actions.hold(key, state);
        render();
      });
      bar.append(button);
    }

    return bar;
  };

  /**
   * Edit one field, as an overlay entry.
   *
   * The argument the whole state-manipulation section rests on: a devtools
   * edit rides the same stack a pending mutation does, so it never writes the
   * base store, undo is removing the entry rather than applying an inverse,
   * and a stream frame that evicts the row takes this with it exactly as it
   * would take a real optimistic write. A merge over a base record that is
   * gone patches nothing, and that rule is already in `OverlayStack`.
   */
  const editBar = (key: string): HTMLElement => {
    const bar = el('div', 'buttons');
    const field = doc.createElement('input');
    const value = doc.createElement('input');

    field.setAttribute('data-act', 'edit-field');
    field.placeholder = 'field';
    field.setAttribute('aria-label', 'Field to edit');
    value.setAttribute('data-act', 'edit-value');
    value.placeholder = 'value';
    value.setAttribute('aria-label', 'New value');

    const apply = el('button', undefined, 'patch');

    apply.setAttribute('data-act', 'edit-apply');
    apply.addEventListener('click', () => {
      const name = field.value.trim();

      if (name === '') return;

      // JSON first so a number stays a number, falling back to the raw string
      // for the common case of typing a word rather than a quoted one.
      let parsed: unknown = value.value;

      try {
        parsed = JSON.parse(value.value) as unknown;
      } catch {
        parsed = value.value;
      }

      devtools.patchEntity(key, { [name]: parsed });
      render();
    });

    bar.append(field, value, apply);

    return bar;
  };

  const entityBar = (key: string): HTMLElement =>
    buttonBar([
      [
        'evict',
        () => {
          devtools.actions.evict(key);
        },
      ],
    ]);

  const renderQueryDetail = (body: HTMLElement, key: string): void => {
    const detail = devtools.detail(key);

    if (detail === undefined) {
      body.append(el('p', 'dim', `${key} is no longer tracked.`));

      return;
    }

    const held = holds.get(key);

    if (held !== undefined) {
      const banner = el('div', 'held-banner');

      banner.append(
        el(
          'div',
          undefined,
          `Held in ${held} by devtools. The real record is untouched in the cache ` +
            'and comes back the moment you release.',
        ),
      );

      const release = el('button', undefined, 'release');

      release.setAttribute('data-act', 'release');
      release.addEventListener('click', () => {
        holds.delete(key);
        devtools.actions.release(key);
        render();
      });
      banner.append(release);
      body.append(banner);
    }

    body.append(el('h4', undefined, detail.key));
    body.append(actionBar(detail));
    body.append(holdBar(key));
    body.append(whyBar(key));
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

  /**
   * The other detail pane: what the store holds for one entity, and who
   * reaches it.
   *
   * The entities tab has always rendered clickable rows, and clicking one
   * always went to `devtools.detail()`, which is a registry lookup. `Order:1`
   * is not a query key, so the answer was `undefined` and the pane said the
   * record was no longer tracked while the row for it sat on screen. This is
   * the branch that was missing.
   *
   * `dependents()` rather than the `dependents` field `entity()` already
   * carries: the field is a list of keys, and this is a list of queries.
   * Whether each one is currently mounted is the thing you want when an
   * entity moved and a screen did not.
   */
  const renderEntityDetail = (body: HTMLElement, key: string): void => {
    const record: EntitySnapshot | undefined = devtools.entity(key);

    if (record !== undefined) body.append(editBar(key));

    if (record === undefined) {
      body.append(el('p', 'dim', `${key} is not in the store.`));

      return;
    }

    body.append(el('h4', undefined, record.key));
    body.append(entityBar(record.key));
    body.append(field('type', record.type));
    body.append(field('id', record.id));
    body.append(field('version', String(record.version)));
    body.append(
      field('frameAt', record.frameAt > 0 ? String(record.frameAt) : 'no frame has written it'),
    );
    body.append(pills(record.refs, 'refs'));

    // `entity()` copies the record one level, so a nested field is still the
    // store's own object and still unbounded. `capture` is what makes it
    // safe to hand to `explorer`, and what stops the pane from aliasing
    // anything the store holds.
    body.append(el('h4', undefined, 'fields'));
    body.append(explorer(capture(record.fields), 'fields'));

    body.append(el('h4', undefined, 'dependents'));
    body.append(
      table(
        ['query', 'mounts', 'state'],
        devtools
          .dependents(record.key)
          .map((entry) => [
            entry.key,
            String(entry.mounts),
            entry.stale ? 'stale' : entry.settled ? 'fresh' : 'empty',
          ]),
      ),
    );
  };

  /**
   * The inspector, which is built only when something is selected.
   *
   * Not rendered-but-empty: with no selection there is no `.detail` element at
   * all, and the list takes the whole panel. A permanently reserved column
   * spends 45% of the width on the sentence "Pick a query on the left", and a
   * query key is exactly the sort of long string that needs the room back.
   */
  const renderDetail = (body: HTMLElement, key: string): void => {
    const head = el('div', 'detail-head');
    const close = el('button', 'ico tip tip-r');

    head.append(
      el('span', 'what', tab === 'entities' ? 'entity' : tab === 'network' ? 'request' : 'query'),
    );
    head.append(el('div', 'spacer'));

    close.innerHTML = icon('<path d="M3.6 3.6l6.8 6.8M10.4 3.6l-6.8 6.8"/>');
    close.setAttribute('data-act', 'close-detail');
    close.setAttribute('data-tip', 'Close the inspector');
    close.setAttribute('aria-label', 'Close the inspector');
    close.addEventListener('click', () => {
      selected.delete(tab);
      render();
    });
    head.append(close);
    body.append(head);

    if (tab === 'entities') renderEntityDetail(body, key);
    else if (tab === 'network') renderRequestDetail(body, key);
    else renderQueryDetail(body, key);
  };

  /**
   * Why this request did what it did.
   *
   * The sentence at the top is the whole reason this view exists. A browser
   * network tab shows a failed `POST` and a failed `GET` identically -- one
   * request, one failure -- and cannot say that the first was never eligible
   * for a retry in the first place.
   */
  const renderRequestDetail = (body: HTMLElement, id: string): void => {
    const one = devtools.requests().find((entry) => String(entry.id) === id);

    if (one === undefined) {
      body.append(el('p', 'dim', 'That request has been overwritten by newer traffic.'));

      return;
    }

    body.append(el('h4', undefined, one.operation));

    if (one.args !== '') body.append(el('p', 'dim', one.args));

    body.append(field('outcome', one.outcome));
    body.append(field('status', one.status === undefined ? 'none' : String(one.status)));
    body.append(field('attempts', `${String(one.attempts)} of ${String(one.limit)} allowed`));
    body.append(
      field('duration', one.duration === undefined ? 'still in flight' : `${String(one.duration)}`),
    );

    const curl = el('pre', 'curl');

    curl.setAttribute('data-curl', '');
    curl.textContent =
      `curl -X ${one.method} '${one.operation.slice(one.method.length + 1)}' \\\n` +
      `  -H 'authorization: Bearer $TOKEN' \\\n` +
      `  -H 'accept: application/json'${one.args === '' || one.args === '{}' ? '' : ` \\\n  -d '${one.args}'`}`;
    body.append(el('h4', undefined, 'as curl'));
    body.append(curl);
    body.append(
      el(
        'p',
        'dim',
        'The credential is a placeholder. This log never keeps the real one, ' +
          'which is why it is cheap enough to leave on.',
      ),
    );

    body.append(el('h4', undefined, 'retry policy'));
    body.append(el('p', one.outcome === 'failed' ? 'warn' : 'dim', retryStory(one)));

    if (one.retries.length > 0) {
      const list = doc.createElement('ul');

      for (const retry of one.retries) {
        list.append(
          el(
            'li',
            undefined,
            `attempt ${String(retry.attempt + 1)} failed with ` +
              `${retry.status === undefined ? 'no status' : String(retry.status)}, ` +
              `backed off ${String(Math.round(retry.delay))}ms`,
          ),
        );
      }

      body.append(list);
    }

    if (one.refreshes > 0) {
      body.append(el('h4', undefined, 'credentials'));
      body.append(
        el(
          'p',
          'dim',
          one.joined
            ? `Hit a 401 and waited on a refresh another request had already started. ` +
              `That is the single flight: one refresh, however many requests stampede it.`
            : `Hit a 401 and started the credential refresh. Any other request that 401'd ` +
              `while it ran waited on this one rather than asking for its own.`,
        ),
      );
    }
  };

  /**
   * The one sentence to read first.
   *
   * `limit` is what makes this answerable: a budget that went unused says the
   * status was the reason, and a budget of one says the method was.
   */
  const retryStory = (one: RequestSnapshot): string => {
    if (one.outcome === 'pending') return 'Still in flight.';
    if (one.outcome === 'ok') {
      return one.attempts > 1
        ? `Retried ${String(one.attempts - 1)} time(s) and succeeded.`
        : 'Succeeded first time, so the policy never came into it.';
    }

    if (one.attempts < one.limit) {
      return (
        `Not retried: ${one.status === undefined ? 'that failure' : String(one.status)} is not ` +
        `a status this policy retries. It allows 408 and 429 and no other 4xx, because a 4xx is ` +
        `the server saying the request is wrong and repeating it gets the same answer. ` +
        `${String(one.limit - one.attempts)} attempt(s) went unused.`
      );
    }

    if (one.limit === 1) {
      return (
        `Not retried: ${one.method} is not idempotent, so a retry was never on the table. ` +
        `The client cannot tell a request the server never saw from one it processed and ` +
        `failed to acknowledge, and only the idempotent methods make that difference safe.`
      );
    }

    return `Gave up after ${String(one.attempts)} attempts, which is the whole budget.`;
  };

  /**
   * The closed state: the mark, a status ring and an error count.
   *
   * Both readings come from `records()`, which carries the scalar fields for
   * every tracked query and no response bodies, so drawing the button stays
   * O(tracked queries) and never O(bytes in the cache).
   */
  const launcher = (): HTMLElement => {
    const button = el('button', 'launcher');
    let fetching = false;
    let errors = 0;

    for (const record of devtools.records()) {
      if (record.fetching) fetching = true;
      if (record.status === 'error') errors += 1;
    }

    const counts = devtools.store();
    const pending = devtools.overlays().length;

    button.innerHTML = MARK;
    button.setAttribute('aria-label', 'Open Forge devtools');
    button.setAttribute('data-pulse', pulse(fetching, errors, pending));

    if (errors > 0) button.append(el('span', 'badge', String(errors)));

    button.addEventListener('click', () => {
      open = true;
      render();
    });

    // The three numbers a glance should answer, revealed on hover so the
    // resting state is still one 30px tile. `pend` appears only when there is
    // something pending: a permanent `0 pend` teaches you to stop reading it.
    const vitals = el('div', 'launcher-vitals');

    vitals.append(el('b', undefined, String(counts.records)), el('span', undefined, 'ent'));
    vitals.append(el('b', undefined, String(counts.mounted)), el('span', undefined, 'mnt'));

    if (pending > 0) {
      vitals.append(el('b', undefined, String(pending)), el('span', undefined, 'pend'));
    }

    const dock = el('div', 'launcher-dock');

    dock.append(button, vitals);

    return dock;
  };

  /**
   * The facets of each tab.
   *
   * OR within a tab, then AND with the text box. Two chips on means "either of
   * these", which is what you want when you press `stale` and then `error`;
   * ANDing them would silently empty the list and read as a bug.
   *
   * Typed per tab rather than over a common row shape, because the useful
   * question differs: entities have no status and requests have no mounts, and
   * a shared shape would flatten both into a string nobody can filter on.
   */
  interface Facet<T> {
    readonly id: string;
    readonly label: string;
    test(row: T): boolean;
  }

  const QUERY_FACETS: readonly Facet<QuerySnapshot>[] = [
    { id: 'mounted', label: 'mounted', test: (row) => row.mounts > 0 },
    { id: 'unmounted', label: 'unmounted', test: (row) => row.mounts === 0 },
    { id: 'stale', label: 'stale', test: (row) => row.stale },
    { id: 'empty', label: 'never settled', test: (row) => !row.settled },
  ];

  const ENTITY_FACETS: readonly Facet<EntitySnapshot>[] = [
    { id: 'framed', label: 'written by a frame', test: (row) => row.frameAt > 0 },
    { id: 'linked', label: 'points at something', test: (row) => row.refs.length > 0 },
  ];

  const TAG_FACETS: readonly Facet<TagSnapshot>[] = [
    { id: 'orphan', label: 'carried by nobody', test: (row) => row.carriers.length === 0 },
    { id: 'unmounted', label: 'no mounted carrier', test: (row) => row.mounted.length === 0 },
  ];

  const REQUEST_FACETS: readonly Facet<RequestSnapshot>[] = [
    { id: 'failed', label: 'failed', test: (row) => row.outcome === 'failed' },
    { id: 'pending', label: 'in flight', test: (row) => row.outcome === 'pending' },
    { id: 'retried', label: 'retried', test: (row) => row.attempts > 1 },
    { id: 'refreshed', label: 'hit a refresh', test: (row) => row.refreshes > 0 },
  ];

  const OVERLAY_FACETS: readonly Facet<OverlaySnapshot>[] = [
    { id: 'creates', label: 'creates a record', test: (row) => row.created !== undefined },
    { id: 'places', label: 'has placement', test: (row) => row.places },
  ];

  /** The ids switched on for this tab. Per tab, like the sort and the selection. */
  const chosen = (): Set<string> => {
    const found = facets.get(tab);

    if (found !== undefined) return found;

    const made = new Set<string>();

    facets.set(tab, made);

    return made;
  };

  /** OR within the tab. No chips on means no narrowing at all. */
  const narrow = <T,>(source: readonly T[], all: readonly Facet<T>[]): readonly T[] => {
    const on = all.filter((one) => chosen().has(one.id));

    return on.length === 0 ? source : source.filter((row) => on.some((one) => one.test(row)));
  };

  /**
   * The chips, each carrying what pressing it would leave behind.
   *
   * The count is measured before the chips are applied and after the text box
   * is, so it answers "how many of what I am looking at" rather than "how many
   * exist", and pressing a chip that reads 0 is a thing you can decide not to
   * do.
   */
  const chipBar = <T,>(source: readonly T[], all: readonly Facet<T>[]): HTMLElement => {
    const bar = el('div', 'facets');
    const on = chosen();

    for (const one of all) {
      const button = el('button', 'facet');

      button.append(el('span', undefined, one.label));
      button.append(el('span', 'count', String(source.filter((row) => one.test(row)).length)));
      button.setAttribute('data-facet', one.id);
      button.setAttribute('aria-pressed', String(on.has(one.id)));
      button.addEventListener('click', () => {
        if (on.has(one.id)) on.delete(one.id);
        else on.add(one.id);

        render();
      });
      bar.append(button);
    }

    if (on.size > 0 || filter !== '') {
      const clear = el('button', 'facet clear', 'clear');

      clear.setAttribute('data-act', 'clear-filters');
      clear.addEventListener('click', () => {
        on.clear();
        filter = '';
        render();
      });
      bar.append(clear);
    }

    return bar;
  };

  /**
   * The counters that say whether anything is leaking.
   *
   * `StoreSnapshot` carries nine of these and the old header showed three of
   * them as a sentence. Tombstones and stamped tags are both bounded caches, so
   * a number that keeps climbing is the shape of a leak, and neither was
   * visible anywhere before this.
   *
   * `pending` appears only when the overlay stack is non-empty. A permanent
   * `0 pending` teaches you to stop reading the strip.
   */
  const vitalsStrip = (): HTMLElement => {
    const counts = devtools.store();
    const pending = devtools.overlays().length;
    const strip = el('div', 'vitals');

    const cell = (key: string, label: string, value: string, note?: string): void => {
      const node = el('div', 'vital');

      node.setAttribute('data-vital', key);
      node.append(el('span', 'k', label));
      node.append(el('span', 'v', value));

      if (note !== undefined) node.append(el('span', 'n', note));

      strip.append(node);
    };

    cell('entities', 'entities', String(counts.records), `v${String(counts.version)}`);
    cell('mounted', 'mounted', String(counts.mounted), `of ${String(counts.remembered)}`);
    cell('store', 'store', `v${String(counts.version)}`, `f${String(counts.frameVersion)}`);

    if (pending > 0) cell('pending', 'pending', String(pending), 'writes');

    cell('tags', 'tags', String(counts.indexedTags), `${String(counts.stampedTags)} stamped`);
    cell('tombstones', 'tombstones', String(counts.tombstones));
    cell('tracked', 'tracked', String(counts.tracked));

    return strip;
  };

  /**
   * One request, drawn to scale against the slowest one on screen.
   *
   * Two segments, and only two, because only two are measured. The transport
   * reports attempt boundaries and the backoff between them, so the wire time
   * and the waiting are real numbers. It does not report when a response
   * started decoding or when the store committed it, and a bar that split
   * those out would be drawing a shape nobody measured.
   */
  const waterfall = (one: RequestSnapshot, slowest: number): HTMLElement => {
    const bar = el('div', 'wf');

    if (one.duration === undefined) {
      bar.append(el('i', 'pending'));

      return bar;
    }

    const waited = one.retries.reduce((total, retry) => total + retry.delay, 0);
    const wire = Math.max(0, one.duration - waited - one.authMs);
    const scale = (ms: number): string => `${String((ms / slowest) * 100)}%`;

    if (one.authMs > 0) {
      const auth = el('i', 'auth');

      auth.style.width = scale(one.authMs);
      auth.title = `${String(Math.round(one.authMs))}ms waiting on the credential refresh`;
      bar.append(auth);
    }

    const sent = el('i', 'wire');

    sent.style.width = scale(wire);
    sent.title = `${String(Math.round(wire))}ms on the wire`;
    bar.append(sent);

    if (waited > 0) {
      const held = el('i', 'backoff');

      held.style.width = scale(waited);
      held.title = `${String(Math.round(waited))}ms backing off across ${String(
        one.retries.length,
      )} retry(s)`;
      bar.append(held);
    }

    return bar;
  };

  /** One icon button on the rail. Icon only, so the tooltip carries the name. */
  const railButton = (
    paths: string,
    tip: string,
    pressed: boolean,
    onPress: () => void,
  ): HTMLElement => {
    const button = el('button', 'ico tip');

    button.innerHTML = icon(paths);
    button.setAttribute('data-tip', tip);
    button.setAttribute('aria-label', tip);
    button.setAttribute('aria-pressed', String(pressed));
    button.addEventListener('click', onPress);

    return button;
  };

  /**
   * The conditions and the toggles, or nothing at all.
   *
   * Rendered only for what the application actually wired. A rail of switches
   * that silently do nothing is worse than no rail: you would spend the
   * afternoon wondering why going offline changed no behaviour.
   */
  const controlRail = (): HTMLElement | undefined => {
    const controls = devtools.controls;
    const revalidation = devtools.revalidation;
    const sources: RevalidationSource[] = (['focus', 'reconnect', 'poll'] as const).filter(
      (name) => revalidation?.registered(name) === true,
    );

    if (controls === undefined && sources.length === 0) return undefined;

    const rail = el('div', 'rail');

    if (controls !== undefined) {
      const group = el('div', 'docks');

      for (const [name, tip, paths] of NETWORK) {
        const button = railButton(paths, tip, controls.mode === name, () => {
          controls.mode = name;
          render();
        });

        button.setAttribute('data-net', name);
        group.append(button);
      }

      rail.append(el('span', 'rail-label', 'network'), group);

      const latency = doc.createElement('input');

      latency.type = 'range';
      latency.min = '0';
      latency.max = '3000';
      latency.step = '50';
      latency.value = String(controls.latency);
      latency.className = 'latency';
      latency.setAttribute('data-act', 'latency');
      latency.setAttribute('aria-label', 'Injected latency in milliseconds');
      latency.addEventListener('input', () => {
        controls.latency = Number(latency.value);
        readout.textContent = `${latency.value}ms`;
      });

      const readout = el('span', 'rail-label', `${String(controls.latency)}ms`);

      rail.append(el('span', 'rail-label', 'latency'), latency, readout);

      // Pressing it again gives up on the armed failure rather than arming a
      // second one, which there is no such thing as.
      const fail = railButton(ICONS.zap, 'Fail the next request', controls.armed, () => {
        if (controls.armed) controls.disarm();
        else controls.failNext();

        render();
      });

      fail.setAttribute('data-act', 'fail-next');
      rail.append(fail);
    }

    if (sources.length > 0) {
      const group = el('div', 'docks');

      for (const name of sources) {
        const button = railButton(
          REVALIDATE[name][1],
          REVALIDATE[name][0],
          revalidation?.enabled(name) === true,
          () => {
            revalidation?.toggle(name);
            render();
          },
        );

        button.setAttribute('data-reval', name);
        group.append(button);
      }

      rail.append(el('span', 'rail-label', 'revalidate'), group);
    }

    rail.append(el('div', 'spacer'));

    // Freezing the *view*, and named that way on purpose. The panel can
    // honestly stop repainting; it cannot stop the cache committing, which
    // would need a seam in the cache rather than in here.
    const freeze = railButton(ICONS.snow, 'Freeze the view', frozen, () => {
      frozen = !frozen;
      render();
    });

    freeze.setAttribute('data-act', 'freeze');
    rail.append(freeze);

    return rail;
  };

  /**
   * What the ring says, most explanatory first.
   *
   * Offline outranks the errors it causes. Every request fails while the rail
   * is offline, and a red ring over failures you switched on yourself sends
   * you debugging a request that was never sent. The error *count* is
   * unaffected and stays on the badge: only the explanation changes.
   *
   * Slow ranks below both, because unlike offline it does not prevent anything
   * from succeeding, so a real error or a request in flight is the more useful
   * thing to be told about while it is on.
   */
  const pulse = (fetching: boolean, errors: number, pending: number): string => {
    const mode = devtools.controls?.mode;

    if (mode === 'offline') return 'offline';
    if (errors > 0) return 'error';
    // A stuck optimistic write is a bug you want to see. A request in flight
    // is Tuesday. So pending outranks fetching, and an error outranks both.
    if (pending > 0) return 'pending';
    if (fetching) return 'fetching';
    if (mode === 'slow') return 'slow';

    return 'idle';
  };

  /** The four dock modes, as one segmented control. */
  const dockBar = (): HTMLElement => {
    const group = el('div', 'docks');

    for (const [name, tip, paths] of DOCKS) {
      const button = el('button', 'ico tip');

      button.innerHTML = icon(paths);
      button.setAttribute('data-dock', name);
      button.setAttribute('data-tip', tip);
      button.setAttribute('aria-label', tip);
      button.setAttribute('aria-pressed', String(name === mode));
      button.addEventListener('click', () => {
        mode = name;
        render();
      });
      group.append(button);
    }

    return group;
  };

  const render = (): void => {
    root.replaceChildren();

    if (!open) {
      root.append(launcher());

      return;
    }

    const panel = el('div', 'panel');

    panel.setAttribute('data-mode', mode);
    panel.setAttribute('data-density', compact ? 'compact' : 'comfortable');

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
    // The grammar is only discoverable from here, so the placeholder teaches
    // it rather than saying "filter" and leaving you to guess.
    search.placeholder =
      tab === 'explain' ? 'query key, then Enter' : 'filter, or status:4xx  >100ms  tag:Order[]';
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
      // Every tab's selection, not just this one: `clear()` drops every entity,
      // every skeleton and every registry entry, so no key held anywhere here
      // still names something.
      selected.clear();
      render();
    });
    bar.append(clearCache);

    // Named for both halves when there is a second half. `devtools.clear()`
    // empties the frame ring along with the event log, and losing a capture to
    // a button labelled about the log, while standing on the frames tab, is
    // exactly the sort of thing you only notice once the frames are gone.
    const clearLog = el(
      'button',
      undefined,
      devtools.capturing ? 'clear log + frames' : 'clear log',
    );

    clearLog.addEventListener('click', () => {
      devtools.clear();
      render();
    });
    bar.append(clearLog);
    bar.append(dockBar());

    const density = el('button', 'ico tip');

    density.innerHTML = icon('<path d="M2.5 4h9M2.5 7h9M2.5 10h9"/>');
    density.setAttribute('data-act', 'density');
    density.setAttribute('data-tip', 'Compact rows');
    density.setAttribute('aria-label', 'Compact rows');
    density.setAttribute('aria-pressed', String(compact));
    density.addEventListener('click', () => {
      compact = !compact;
      render();
    });
    bar.append(density);

    const close = el('button', 'ico tip tip-r');

    close.innerHTML = icon('<path d="M3.6 3.6l6.8 6.8M10.4 3.6l-6.8 6.8"/>');
    close.setAttribute('data-tip', 'Close the panel');
    close.setAttribute('aria-label', 'Close the panel');
    close.addEventListener('click', () => {
      open = false;
      render();
    });
    bar.append(close);

    const rail = controlRail();
    const vitals = vitalsStrip();
    const split = el('div', 'split');
    const list = el('div', 'list');
    const key = selected.get(tab);

    renderList(list);
    split.append(list);

    if (key !== undefined) {
      const detail = el('div', 'detail');

      renderDetail(detail, key);
      split.append(detail);
    }

    panel.append(bar, vitals);

    if (rail !== undefined) panel.append(rail);

    panel.append(split);
    root.append(panel);
  };

  const schedule = (): void => {
    // Frozen holds the view still. The cache carries on committing and the
    // next repaint after release shows where it got to, which is the point: a
    // channel at twelve frames a second otherwise scrolls the thing you are
    // reading off the screen while you read it.
    //
    // Note this does *not* bail when closed. The launcher carries a status
    // ring, and a ring drawn once when the panel mounted is decoration rather
    // than status. Redrawing one button is cheap and is already coalesced to
    // one animation frame, which is the whole budget this costs.
    if (scheduled || frozen) return;

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

  /**
   * The shortcuts, on the document rather than on the panel.
   *
   * The panel lives in a shadow root and does not hold focus, so a listener
   * bound inside it would only fire once you had already clicked into it,
   * which is exactly when you do not need a shortcut.
   *
   * Nothing fires while you are typing. A devtools panel that eats a `3` out
   * of its own filter box is worse than one with no shortcuts at all, so any
   * event whose composed path contains an input or a textarea is left alone.
   */
  const typing = (event: KeyboardEvent): boolean => {
    for (const node of event.composedPath()) {
      const name = (node as Partial<Element>).tagName;

      if (name === 'INPUT' || name === 'TEXTAREA') return true;
    }

    return false;
  };

  const onKey = (event: KeyboardEvent): void => {
    if (typing(event)) return;

    const meta = event.metaKey || event.ctrlKey;

    // Toggling the panel is the only one that works while it is closed.
    if (meta && event.shiftKey && event.key.toLowerCase() === 'f') {
      event.preventDefault();
      open = !open;
      render();

      return;
    }

    // Jump to the filter box. The mock drew a command palette here; the filter
    // box already parses `status:4xx`, `>100ms` and `tag:`, so a palette would
    // be a second front door to the same room.
    if (open && ((meta && event.key.toLowerCase() === 'k') || (!meta && event.key === '/'))) {
      event.preventDefault();
      (root.querySelector('.bar input') as HTMLInputElement | null)?.focus();

      return;
    }

    // Pop the top of the overlay stack. Removal is the whole of rollback, so
    // undo here means what undo means everywhere else in this package.
    if (open && meta && event.key.toLowerCase() === 'z') {
      const stack = devtools.overlays();
      const top = stack[stack.length - 1];

      if (top === undefined) return;

      event.preventDefault();
      devtools.actions.rollback(top.id);
      render();

      return;
    }

    if (open && event.key === 'F11') {
      event.preventDefault();
      mode = mode === 'full' ? 'bottom' : 'full';
      render();

      return;
    }

    if (!open || meta || event.altKey) return;

    if (event.key === 'Enter') {
      const picked = selected.get(tab);

      if (picked === undefined) return;

      filter = picked;
      tab = 'explain';
      render();

      return;
    }

    if (event.key === 'Escape') {
      if (selected.get(tab) === undefined) return;

      selected.delete(tab);
      render();

      return;
    }

    const slot = Number(event.key);

    if (Number.isInteger(slot) && slot >= 1 && slot <= TABS.length) {
      tab = TABS[slot - 1] as Tab;
      render();

      return;
    }

    if (event.key.toLowerCase() === 'f') {
      frozen = !frozen;
      render();

      return;
    }

    // The two actions worth a key, and only for the row you have selected.
    const picked = selected.get(tab);

    if (picked !== undefined && (tab === 'queries' || tab === 'trace')) {
      if (event.key.toLowerCase() === 'r') {
        void devtools.actions.refetch(picked).catch(() => undefined);
        render();

        return;
      }

      if (event.key.toLowerCase() === 'i') {
        devtools.actions.invalidate(picked);
        render();

        return;
      }
    }

    if (event.key === ' ') {
      event.preventDefault();
      frozen = !frozen;
      render();
    }
  };

  doc.addEventListener('keydown', onKey);

  const unsubscribe = devtools.subscribe(schedule);

  render();

  return () => {
    unsubscribe();
    doc.removeEventListener('keydown', onKey);
    host.remove();
  };
}
