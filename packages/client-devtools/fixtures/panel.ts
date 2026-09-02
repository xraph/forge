import { attach } from '../dist/index.js';
import { mountPanel } from '../dist/panel.js';
import { client, run } from './production';

/**
 * The panel, imported statically. The control for the two assertions about it:
 * the production fixture must not contain its markers, and this one must.
 */
export function boot(): void {
  run();
  mountPanel(attach(client));
}
