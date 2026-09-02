import { ForgeDevtools } from '@forge-go/client-react-devtools';
import { createElement } from 'react';
import { client, run } from './production';

/**
 * What an application now writes, in place of the guarded dynamic import.
 *
 * Bundled twice from this one file. Under the `development` condition the
 * package resolves to `dist/dev.js` and the markers are all present; with no
 * conditions it resolves to `dist/noop.js` through the `default` key, and the
 * bundle contains neither the devtools nor a request for them.
 */
export function App(): unknown {
  run();

  return createElement('div', null, createElement(ForgeDevtools, null));
}

export { client };
