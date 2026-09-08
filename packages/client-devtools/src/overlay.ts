/**
 * The devtools overlay: the full panel.
 *
 * `mountOverlay` is the import people reach for, so it is the one that has to
 * be the whole thing. It was the lean six-table view until 1.11, which meant
 * that following your instincts got you the least capable UI in the package
 * and no indication that a better one existed one subpath away.
 *
 * The lean view has not gone anywhere. It is `mountMini` from `./mini`, and it
 * is still six read-only tables in 3.5 kB.
 *
 * This module is an alias and nothing else: `/overlay` and `/panel` are the
 * same implementation, so an application already importing `/panel` sees no
 * change at all.
 */
export { mountPanel, mountPanel as mountOverlay } from './panel.js';
export type { PanelOptions, PanelOptions as OverlayOptions } from './panel.js';
