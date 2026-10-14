# ISO 8583 Core v0.2.0

This release adds production interoperability features while preserving the v0.1 ASCII and binary wire behavior.

## Highlights

- Pack and parse decimal BCD fields consistently in Go, C#/.NET 8, and C++17.
- Configure ASCII or packed-BCD LLVAR and LLLVAR length prefixes per field.
- Preserve logical digit values at the message API while minimizing packed wire size.
- Extract one frame at a time from partial or concatenated TCP receive buffers without copying payload data.
- Verify exact BCD payload and transport bytes through the shared conformance suite.

Odd-digit BCD values use a leading zero nibble. Decoders reject non-decimal nibbles and non-zero padding. Existing profiles without the new options retain their v0.1 encoding behavior.

The release includes `Iso8583.Core.0.2.0.nupkg`. Go consumers should use `go/v0.2.0`; C++ consumers can use the source archive or installed CMake config package.