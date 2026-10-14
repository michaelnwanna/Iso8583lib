# Changelog

All notable changes to this project are documented here. This project follows Semantic Versioning.

## [0.2.0] - 2026-10-14

### Added

- Packed BCD numeric fields with deterministic odd-digit padding in all three codecs.
- ASCII or packed-BCD LLVAR/LLLVAR field length prefixes.
- Incremental zero-copy frame extraction for partial and concatenated receive buffers.
- Shared cross-language BCD conformance coverage.

### Changed

- The example profile now demonstrates packed private numeric data in field 60.
- Release automation selects notes by version tag.

## [0.1.0] - 2026-10-14

### Added

- Dependency-free ISO 8583 codecs for Go 1.23, .NET 8, and C++17.
- Binary primary and secondary bitmaps with fixed, LLVAR, and LLLVAR fields.
- Strict JSON host profiles, per-MTI validation, and configurable transport framing.
- Shared cross-language conformance vectors and synthetic authorization scenarios.
- Go fuzz targets, race testing, and dependency-free benchmarks for all implementations.
- NuGet package metadata and an installable CMake config package.

[0.1.0]: https://github.com/michaelnwanna/Iso8583lib/releases/tag/v0.1.0
[0.2.0]: https://github.com/michaelnwanna/Iso8583lib/releases/tag/v0.2.0