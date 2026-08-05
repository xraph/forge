import { afterEach } from 'vitest';
import { enableAutoUnmount } from '@vue/test-utils';
import { setClient } from '@forge-go/client-core';

/**
 * Unmount whatever the test mounted, so a component that outlives its `it()`
 * cannot keep a subscription alive into the next one and turn a mount-count
 * assertion into a pass or a failure for the wrong reason.
 */
enableAutoUnmount(afterEach);

afterEach(() => {
  // The module-level client is global state, and a test that configures one
  // and then leaks it turns the *next* test's "no client configured" assertion
  // into a pass for the wrong reason.
  setClient(undefined);
});
