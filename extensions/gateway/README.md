# Gateway (removed)

This extension is gone. Use [bastion](https://github.com/xraph/bastion), which
lives in its own repo and ships its own Forge extension.

What this one did: FARP service discovery, HTTP, WebSocket, SSE and gRPC
proxying, load balancing, circuit breakers, rate limiting, health monitoring,
response caching, TLS and mTLS, OpenAPI aggregation, and an admin dashboard for
managing routes. Bastion covers that ground and is the one that gets worked on.

## Moving over

```bash
go get github.com/xraph/bastion
```

Register it the way you registered this one, in the `Extensions` list of your
app config. Routes, upstreams and policies are configured on Bastion's own
types rather than this extension's, so budget time to translate your gateway
config rather than expecting it to drop in unchanged.

The admin dashboard moved with it, so you do not lose route management.
