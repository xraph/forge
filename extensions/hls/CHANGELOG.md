# Changelog

## [2.0.0](https://github.com/xraph/forge/compare/extensions/hls/v1.10.0...extensions/hls/v2.0.0) (2026-09-06)


### ⚠ BREAKING CHANGES

* **extensions:** the storage and cron extensions are removed. Use github.com/xraph/trove and github.com/xraph/dispatch. The hls extension now requires the trove extension to be registered instead of the storage one.

### Features

* **logger:** adopt the rewritten logger with automatic format selection ([0c0b5de](https://github.com/xraph/forge/commit/0c0b5dee6180228e5d830447a9398bcfe7d1d2c0))


### Bug Fixes

* **deps:** move every module to grpc 1.83.2 ([79ae327](https://github.com/xraph/forge/commit/79ae32783b4f31cf18114402ef7fc8ca2967b7c2))
* **deps:** move to confy v1.0.3 ([#77](https://github.com/xraph/forge/issues/77)) ([5013a4a](https://github.com/xraph/forge/commit/5013a4a28f3c971e9e6817f83ed3f17f0aaaffed))
* **hls:** stop segment cleanup deleting the newest segments ([8651753](https://github.com/xraph/forge/commit/8651753b282cad19fe6338853cd7302a6319f89c))
* **openapi:** key unnamed types by shape and surface generation errors ([6faf745](https://github.com/xraph/forge/commit/6faf7452b2586215183f6e1e7616f920015a07ac))


### Code Refactoring

* **extensions:** remove storage and cron, move hls onto trove ([c0a2185](https://github.com/xraph/forge/commit/c0a218531acc49611d622738fa32dc0b6fc751e3))
