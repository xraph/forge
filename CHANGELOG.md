# Changelog

## [0.9.11](https://github.com/xraph/forge/compare/v0.9.10...v0.9.11) (2026-02-18)


### Features

* **build:** add build-modules target and enhance CI workflow ([05ae5c5](https://github.com/xraph/forge/commit/05ae5c5))


### Maintenance

* Merge branch 'main' of github.com:xraph/forge ([2b900fd](https://github.com/xraph/forge/commit/2b900fd))
* **changelog:** update CHANGELOG.md for v0.9.10 ([67abd3e](https://github.com/xraph/forge/commit/67abd3e))

## [0.9.10](https://github.com/xraph/forge/compare/v0.9.9...v0.9.10) (2026-02-16)


### Features

* **database:** implement app-scoped migration management and configuration ([58eb2bd](https://github.com/xraph/forge/commit/58eb2bd))


### Refactoring

* enhance dependency injection with new methods and update documentation ([cee3d89](https://github.com/xraph/forge/commit/cee3d89))
* streamline dependency injection with Provide and update FARP configuration ([8215292](https://github.com/xraph/forge/commit/8215292))


### Maintenance

* **changelog:** update CHANGELOG.md for v0.9.9 ([0706004](https://github.com/xraph/forge/commit/0706004))

## [0.9.9](https://github.com/xraph/forge/compare/v0.9.8...v0.9.9) (2026-02-15)


### Features

* **discovery:** add FARP and mDNS configuration options with comprehensive tests ([33d74ab](https://github.com/xraph/forge/commit/33d74ab))


### Maintenance

* **changelog:** update CHANGELOG.md for v0.9.8 ([e955a2e](https://github.com/xraph/forge/commit/e955a2e))

## [0.9.8](https://github.com/xraph/forge/compare/v0.9.7...v0.9.8) (2026-02-14)


### Features

* **config:** add configuration validation for Forge projects ([e76119d](https://github.com/xraph/forge/commit/e76119d))
* **docs:** enhance build, deploy, and development command documentation ([1e14b5d](https://github.com/xraph/forge/commit/1e14b5d))
* **docker:** integrate Docker support into development configuration ([c9d0681](https://github.com/xraph/forge/commit/c9d0681))


### Maintenance

* simplify test execution in CI workflow ([aba63f6](https://github.com/xraph/forge/commit/aba63f6))
* Add new dependencies and build artifacts to the project. ([13e51ff](https://github.com/xraph/forge/commit/13e51ff))
* Update dependencies and build artifacts. ([692f9de](https://github.com/xraph/forge/commit/692f9de))

## [0.9.7](https://github.com/xraph/forge/compare/v0.9.6...v0.9.7) (2026-02-10)


### Features

* **makefile:** enhance formatting and vetting for all Go modules ([5c25cc5](https://github.com/xraph/forge/commit/5c25cc5))


### Refactoring

* **config:** standardize formatting and improve readability in configuration files ([3907b6e](https://github.com/xraph/forge/commit/3907b6e))

## [0.9.6](https://github.com/xraph/forge/compare/v0.9.5...v0.9.6) (2026-02-10)


### Features

* **gateway:** add new gateway extension with access logging, authentication, caching, and circuit breaker ([c11ca75](https://github.com/xraph/forge/commit/c11ca75))

## [0.9.5](https://github.com/xraph/forge/compare/v0.9.4...v0.9.5) (2026-02-10)


### Bug Fixes

* **health:** reduce log noise by changing periodic health reports to DEBUG level ([bf23a23](https://github.com/xraph/forge/commit/bf23a23))

## [0.9.4](https://github.com/xraph/forge/compare/v0.9.3...v0.9.4) (2026-02-10)


### Bug Fixes

* **cli:** add missing app_config.go file and update .gitignore ([d205473](https://github.com/xraph/forge/commit/d205473))

## [0.9.3](https://github.com/xraph/forge/compare/v0.9.2...v0.9.3) (2026-02-10)


### Features

* **config:** implement loading of .forge.yaml for app-level configuration ([6ef25cd](https://github.com/xraph/forge/commit/6ef25cd))


### Maintenance

* **dependencies:** update forge and ai-sdk versions across multiple modules ([04ffee4](https://github.com/xraph/forge/commit/04ffee4))

## [0.9.2](https://github.com/xraph/forge/compare/v0.9.1...v0.9.2) (2026-02-09)


### Features

* **generate:** add CLI command generation and improve app structure handling ([bce139e](https://github.com/xraph/forge/commit/bce139e))

## [0.9.1](https://github.com/xraph/forge/compare/v0.9.0...v0.9.1) (2026-02-08)


### Features

* **release:** enhance release management for extensions and update workflows ([beb1b34](https://github.com/xraph/forge/commit/beb1b34))
* **database:** integrate dotenv for environment variable management ([837433b](https://github.com/xraph/forge/commit/837433b))


### Bug Fixes

* **workflows:** improve test command in GitHub Actions workflow ([5b36d70](https://github.com/xraph/forge/commit/5b36d70))
* **manifest:** correct formatting in release-please manifest file ([6e74d3f](https://github.com/xraph/forge/commit/6e74d3f))
* **dev:** ensure goroutine completion in file watcher implementation ([f090469](https://github.com/xraph/forge/commit/f090469))


### Refactoring

* **router:** migrate to vessel for dependency injection ([6acc086](https://github.com/xraph/forge/commit/6acc086))
* **router:** enhance router functionality and improve test coverage ([de411d0](https://github.com/xraph/forge/commit/de411d0))
* **router:** introduce Any method for multi-method route registration ([7b5cdef](https://github.com/xraph/forge/commit/7b5cdef))
* **http:** replace di context with http context in tests and middleware ([732475a](https://github.com/xraph/forge/commit/732475a))
* **extensions:** overhaul AI extension structure and improve modularity ([4bf291c](https://github.com/xraph/forge/commit/4bf291c))
* **extensions:** enhance modularity and update dependencies across multiple extensions ([25d3f34](https://github.com/xraph/forge/commit/25d3f34))
* **dependencies:** migrate to vessel and update dependencies ([e029f3e](https://github.com/xraph/forge/commit/e029f3e))
* **tests:** update error handling tests and remove deprecated code ([0a47571](https://github.com/xraph/forge/commit/0a47571))


### Maintenance

* **goreleaser:** update build configuration and pre-build hooks ([c026e47](https://github.com/xraph/forge/commit/c026e47))
* **dependencies:** update forge dependency and clean up go.sum ([dc280e8](https://github.com/xraph/forge/commit/dc280e8))
* **dependencies:** update Go version and quic-go dependencies ([fc67b88](https://github.com/xraph/forge/commit/fc67b88))
* **dependencies:** update forgeui dependency and enhance README documentation ([83a7a40](https://github.com/xraph/forge/commit/83a7a40))
* **dependencies:** update toml and k8s libraries across examples ([e151c50](https://github.com/xraph/forge/commit/e151c50))
* **examples:** remove outdated example binaries from the repository ([2a7ea5a](https://github.com/xraph/forge/commit/2a7ea5a))
* **examples:** remove database-demo example and related resources ([c31a35d](https://github.com/xraph/forge/commit/c31a35d))

## [0.9.0](https://github.com/xraph/forge/compare/v0.8.6...v0.9.0) (2026-02-08)


### ⚠ BREAKING CHANGES

* **router:** Migrated dependency injection from custom DI to Vessel. HTTP context is now used instead of DI context in middleware and handlers.

### Refactoring

* **router:** migrate to vessel for dependency injection ([6acc086](https://github.com/xraph/forge/commit/6acc086))
* **http:** replace di context with http context in tests and middleware ([732475a](https://github.com/xraph/forge/commit/732475a))


### Bug Fixes

* **goreleaser:** wrap CLI tidy hook in shell invocation ([22b1827](https://github.com/xraph/forge/commit/22b1827))
* **goreleaser:** use shell command for CLI module tidy hook ([5202498](https://github.com/xraph/forge/commit/5202498))

## [0.8.6](https://github.com/xraph/forge/compare/v0.8.5...v0.8.6) (2026-01-03)


### Refactoring

* **database:** enhance database config loading with ConfigManager ([05de831](https://github.com/xraph/forge/commit/05de831))

## [0.8.5](https://github.com/xraph/forge/compare/v0.8.4...v0.8.5) (2026-01-03)


### Features

* **config:** add environment variable expansion with defaults ([25c7546](https://github.com/xraph/forge/commit/25c7546))

## [0.8.4](https://github.com/xraph/forge/compare/v0.8.3...v0.8.4) (2026-01-02)


### Bug Fixes

* **database:** implement lazy migration discovery for improved startup in Docker ([1c1b9de](https://github.com/xraph/forge/commit/1c1b9de))

## [0.8.3](https://github.com/xraph/forge/compare/v0.8.2...v0.8.3) (2026-01-02)


### Features

* **config:** add environment variable source configuration options ([3089e74](https://github.com/xraph/forge/commit/3089e74))

## [0.8.2](https://github.com/xraph/forge/compare/v0.8.1...v0.8.2) (2025-12-31)


### Maintenance

* Minor dependency updates and internal improvements.

## [0.8.1](https://github.com/xraph/forge/compare/v0.8.0...v0.8.1) (2025-12-31)


### Features

* **database:** improve migration path handling and directory creation ([e0a3f59](https://github.com/xraph/forge/commit/e0a3f59))
* **database:** add migration checks and verbose output ([5ef93a7](https://github.com/xraph/forge/commit/5ef93a7))


### Maintenance

* **go.mod:** downgrade Go version from 1.25.3 to 1.24.4 ([788a5c1](https://github.com/xraph/forge/commit/788a5c1))

## [0.8.0](https://github.com/xraph/forge/compare/v0.7.5...v0.8.0) (2025-12-29)


### Features

* enhance AI extension with streaming support and new SDK features ([b9e03ff](https://github.com/xraph/forge/commit/b9e03ff))
* enhance AI extension with new LLM providers and improved configuration ([62f9d4e](https://github.com/xraph/forge/commit/62f9d4e))


### Maintenance

* **release:** bump version to 0.8.0 ([19f1329](https://github.com/xraph/forge/commit/19f1329))
* update .gitignore to exclude additional files and directories ([9eedded](https://github.com/xraph/forge/commit/9eedded))

## [0.7.5](https://github.com/xraph/forge/compare/v0.7.4...v0.7.5) (2025-12-21)


### Maintenance

* **cleanup:** remove trailing whitespace in client_config.go and client.go ([0333877](https://github.com/xraph/forge/commit/0333877))
* **cleanup:** clean up trailing whitespace in multiple files ([558e1cb](https://github.com/xraph/forge/commit/558e1cb))

## [0.7.4](https://github.com/xraph/forge/compare/v0.7.3...v0.7.4) (2025-12-19)


### Features

* **cron:** add cron extension with job scheduling, execution history, and metrics ([a3101e8](https://github.com/xraph/forge/commit/a3101e8))

## [0.7.3](https://github.com/xraph/forge/compare/v0.7.2...v0.7.3) (2025-12-12)


### Bug Fixes

* **validation:** resolve zero-value validation bug for all primitive types - Fixed critical validation bug where required query/header/path parameters with zero values (`false`, `0`, `0.0`) were incorrectly rejected as missing. The validator now properly skips zero-value validation for parameter fields since they're already validated during binding where we can distinguish between missing and explicit zero values. Adds 9 comprehensive test cases covering all primitive types.

## [0.7.2](https://github.com/xraph/forge/compare/v0.7.1...v0.7.2) (2025-12-10)


### Maintenance

* **cleanup:** remove test artifacts and temporary files from boolean validation fix

## [0.7.1](https://github.com/xraph/forge/compare/v0.7.0...v0.7.1) (2025-12-10)


### Bug Fixes

* **validation:** fix required boolean query parameters incorrectly failing when set to false - Previously, required boolean query parameters would fail validation with "field is required" error when explicitly set to `false`. The validator was incorrectly treating Go's zero value (`false`) as a missing parameter. This fix excludes boolean fields from the zero-value required check since they are already validated during the binding phase.

## [0.7.0](https://github.com/xraph/forge/compare/v0.6.0...v0.7.0) (2025-12-08)


### Features

* enhance JSON response handling with struct tags ([ed6d411](https://github.com/xraph/forge/commit/ed6d411f3f5edb488124e1d2591a567ea80d34bb))
* enhance logging and configuration in tests and examples ([75273c6](https://github.com/xraph/forge/commit/75273c6a3608cc3ae9d0cbbaa353f058fa0862a4))
* implement sensitive field cleaning in JSON responses ([f1985cf](https://github.com/xraph/forge/commit/f1985cfeff3c7ad5c8f158c1d3f8573957962ba2))


### Bug Fixes

* add comprehensive documentation for Context and Error Handling ([9f3b27e](https://github.com/xraph/forge/commit/9f3b27e5f8ef49fae77dc4c749b991472d3600cd))
* initialize appWatcher with configuration in tests ([42d7458](https://github.com/xraph/forge/commit/42d7458b7ad58c5cef964016d509749aece95c7c))
* make OpenAPIServer fields optional in OpenAPI spec ([32bc9da](https://github.com/xraph/forge/commit/32bc9da6280b6691ec8eb2f50daabd3df52b6e1c))
* replace NoopLogger with TestLogger in WebRTC and router benchmarks ([e797f9f](https://github.com/xraph/forge/commit/e797f9f79ee21f4a8221413ed1f430326ff71b01))

## [0.6.0](https://github.com/xraph/forge/compare/forge-v0.5.0...forge-v0.6.0) (2025-11-19)


### ⚠ BREAKING CHANGES

* **ci:** Release automation now uses Release Please instead of custom workflow. Version tracking moved from .github/version.json to .release-please-manifest.json.

### Features

* add initial documentation structure and configuration files ([a14bc4f](https://github.com/xraph/forge/commit/a14bc4fdaf5db9c6689edc08a7fc7e35751edfad))
* **app:** introduce functional options for AppConfig and update app creation methods ([69e2319](https://github.com/xraph/forge/commit/69e2319b8265b71865af2614d163983ff09ef20c))
* **banner:** implement startup banner display with configuration options ([c1faf4d](https://github.com/xraph/forge/commit/c1faf4d53311a3226c923b58b083bf1d1a713df8))
* **ci:** implement comprehensive CI/CD workflows and documentation ([5e3c81e](https://github.com/xraph/forge/commit/5e3c81e571812b50ed8d2a172b8cabeab8d7cd54))
* **ci:** migrate to Release Please for automated releases ([412e43b](https://github.com/xraph/forge/commit/412e43ba39e9b69056076cd9c0a523225dcbf46f))
* **config:** enhance DI container integration and update license ([f41a2ba](https://github.com/xraph/forge/commit/f41a2babbc20ddf7647e8d3e8387332ec2031efd))
* **consensus:** enhance RaftNode interface and implement new methods ([75423fe](https://github.com/xraph/forge/commit/75423fe039e2bd51b097544fd10e9a0b9a3b52ed))
* **dev:** implement hot reload functionality and update command syntax ([5ee99c6](https://github.com/xraph/forge/commit/5ee99c6b725f5347fa749744d3405c48c6b46858))
* **discovery:** remove outdated discovery examples and introduce database helpers ([25f2ab0](https://github.com/xraph/forge/commit/25f2ab07fc7ed7d17a7beb2c476e9e03c2a67f7d))
* **docs:** add comprehensive documentation and branding for Forge framework ([cf770d2](https://github.com/xraph/forge/commit/cf770d205cd5875e94758fe7e14bbb8a8b80621f))
* **docs:** add themed logo component and update extensions documentation ([b6a9838](https://github.com/xraph/forge/commit/b6a98380b1de22d801337220648c988e5fc387bb))
* **docs:** update metrics and add logo assets ([d1e4c55](https://github.com/xraph/forge/commit/d1e4c55f4d3cc5ce982a1cf6996fd44c8d74f1fc))
* enhance client generator with new features and error handling ([51692e5](https://github.com/xraph/forge/commit/51692e573f68a92c60c5948953faeec4f8be7471))
* **extension:** enhance process management with wait channel ([76427c0](https://github.com/xraph/forge/commit/76427c0076cc0505ba9f048643938a38eaa43ea9))
* **farp:** add new FARP extension with initial implementation ([67fed23](https://github.com/xraph/forge/commit/67fed23eb5620914a4b435c083b40698f5a820ca))
* **health:** add Windows-specific disk and system metrics collectors ([d61f475](https://github.com/xraph/forge/commit/d61f475c091df0df11fc33f57eb9fcedec9e22e2))
* introduce new CLI framework and dashboard extension ([c64dac8](https://github.com/xraph/forge/commit/c64dac8351f17444040c26fb65351d648c8474a3))
* **license:** add Forge License Decision Tree for quick licensing guidance ([e73443c](https://github.com/xraph/forge/commit/e73443c683a94e3eb4bfe1cf07ccb7037e16ecbc))
* **lifecycle:** introduce LifecycleManager for managing application lifecycle hooks ([b831e50](https://github.com/xraph/forge/commit/b831e509a2012ed75cd987eb5082ba1744877d17))
* **lifecycle:** introduce LifecycleManager for managing application lifecycle hooks ([52b2cff](https://github.com/xraph/forge/commit/52b2cffeae98b6f7ed53dd782b75bfdb5ceaf7b3))
* **local:** add stubs for presence and room store methods ([fe50c28](https://github.com/xraph/forge/commit/fe50c28107d6151ab7238035af5a63323314cbf9))
* **logger:** introduce BeautifulLogger for enhanced logging experience ([e2415d9](https://github.com/xraph/forge/commit/e2415d993ba143b2a1d25248598f2b610216834a))
* **logo:** add new SVG logo asset ([3103a69](https://github.com/xraph/forge/commit/3103a692b5d04fd4e1adbca9fb93b1d2a8f5ceec))
* **memory:** enhance memory manager with embedding function and consolidation testing ([69a56d5](https://github.com/xraph/forge/commit/69a56d5e81c33ab9ff8c5b456028330e61bdbe52))
* **observability:** add metrics and health endpoints to app ([a253e7d](https://github.com/xraph/forge/commit/a253e7da28bb9537a4ad5fb1f08f66c041c9dc7b))
* **process:** implement platform-specific process management for Unix and Windows ([7ae559e](https://github.com/xraph/forge/commit/7ae559ee3c92d0906b086180d35b75c17cd77cc2))
* **scripts:** add script to fix gosec SARIF file format ([96ce0a0](https://github.com/xraph/forge/commit/96ce0a06f426e5aa3e3392b2d70fe4cf62acf602))
* **tests:** add logger and metrics configuration to runnable extension tests ([4704e6b](https://github.com/xraph/forge/commit/4704e6b318bc5cb4ff13f03786090b64dbbae98e))


### Bug Fixes

* add comprehensive README for Forge v2 framework ([fc02c41](https://github.com/xraph/forge/commit/fc02c41a28b56e55b9853174a3222fdd270c1b3c))
* **app:** enhance error handling for endpoint setup and response writing ([5316849](https://github.com/xraph/forge/commit/531684992b11dbbeabe4f91edab579736116427f))
* cast page.data to any to avoid type errors ([004a2d3](https://github.com/xraph/forge/commit/004a2d320f5781ee15aea51f7879b189e456370f))
* **ci:** add continue-on-error to quality job and improve vulnerability check step ([d295ec6](https://github.com/xraph/forge/commit/d295ec661282de39524dd9e9e1369acb48f60a9a))
* **ci:** disable snapcraft builds in GoReleaser - snapcraft not available in GitHub Actions ([0fffc76](https://github.com/xraph/forge/commit/0fffc7675fbe5b6aee8d399e6afa60c95b2969f6))
* **ci:** enable snapcraft builds and install snapcraft in release workflow ([9ad702e](https://github.com/xraph/forge/commit/9ad702e231215ae5bf7b0b299f8e7fc06cf55d56))
* **ci:** exclude Windows from release tests due to flaky AI extension tests ([27a5b6d](https://github.com/xraph/forge/commit/27a5b6d6e9d25a8a5d6b9050063da900ea5a125d))
* **ci:** improve create-or-update-release-pr workflow robustness ([7301d16](https://github.com/xraph/forge/commit/7301d16a5ad257d418de1619a866955712341d5d))
* **ci:** install snapcraft via snap instead of apt ([4d19aad](https://github.com/xraph/forge/commit/4d19aade361977387b7cc97ddff7ae5a326f6734))
* **ci:** make quality checks non-blocking in release workflow ([e73f75e](https://github.com/xraph/forge/commit/e73f75ee906a3f0393fb2c4390768fb3d30a0aa8))
* **ci:** make Windows tests optional in multi-module release workflow ([1ada987](https://github.com/xraph/forge/commit/1ada9872f81392b920701cf5846f43c49a00d968))
* **ci:** prevent auto-release on direct release commits ([d481cbb](https://github.com/xraph/forge/commit/d481cbb8fa344ffe99402354e244081feb10d32e))
* **ci:** resolve bash syntax error in Release Please workflow summary ([3931722](https://github.com/xraph/forge/commit/393172215af9f8119e0a29c1ec04fd997d7a2f84))
* **ci:** trigger auto-release on PR merge event ([77c635b](https://github.com/xraph/forge/commit/77c635b936fbeba71588c0832a4eec9bfb33c775))
* **config:** add range checks for type conversions in GetInt8, GetInt16, and GetUint8 methods ([e0c7451](https://github.com/xraph/forge/commit/e0c745116d4b710d717cf15926415d69917ede35))
* correct loop iteration and enhance comments for clarity ([500ee2b](https://github.com/xraph/forge/commit/500ee2bb90c1e1e5ef1f81fe9ec588d6176fbae6))
* **docs:** update button variants and type assertions ([08305d6](https://github.com/xraph/forge/commit/08305d641aa7aa008066ec0ac2f7d5eba54b1159))
* fixed tests ([c3e812c](https://github.com/xraph/forge/commit/c3e812c1efa70518ec1c778e9f6bdfe1110da0e4))
* force release v0.0.3 ([29c2674](https://github.com/xraph/forge/commit/29c2674ef46231db26bbc1801513b2e8507b7923))
* forced release ([b3fe39f](https://github.com/xraph/forge/commit/b3fe39fe55e760ae07e9809e31c8428f3f6db908))
* forced release ([5f98a16](https://github.com/xraph/forge/commit/5f98a16fbc93a13275ea852895dec896dfc28617))
* **hero:** correct typo in hero component text ([b6a9838](https://github.com/xraph/forge/commit/b6a98380b1de22d801337220648c988e5fc387bb))
* improve GoReleaser config validation in release workflow ([5e81e7f](https://github.com/xraph/forge/commit/5e81e7f9a4b48cd9b5c893a0f2f068e82e5831ff))
* **init:** correct substring length for single-module layout check in project initialization ([ac12030](https://github.com/xraph/forge/commit/ac12030bbd4afce242c0ca08c3134facd394a3a8))
* resolve cross-platform test timing issues in consensus cluster tests ([6c47900](https://github.com/xraph/forge/commit/6c479001d438eaa6c6cb2d2b5b8a57736815513f))
* resolve deadlock between metrics and health manager during concurrent access ([f1a750d](https://github.com/xraph/forge/commit/f1a750d507b8149a7deb5466469216730d6df434))
* update Go version and GitHub Actions dependencies ([9f1777c](https://github.com/xraph/forge/commit/9f1777cdc86ac1e7582518b9fb87df900e500ae1))
* update TypeScript client generator to use HTTPClient ([f95192c](https://github.com/xraph/forge/commit/f95192c1b109697399650de9e3e497e04aafabbd))


### Documentation

* **ci:** add Release Please migration summary ([802d84b](https://github.com/xraph/forge/commit/802d84be8222f8c10e6a260aade720288e3de9a3))
* **extensions:** add comprehensive documentation for core, consensus, events, and hls extensions ([b6a9838](https://github.com/xraph/forge/commit/b6a98380b1de22d801337220648c988e5fc387bb))
* **forge:** add icons to documentation pages ([2e87a10](https://github.com/xraph/forge/commit/2e87a10da8a7306a0c6dc0578c6482c55939d7df))
* update documentation structure and content ([409dd57](https://github.com/xraph/forge/commit/409dd57a44ddf1b242329b1d972318a3815ae639))

### Bug Fixes

* update TypeScript client generator to use HTTPClient ([f95192c](https://github.com/xraph/forge/commit/f95192c1b109697399650de9e3e497e04aafabbd))
