# ISO 8583 Core v0.1.0

The first public release provides equivalent, dependency-free ISO 8583 codec foundations for Go, C#/.NET 8, and C++17.

## Highlights

- Encode and parse binary primary and secondary bitmaps.
- Define fixed, LLVAR, and LLLVAR fields through immutable codec specifications.
- Load strict host profiles from JSON and validate required fields by MTI.
- Add and validate none, two-byte, or four-byte binary/ASCII transport length headers.
- Verify identical wire output in all three languages with shared golden vectors.
- Reuse caller-owned output capacity through each implementation's fast path.

The release includes the `Iso8583.Core.0.1.0.nupkg` NuGet package as a GitHub release asset. Go consumers can use the tagged module path, and C++ consumers can build from the source archive with either `add_subdirectory` or the installed CMake config package.

## Safety

The example profile and transaction identifiers are synthetic. This library does not provide network I/O, cryptographic key management, MAC verification, or PCI DSS compliance. Integrators must use their network's approved field dictionary and certification vectors.