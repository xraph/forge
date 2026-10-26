# Changelog

## [2.0.0](https://github.com/xraph/forge/compare/extensions/grpc/v1.10.0...extensions/grpc/v2.0.0) (2026-10-26)


### ⚠ BREAKING CHANGES

* **grpc:** the gRPC port opens when app.Start returns, not when the extension starts. An extension Start or a lifecycle hook that dials the local gRPC server, or reads GetServer, IsRunning or GetServices, now sees a server that isn't serving yet. Do that after app.Start returns. A listen or TLS error still fails app.Start, now from the "grpc-serve" hook, and at that point the app counts as started, so call app.Stop as you would after any other failed after-start hook.
* **grpc:** an app whose TLS files don't load used to come up serving plaintext and now fails to start. Fix the paths or turn enable_tls off. Clients that were connecting with -plaintext to such a server were never getting the TLS you configured.
* **grpc:** record rpc.server.call.duration in place of the grpc_* series

### Features

* **grpc:** record rpc.server.call.duration in place of the grpc_* series ([04dd2c1](https://github.com/xraph/forge/commit/04dd2c1c70a17d22d6ebdef905e703f73207bba8))
* **grpc:** register app services with WithService and WithServiceRegistrar ([acf3cd3](https://github.com/xraph/forge/commit/acf3cd3bea2880bfa6dcf8307c51440d84a1abe7))
* **grpc:** report checker changes from health Watch and statuses from List ([d6d69b8](https://github.com/xraph/forge/commit/d6d69b88ef9e22cb98d418d99df7f05e0487c2ae))
* **grpc:** start the server at the end of app.Start so other extensions can register ([c96d1d9](https://github.com/xraph/forge/commit/c96d1d9d970a959b01afe4a7b55a069503472f51))
* **grpc:** trace outbound gRPC calls from conduit and ClientDialOptions ([6a83e26](https://github.com/xraph/forge/commit/6a83e26ca86d6a00096a110f3aad048ff85534a2))
* **grpc:** trace served calls with otelgrpc and put trace IDs on the log context ([78e33f3](https://github.com/xraph/forge/commit/78e33f33a6f9125cdaa8a3cbaff7b135a72d335c))
* **telemetry:** bridge go-utils log lines to OTLP ([03b675a](https://github.com/xraph/forge/commit/03b675a732ed631efd248324721447a7521ceca1))


### Bug Fixes

* bumped confy version ([23956ac](https://github.com/xraph/forge/commit/23956acc2994e5b71c156146ae03cc726bdf5df6))
* **grpc:** drop the server when Start fails to listen ([92adeee](https://github.com/xraph/forge/commit/92adeee320b03e56de175ec880da5913a3baf8ad))
* **grpc:** fail Start when TLS is on and the credentials don't load ([fa11e8f](https://github.com/xraph/forge/commit/fa11e8f88954a83c6ed2c9d58d09d5d9f2c06c9d))
* **grpc:** keep services registered before Start ([977f62f](https://github.com/xraph/forge/commit/977f62f2e10b2e62b69d38f6b383764477b944a5))
* **grpc:** put trace_id and span_id on the server's own log lines ([d03e129](https://github.com/xraph/forge/commit/d03e129cdbb733f7b6ce6ec61d8005f32d32a7e3))
* **grpc:** register the example's services before the server starts ([f867897](https://github.com/xraph/forge/commit/f8678975b07258994027abf73448443bcc7886db))
* **grpc:** stop GracefulStop deadlocking on calls in flight ([ca6120b](https://github.com/xraph/forge/commit/ca6120b1a2e12b5a8ab71f942201ba769dafd0c3))
