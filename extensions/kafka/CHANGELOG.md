# Changelog

## [1.11.0](https://github.com/xraph/forge/compare/extensions/kafka/v1.10.0...extensions/kafka/v1.11.0) (2026-10-22)


### Features

* **kafka:** trace records from send to handler and record semconv durations ([e275ff3](https://github.com/xraph/forge/commit/e275ff34700891ab6984dfc8090c618ce909cea0))
* **telemetry:** bridge go-utils log lines to OTLP ([03b675a](https://github.com/xraph/forge/commit/03b675a732ed631efd248324721447a7521ceca1))


### Bug Fixes

* bumped confy version ([23956ac](https://github.com/xraph/forge/commit/23956acc2994e5b71c156146ae03cc726bdf5df6))
* **kafka:** inject the async send span's context into the record ([b4c986b](https://github.com/xraph/forge/commit/b4c986b2e0193d9dad6b0f60a018cff2c1ddd13b))
* **kafka:** send the headers and timestamp SendMessages was dropping, add ctx-aware calls ([502586c](https://github.com/xraph/forge/commit/502586c8c7a3d4c134922ce6c83be10c440a3584))
* **kafka:** skip telemetry for an empty batch, bound conduit tracing test waits ([0b29c58](https://github.com/xraph/forge/commit/0b29c5858ceba58951ce831c8fffd355f90c17bb))
