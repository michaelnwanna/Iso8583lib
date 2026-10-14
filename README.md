# ISO 8583 Core

A dependency-free, performance-oriented ISO 8583 codec implemented consistently in Go, C#, and C++17.

## Features

- Binary primary and secondary bitmaps (fields 2-128)
- Fixed, LLVAR, and LLLVAR byte fields
- Strict validation for undefined, oversized, truncated, and trailing data
- Caller-owned output buffers in each implementation's fast path
- Immutable message specifications after construction
- No runtime dependencies outside each language's standard library
- CI builds and tests on Linux with Go, .NET 8, GCC, and CMake

The codec deliberately treats field payloads as bytes. Character sets, BCD, packed numerics, headers, MAC calculation, and network framing belong in composable adapters so the hot wire-codec path remains predictable.

## Layout

| Directory | Package | Minimum target |
| --- | --- | --- |
| `go/` | `github.com/michaelnwanna/iso8583-core/go/iso8583` | Go 1.23 |
| `csharp/` | `Iso8583.Core` | .NET 8 |
| `cpp/` | `iso8583::core` | C++17 |

## Go

```go
spec, _ := iso8583.NewSpec(map[int]iso8583.FieldSpec{
    2: {LengthType: iso8583.LLVAR, Length: 19},
    3: {LengthType: iso8583.Fixed, Length: 6},
})

message, _ := iso8583.NewMessage("0200")
_ = message.SetField(2, []byte("4761739001010010"))
_ = message.SetField(3, []byte("000000"))
wire, err := message.AppendTo(make([]byte, 0, 128), spec)
```

Run:

```sh
cd go
go test ./...
go test -run '^$' -bench . -benchmem ./...
```

## C#

```csharp
var spec = new MessageSpec(new Dictionary<int, FieldSpec>
{
    [2] = new(LengthType.Llvar, 19),
    [3] = new(LengthType.Fixed, 6)
});

var message = new Message("0200");
message.SetField(2, "4761739001010010"u8);
message.SetField(3, "000000"u8);
Span<byte> wire = stackalloc byte[128];
message.TryWrite(wire, spec, out var bytesWritten);
```

Run:

```sh
dotnet run --project csharp/tests/Iso8583.Core.Tests -c Release
```

## C++

```cpp
const iso8583::spec spec({
    {2, {iso8583::length_type::llvar, 19}},
    {3, {iso8583::length_type::fixed, 6}},
});

iso8583::message message("0200");
message.set_field(2, "4761739001010010");
message.set_field(3, "000000");
std::vector<std::uint8_t> wire;
wire.reserve(128);
message.append_to(wire, spec);
```

Build and test:

```sh
cmake -S cpp -B cpp/build -DCMAKE_BUILD_TYPE=Release
cmake --build cpp/build --parallel
ctest --test-dir cpp/build --output-on-failure
```

## Performance

Reuse output capacity in hot paths. Go's `AppendTo`, C#'s `TryWrite`, and C++'s `append_to` all write into caller-provided storage. Specifications can be shared across requests; messages should remain request-scoped.

Benchmarks are machine-dependent. Run the included Go benchmark on deployment-class hardware rather than relying on README numbers.

## Scope and safety

ISO 8583 deployments differ by network and version. Define the exact field dictionary required by your host and test it against approved network vectors. Do not log PAN, PIN, track, CVV, or key material. This project is a codec foundation, not a PCI DSS compliance boundary or a network-certified message profile.

## License

MIT