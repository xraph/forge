# @forge-go/client-angular

The Angular binding over [`@forge-go/client-core`](../client-core). Three bindings
and an optional provider, 1.44 kB gzipped.

Everything that decides *what* a value is — identity, staleness, deduplication,
invalidation — was decided in the core, where it is testable without a renderer.
What is left here is the narrow job of putting those values into the signal
graph without copying them on the way.

```ts
import { Component, ChangeDetectionStrategy, signal } from '@angular/core';
import { injectQuery, injectMutation } from '@forge-go/client-angular';
import { useOrderList, useOrderCreate } from './generated/hooks';

@Component({
  selector: 'app-orders',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (orders.status() === 'pending') { <app-spinner /> }
    @else {
      <button [disabled]="create.isPending()" (click)="create.mutate({ body: { total: 0 } })">
        New order
      </button>
      @if (create.status() === 'error') { <app-warning [error]="create.error()" /> }
      @for (order of orders.data() ?? []; track order.id) { <app-row [order]="order" /> }
    }
  `,
})
export class Orders {
  readonly filter = signal('open');
  readonly orders = injectQuery(useOrderList, () => ({ query: { status: this.filter() } }));
  readonly create = injectMutation(useOrderCreate);
}
```

## Why `injectQuery` and not `resource`

`inject*` is what Angular calls a function that must run in an injection
context, and this one does: it resolves the cache and the `DestroyRef` from the
injector. It is also the name the ecosystem already uses for exactly this shape.

Angular 19's `resource()` was the other candidate and was not taken. A
`resource` owns its request and re-runs a loader when its params change; this
owns nothing. The request, the cache entry, the tag graph and the invalidation
all belong to the core, and the query is *shared* with every other caller asking
the same question — two components calling `injectQuery(useOrderList)` are one
entry and one request. Wearing a `resource`'s clothes would advertise a
lifecycle this does not have.

## Identity, and what would destroy it

The core returns the **same object** when nothing it can prove changed, so an
`OnPush` child whose `[order]` did not move is never checked.

Angular will not rewrite that object — a signal holds whatever it is given —
which makes the hazard here different from Vue's. It is the binding itself: one
`{...state()}` in a `computed`, one `.map` on the way to a template, and every
read mints a new object. Because Angular's default signal equality is
`Object.is`, a `computed` that rebuilds its value is never equal to itself, so
it does not merely waste work — it marks its consumers dirty on every change
detection, forever.

Nothing here copies. The identity tests assert it against the cache's own
objects — `toBe(cache.getState().data)` — and four of the five fail when a
`computed` is made to rebuild its value; that was measured, not assumed.

## Reactive arguments

`args` may be a plain object, a signal, or a getter — a signal *is* a getter, so
one type covers both:

```ts
injectQuery(useOrderGet, { path: { id: 1 } })                    // read once
injectQuery(useOrderGet, () => ({ path: { id: this.id() } }))    // follows the signal
```

The re-subscription is an `effect` over the derived **cache key**, not over the
arguments object: a getter mints a fresh literal every evaluation, and watching
it would tear the subscription down and put it back on every unrelated tick —
which against a ref-counted cache drops the query's mount count to zero each
time, unlinking it from the tag index and making a query on screen a candidate
for LRU eviction. Static arguments create no effect at all.

The re-subscription's signal writes run inside `untracked`, which is what makes
one binding correct from Angular 17 through 22: before 19 a signal write during
an effect's tracked execution is `NG0600`, and the option that used to permit it
has since been removed from the effect signature.

## Lifetime is the injector's

Teardown hangs off `DestroyRef`, so the binding is released when the injection
context is destroyed — a component, a lazily-loaded route's injector, a service,
or one created by hand. `ngOnDestroy` would only ever cover the first.

Pass `{injector}` to call either binding from outside an injection context —
from `ngOnInit`, or a callback — and its lifetime becomes that injector's.

## Mutations

`mutate` **never rejects**: a failure lands in `status` and `error`, and the
promise resolves with `undefined`. That is why the `(click)` above needs no
`.catch`. A mutation that recorded the error *and* rejected would ask every
caller to remember one — and each forgotten `.catch` is an `unhandledrejection`
per failed write, which in production means an alert firing about an error the
user is already looking at. Angular offers no convention that argues the other
way: `ErrorHandler` receives errors thrown *through* Angular — a template
expression, a lifecycle hook, an effect — and a rejected promise returned from
an event binding is not one of them, so the rejection would be nobody's.

When you need to sequence work after a write and must not continue if it did not
happen, ask for the rejection by name:

```ts
await this.create.mutateAsync({ body: { total: 0 } }); // throws on failure
this.router.navigate(['/orders']);
```

Both record identical state. The only difference is who owns the failure.

`injectMutation`'s options may be a getter, so a `place` callback that closes
over component state is current when the write actually runs. Only `client` is
read once: which cache a write goes to cannot change under an in-flight request.

## Configuring a client

`configureClient()` from the core is enough on its own:

```ts
import { configureClient, RestTransport } from '@forge-go/client-core';
import { entities } from './generated/ops';
import { client } from './generated/rest';

