import { defineComponent } from 'vue';
import type { PropType, VNode } from 'vue';
import { hydrateBoundary } from '@forge-go/client-core';
import type { DehydratedState, OperationMeta, QueryCache } from '@forge-go/client-core';
import { useClient } from './context';

/**
 * Hydrate a payload into the cache this subtree reads from.
 *
 * ```vue
 * <HydrationBoundary :state="state" :ops="ops">
 *   <Orders />
 * </HydrationBoundary>
 * ```
 *
 * **It hydrates during render, not in `onMounted`.** The default slot is
 * evaluated inside the render function below, after the hydrate call, so the
 * children of this component read a cache that is already populated on their
 * very first render. A mounted hook runs after the tree has been created: the
 * first paint would be the loading branch and then flip, which is a visible
 * flash and, on the hydration pass, exactly the mismatch this component exists
 * to remove.
 *
 * It renders no element of its own. A wrapper would change the DOM that the
 * server and the client compare, in a component whose entire job is to make
 * those two agree.
 *
 * In the render function rather than in `setup` so that a `state` prop
 * arriving later, from a route resolve or a suspended parent, still hydrates.
 * Doing it repeatedly costs nothing: `hydrateBoundary` walks a given payload
 * once per cache.
 */
export const HydrationBoundary = defineComponent({
  name: 'ForgeHydrationBoundary',
  props: {
    /** The payload from `dehydrate`, after whatever transport carried it. */
    state: { type: Object as PropType<DehydratedState | undefined>, default: undefined },
    /** The generated `ops.ts` table, passed verbatim. */
    ops: { type: Object as PropType<Readonly<Record<string, OperationMeta>>>, required: true },
    /** Use this cache rather than the provided or configured one. */
    client: { type: Object as PropType<QueryCache | undefined>, default: undefined },
    /** Settle the hydrated queries behind the server, so mounting refetches. */
    stale: { type: Boolean, default: false },
  },
  setup(props, { slots }) {
    // Resolved here because `inject` only works during `setup`. The prop is
    // re-read on every render below, so an application that swaps caches gets
    // the new one; what it cannot do is start injecting a different provider
    // mid-life, which is true of every `inject` in Vue.
    const resolved = useClient(props.client);

    return (): VNode[] | undefined => {
      hydrateBoundary(props.client ?? resolved, props.state, {
        ops: props.ops,
        ...(props.stale ? { stale: true } : {}),
      });

      return slots.default?.();
    };
  },
});
