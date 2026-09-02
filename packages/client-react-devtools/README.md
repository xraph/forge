# @forge-go/client-react-devtools

Mount `@forge-go/client-devtools` from React, and pay nothing for it in
production.

You've probably wired the devtools by hand before:

```ts
if (process.env.NODE_ENV !== 'production') {
  void (async () => {
    const { attach } = await import('@forge-go/client-devtools');
    const { mountOverlay } = await import('@forge-go/client-devtools/overlay');

    globalThis.forge = attach(client);
    mountOverlay(globalThis.forge);
  })();
}
```

Two imports, because `attach` and `mountOverlay` live at two entry points: the
inspection API is the root, the UI is `/overlay` (or `/panel`). Reaching for
`mountOverlay` on the root import gets you `undefined`, and written as an
optional call it fails silently.

This package replaces that block with a component:

```tsx
<ClientProvider client={client}>
  <Routes />
  <ForgeDevtools />
</ClientProvider>
```

`ForgeDevtools` resolves the cache through `useClient()`, the same explicit,
then provider, then global precedence your other hooks already follow. On
mount it attaches the inspector, mounts the panel, and sets `globalThis.forge`
so you can drive it from the console. On unmount it tears all of that down.
It renders `null`: the panel lives in its own shadow root on `document.body`,
so your re-renders can't disturb it and your CSS can't reach it.

Not a subpath on `@forge-go/client-react`. That package is a production
dependency of your app, and hanging a devtools subpath off it would drag a
`client-devtools` peer into a production manifest. A separate package keeps
`client-devtools` out of that manifest, which is where the constraint actually
bites. This package does declare it as a peer, because `ForgeDevtools` imports
it at runtime, but this package is itself a devDependency of your app, so the
peer it pulls in is one too.

## Install

Four packages, all of them dev-only from your app's point of view:

```sh
npm i -D @forge-go/client-react-devtools @forge-go/client-devtools
```

`@forge-go/client-core` and `@forge-go/client-react` are the other two peers,
and you already have them: they are what your app is built on. Skipping
`@forge-go/client-devtools` installs cleanly and then throws
`ERR_MODULE_NOT_FOUND` the first time `<ForgeDevtools />` mounts, because that
is when it is dynamically imported.

## Props

| prop | default | |
|---|---|---|
| `client` | none | Beats the provider and the module default, same as everywhere else. |
| `panel` | `true` | `false` mounts the lean overlay instead of the full panel. |
| `open` | `false` | Start open rather than as a button in the corner. |
| `frames` | off | Keep the last N stream frames, payloads included. |
| `limit` | `500` | How many events the causal log holds. |
| `manager` | from `cache.live` | Only needed if your app built a `SubscriptionManager` the cache doesn't know about. |
| `binder` | from `cache.live` | Same escape hatch as `manager`, for a `StreamBinder`. |

`useForgeDevtools(client?)` gets you the same `Devtools` instance from a
hook, for a test or a component that wants to call `whyNotRefetched` directly
instead of going through the panel.

## Zero production cost, from two directions

The package's `exports` map does the first one:

```json
"exports": {
  ".": {
    "types": "./dist/index.d.ts",
    "development": "./dist/dev.js",
    "default": "./dist/noop.js"
  }
}
```

A bundler that honours the `development` condition (Vite and webpack 5 both
do; esbuild wants `--conditions=development`) resolves `import` and
`require` calls straight to `dist/noop.js` in a production build. That file
is one function returning `null`. There's no devtools code in the bundle to
tree-shake away, because none of it was ever resolved in the first place.

The second guard is for a bundler that ignores export conditions and resolves
`dist/dev.js` regardless. Inside it, a bare `process.env.NODE_ENV !==
'production'` wraps the dynamic imports of `client-devtools`, `./panel` and
`./overlay`. Written bare, that check folds at build time and the import
along with it, so a minifier drops the whole branch. Spelt defensively, as
`typeof process === 'undefined' || process?.env?.NODE_ENV !== 'production'`,
the first half can't fold, the guard survives, and the whole package ships to
production with it. `client-devtools`'s own README carries the same warning,
for the same reason: it's cheap to write the safe-looking version and
expensive to find out later that it never folded.

## StrictMode

`attach()` claims `cache.observer`, a single slot on the cache, and hands it
back on `dispose()`. React 18 StrictMode double-invokes effects in
development, which is exactly where `ForgeDevtools` runs. Mount, mount,
unmount, unmount against one observer slot would either leave two panels on
screen or restore a stale observer over a live one, and neither is a bug you
want to chase at 11pm.

So attachment is a module-level map, keyed by cache, refcounted per
component. A second `<ForgeDevtools />` mounted against the same cache joins
the first instance instead of attaching a second one, and the observer only
comes back once the last holder has unmounted. Two components mounting in the
same tick both await the same dynamic import; whichever resolves second finds
the first one's entry already there and joins it too.