configureClient({ transport: new RestTransport({ client }), entities });
```

The provider is **optional**, and deliberately so. A generated `hooks.ts` binds
at module scope, long before an application exists to hand anything to;
requiring a provider would mean a file regenerated from a Go route table had
decided how the consuming application does dependency injection. Use one when a
global is the wrong answer:

```ts
bootstrapApplication(App, { providers: [provideClient(cache)] });
```

It works anywhere providers do — a lazy route's, a component's own — and
`CLIENT` is exported for an application that would rather inject the cache
the way it injects everything else.

Resolution is explicit, then injected, then global: `injectQuery(op, args, {client})`
beats a provider, which beats `configureClient`. With none of the three,
`getClient()` throws by name rather than minting a scratch cache that nothing
else can see.

## What it guarantees

- **One request per query, not per component.** Two components calling
  `injectQuery(useOrderList)` are one cache entry, one registry mount and one
  request. They settle together.
- **A write to `Order:7` checks only what references `Order:7`.** No
  invalidation is authored in the component; the server declared it. That holds
  for the entities a mutation's own response commits, even when no tag it
  declared reaches them — a query displaying `Order:9` is updated by a create
  that returns `Order:9` and invalidates only `Order[]`.
- **An unchanged entity keeps its object identity across a refetch**, so an
  `OnPush` child holding it as an input is never checked. The container is not
  guaranteed: a fresh response is a fresh skeleton, and the store does not claim
  to know that the shape of a list is unchanged. Identity is guaranteed where
  identity is known.
- **One subscription, released once**, whether the injector is destroyed, the
  component is, or `destroy()` is called twice by hand. `live` releases on all
  three as well.
- **`live` is opt-in per call site, and shared underneath.** Two components on
  the same live query are one subscription; two *different* live queries whose
  entities ride the same channel are one connection.

## Refreshing a query you don't hold

`injectQuery` hands back a `refetch` for the query that component opened. When
the write lives somewhere else, use `injectInvalidate`:

```ts
import { Component, input, output } from '@angular/core';
import { injectInvalidate, injectMutation } from '@forge-go/client-angular';
import { useOrderArchive, useOrderList } from './generated/hooks';

@Component({
  selector: 'app-archive-dialog',
  template: '<button (click)="submit()">Archive</button>',
})
export class ArchiveDialog {
  readonly id = input.required<number>();
  readonly done = output<void>();

  private readonly archive = injectMutation(useOrderArchive);
  private readonly invalidate = injectInvalidate();

