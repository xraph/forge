import type { Devtools } from '@forge-go/client-devtools';
import type { ForgeDevtoolsProps } from './dev.js';

/**
 * What a production build resolves to.
 *
 * The `default` export condition points here, so a production bundle contains
 * this file and nothing else from this package: there is no devtools code in
 * it to fold away, because none was ever resolved. `dev.ts` additionally
 * guards its own imports, for bundlers that ignore export conditions.
 *
 * The import above is type-only and emits nothing.
 */
export function ForgeDevtools(_props: ForgeDevtoolsProps = {}): null {
  return null;
}

export function useForgeDevtools(): Devtools | undefined {
  return undefined;
}

export type { ForgeDevtoolsProps } from './dev.js';
