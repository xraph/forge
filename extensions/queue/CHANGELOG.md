# Changelog

## [1.11.0](https://github.com/xraph/forge/compare/extensions/queue/v1.10.0...extensions/queue/v1.11.0) (2026-10-22)


### Features

* **queue:** trace messages from publish to handler and record messaging durations ([35e5019](https://github.com/xraph/forge/commit/35e50193f2b2e504107850aa6b55dfedbddbcb5d))
* **telemetry:** bridge go-utils log lines to OTLP ([03b675a](https://github.com/xraph/forge/commit/03b675a732ed631efd248324721447a7521ceca1))


### Bug Fixes

* bumped confy version ([23956ac](https://github.com/xraph/forge/commit/23956acc2994e5b71c156146ae03cc726bdf5df6))
* **queue:** escape AMQP credentials and keep passwords out of logs ([fd078e5](https://github.com/xraph/forge/commit/fd078e54a954c084ef21ce77fe88c0e49f769fec))
* **queue:** merge credential escaping and log redaction ([b3bbf6d](https://github.com/xraph/forge/commit/b3bbf6d92dddde7bde11edfe3afeca36641b958f))
