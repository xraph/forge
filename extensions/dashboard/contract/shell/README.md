# Dashboard Contract Shell

The React/TypeScript runtime that consumes the dashboard contract.

## Development

```bash
pnpm install
pnpm dev   # Vite dev server on :5173, proxies /api/dashboard/* to :8080
```

## Build

```bash
pnpm build   # Emits dist/ — embedded into the dashboard Go binary via //go:embed
```

## Test

```bash
pnpm test
```

See [SLICE_D_DESIGN.md](../SLICE_D_DESIGN.md) for the architecture and [SLICE_D_PLAN.md](../SLICE_D_PLAN.md) for the implementation plan.
