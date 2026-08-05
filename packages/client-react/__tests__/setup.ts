import { afterEach } from 'vitest';
import { cleanup } from '@testing-library/react';
import { setClient } from '@forge-go/client-core';

/**
 * React only permits `act()` when it is told it is in a test environment, and
 * it warns on every update that is not wrapped when it is not.
 */
declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

afterEach(() => {
  cleanup();
  // The module-level client is global state, and a test that configures one
  // and then leaks it turns the *next* test's "no client configured" assertion
  // into a pass for the wrong reason.
  setClient(undefined);
});