  async submit(): Promise<void> {
    await this.archive.mutateAsync({ path: { id: this.id() } });
    this.invalidate(useOrderList);
    this.done.emit();
  }
}
```

You name the operation, never the component. `useOrderList` is a module-level
constant out of the generated `hooks.ts`, so the dialog imports the read it wants
refreshed and stays ignorant of whatever list happens to be displaying it.

Three ways to say which:

```ts
this.invalidate(useOrderList);                     // every cached variant
this.invalidate(useOrderGet, { path: { id: 7 } }); // that one exactly
this.invalidate.tags(['Order[]', 'Order:7']);      // the tag graph directly
```

Pass no arguments and you cover every page, filter and sort the cache is holding
for that operation, which is usually what you mean once a write has landed. Pass
arguments and you get the one query they key, the same key `injectQuery` computed
when some other component opened it. A getter works there too, and a `Signal` is
a getter, so the `() => ({ path: { id: this.id() } })` you already handed
`injectQuery` names the same query here. It is read at the call, and not as a
tracked read, so nothing re-runs when the signal moves. One wrinkle.
`invalidate(op)` and `invalidate(op, {})` do not name the same query, because a
read called with no arguments keys differently from one called with an empty
object, and the no-argument form covers both.

`invalidate` marks queries stale and returns. Mounted ones refetch on the next
batch, and several invalidations raised in the same turn coalesce into one round
of requests. Unmounted ones keep the flag and refetch when they next mount, so a
list on a route you have navigated away from costs you nothing until you go back
to it.

When you have to wait, ask for it by name:

```ts
await this.archive.mutateAsync({ path: { id: this.id() } });
await this.invalidate.refetch(useOrderList);
this.done.emit();
```

That starts the mounted matches now and resolves once they have settled, which
is what you want before a dialog closes over a list the user is about to read.
It rejects on failure, like `mutateAsync` and unlike `mutate`. Unmounted matches
stay lazy. Refetching twenty cached filter combinations nobody is looking at
would spend twenty requests to no purpose.

Reach for `tags` wherever your operations declare `provides`. That is the
runtime's own model, and it keeps the decision on the server where the schema
lives. Most generated reads declare nothing today, though, so the callable form
addresses queries by operation and works whether or not the tag graph has an edge
to the thing you want refreshed.

Named `inject*` like its siblings, because it resolves the cache from the
injector. It takes the same `{injector}` escape hatch for a call site with no
ambient context, and the same `{client}` override. Unlike them it registers no
teardown, so there is no `destroy()`: it holds no subscription, no effect and no
socket, and a `{client}` call needs no injection context at all.

## Live queries

```ts
readonly streaming = signal(false);
readonly orders = injectQuery(useOrderList, () => ({ query: { status: this.filter() } }), {
  live: this.streaming,
});
```

That subscribes to every channel the manifest binds to an entity this query's
result type can contain, and releases it when the injection context is
destroyed. A frame updates the store directly, so `order.updated` costs no
request at all.

**Opt-in, per call site, deliberately.** Making it automatic would be fewer
characters and two worse properties: a developer reading a component could no
longer tell whether it holds a socket, and the application's connection count
would become an emergent property of the render tree.

`live` is reactive, like `args` — a `Signal` is a function, so one type covers a
signal and a `() => this.tab() === 'live'`. Toggling it subscribes or releases
and does **nothing** to the query: no remount, no refetch, no loading state.
Turning it on does not refetch to cover the window it was off for; freshness is
the cache's business, and `live` is not a hidden refetch trigger. The gap that
genuinely is the runtime's fault, a dropped socket, is recovered by the core.
`destroy()` stops the live subscription and its effect too.

It needs a stream runtime — a `StreamBinder` constructed over the same cache.
Without one, `{live: true}` reports through the cache's `onError` rather than
silently handing back a query that never updates.

## What it does not do yet

Devtools and SSR hydration — both land across every adapter at once rather than
one framework at a time.

## Peer dependencies

Angular **and** `@forge-go/client-core` are peers, not dependencies. Two copies
of Angular means two DI trees and two reactive graphs. Two copies of the core
means two module-level caches, so the client the application configured is not
the one its generated hooks read from — the same defect, one layer down.

Angular 17 and above, where signals and `DestroyRef` are stable; developed and
tested against 22. The package ships plain `tsc` output rather than an ngc
build, because it contains no components, no templates and no decorators —
`InjectionToken` and `inject()` need no Angular compiler.
