import { attach } from '../dist/index.js';
import { client, run } from './production';

/**
 * The same application, with the inspector statically imported.
 *
 * The treatment in the zero-cost experiment. Every marker this bundle contains
 * and `production.ts` does not is a byte the production build was proved not to
 * be paying for.
 */
export function boot(): void {
  const devtools = attach(client);

  run();
  devtools.whyNotRefetched('GET /orders');
  devtools.dispose();
}
