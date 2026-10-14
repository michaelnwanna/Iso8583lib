# ISO 8583 Core

A dependency-free, performance-oriented ISO 8583 codec implemented consistently in Go, C#, and C++17.

## Features

- Binary primary and secondary bitmaps (fields 2-128)
- Fixed, LLVAR, and LLLVAR byte fields
- Strict validation for undefined, oversized, truncated, and trailing data
- Strict host profiles loaded from JSON with per-MTI required fields
- Configurable binary/ASCII transport length headers
- Caller-owned output buffers in each implementation's fast path
- Immutable message specifications after construction
- No runtime dependencies outside each language's standard library
- CI builds and tests on Linux with Go, .NET 8, GCC, and CMake

The codec deliberately treats field payloads as bytes. Character sets, BCD, packed numerics, headers, MAC calculation, and network framing belong in composable adapters so the hot wire-codec path remains predictable.

## ISO 8583 primer

ISO 8583 is a family of standards for messages exchanged by systems that process card-originated financial transactions. A point-of-sale terminal, ATM, acquirer, switch, and issuer can use these messages to request an authorization, report a completed transaction, reverse a transaction, or perform network management.

This section is an educational overview, not a substitute for the licensed ISO standard or a payment network's implementation guide. ISO editions and network profiles differ. Obtain the normative documents from the [ISO catalogue](https://www.iso.org/search.html?q=ISO%208583) and the profile required by your processor or card network.

### Message anatomy

The wire format handled by this library is:

```text
+----------+--------------------------+------------------------------------+
| MTI      | primary/secondary bitmap | data elements in field-number order |
| 4 bytes  | 8 or 16 binary bytes     | fixed or length-prefixed bytes      |
+----------+--------------------------+------------------------------------+
```

1. **Message Type Indicator (MTI):** four digits describing the message's version, class, function, and origin.
2. **Bitmap:** one bit per possible data element. A set bit means that field is present. Bit 1 signals a secondary bitmap; fields 2-64 use the primary bitmap and fields 65-128 use the secondary bitmap.
3. **Data elements:** present fields serialized in ascending numeric order. The profile determines each field's meaning, encoding, and length.

### Reading an MTI

For a commonly deployed 1987-style profile, `0100` can be read as:

| Position | Value | Educational interpretation |
| --- | --- | --- |
| 1 | `0` | ISO 8583:1987 family |
| 2 | `1` | Authorization message class |
| 3 | `0` | Request function |
| 4 | `0` | Acquirer-originated |

An approving host commonly returns `0110`: the function digit changes from request (`0`) to response (`1`). Other editions and private profiles can assign different meanings, so MTIs must be interpreted using the parties' agreed profile.

### Bitmaps

Field $n$ maps to bit $n$. Bits are transmitted most-significant-bit first within each byte. For example, field 3 maps to the third bit of the primary bitmap:

```text
field:       1 2 3 4 5 6 7 8
bit mask:   80 40 20 10 08 04 02 01  (hex)
```

The library stores the bitmap in binary, not as 16 or 32 printable hexadecimal characters. When any field from 65 through 128 is present, it sets bit 1 and emits the additional eight-byte secondary bitmap.

### Field lengths

This project supports the three framing forms used by many profiles:

| Form | Configuration example | Wire representation |
| --- | --- | --- |
| Fixed | field 3, length 6 | exactly 6 payload bytes |
| LLVAR | field 2, maximum 19 | 2 ASCII length bytes, then payload |
| LLLVAR | field 48, maximum 999 | 3 ASCII length bytes, then payload |

For example, a 16-byte synthetic PAN in LLVAR field 2 is encoded as `16` followed by the 16 payload bytes. `Length` is exact for fixed fields and a maximum for variable fields.

The field payload remains opaque to the core codec. A profile may require ASCII digits, EBCDIC, packed BCD, binary values, nested TLV data, or a network-specific character set. Validate and transform those values at the application/profile boundary.

### Common data elements

The following names provide context for the example only; they are not a normative field dictionary:

| Field | Common use in financial profiles |
| --- | --- |
| 2 | Primary account number (PAN) |
| 3 | Processing code |
| 4 | Transaction amount in minor currency units |
| 7 | Transmission date and time |
| 11 | Systems trace audit number (STAN) |
| 12/13 | Local transaction time/date |
| 14 | Card expiration date |
| 18 | Merchant category code |
| 22 | Point-of-service entry mode |
| 32 | Acquiring institution identifier |
| 37 | Retrieval reference number (RRN) |
| 38 | Authorization identification response |
| 39 | Response code |
| 41/42 | Terminal and merchant identifiers |
| 49 | Transaction currency code |

Never assume these meanings are universal. Treat a `Spec`/`MessageSpec` as a versioned contract between endpoints, and test it with vectors approved by the receiving institution.

## Library design

The library is intentionally a small wire-codec core. It separates ISO 8583 structure from institution-specific business rules and transport concerns:

```mermaid
flowchart LR
    A[TCP, TLS, queue, or file] --> B[Transport framing]
    B --> C[ISO 8583 codec]
    D[Institution profile] --> C
    C --> E[Message and byte fields]
    E --> F[Business validation and routing]
    F --> E
    E --> C
    C --> B
```

- **Transport framing** owns network headers, message-length prefixes, connection management, retries, and timeouts. It passes exactly one ISO 8583 payload to this library.
- **Institution profile** defines which fields are valid and whether each is fixed, LLVAR, or LLLVAR. Create it once at startup and reuse it.
- **Codec** maps MTI, bitmap bits, length prefixes, and field bytes. It does not assign business meaning to payloads.
- **Business layer** validates data types and required fields, authorizes or routes transactions, applies idempotency, and builds responses.

### Core abstractions

| Concept | Go | C# | C++ | Responsibility |
| --- | --- | --- | --- | --- |
| Length form | `LengthType` | `LengthType` | `length_type` | Fixed, LLVAR, or LLLVAR framing |
| Field definition | `FieldSpec` | `FieldSpec` | `field_spec` | Framing and exact/maximum byte length |
| Profile | `Spec` | `MessageSpec` | `spec` | Field-number lookup table for one network contract |
| Message | `Message` | `Message` | `message` | MTI and request-scoped field values |
| Encode | `AppendTo` | `TryWrite` / `Encode` | `append_to` | Produce MTI, bitmap, prefixes, and payload bytes |
| Decode | `Parse` | `Message.Parse` | `parse` | Validate and reconstruct a message from wire bytes |

Each implementation uses an array indexed by field number. Field lookup is $O(1)$, and encoding/decoding is a bounded scan of at most 128 fields. There is no reflection, schema discovery, or global mutable registry on the transaction path.

### Request lifecycle

1. At process startup, construct the profile agreed with the receiving institution. Treat it as immutable and share it across requests.
2. Read one complete payload using the network's transport framing. Do not include a TCP length header in the bytes passed to `Parse`.
3. Decode the message and handle malformed input as a protocol error.
4. Validate profile semantics such as required fields, numeric content, supported MTIs, currency, and allowed processing codes.
5. Perform routing or authorization and construct a new response message. Do not mutate and reuse a request across transactions.
6. Encode into reusable output storage, add the network framing, and send the response.

### Host-configured profiles

Host applications can load a complete network contract from JSON instead of compiling field definitions into application code. See [`profiles/example-acquirer.json`](profiles/example-acquirer.json) and validate custom files against [`profiles/profile.schema.json`](profiles/profile.schema.json).

```json
{
    "name": "my-acquirer-v1",
    "transport": {
        "headerBytes": 2,
        "lengthEncoding": "binary-big-endian",
        "maxMessageBytes": 4096
    },
    "fields": {
        "2": {
            "name": "Primary account number",
            "lengthType": "llvar",
            "length": 19,
            "encoding": "ascii",
            "content": "numeric",
            "sensitive": true
        },
        "3": {
            "name": "Processing code",
            "lengthType": "fixed",
            "length": 6,
            "encoding": "ascii",
            "content": "numeric"
        }
    },
    "messages": {
        "0100": { "required": [2, 3] }
    }
}
```

| Property | Meaning |
| --- | --- |
| `name` | Versioned profile identity used in diagnostics and deployment configuration |
| `transport.headerBytes` | `0` for an unframed payload, or a `2`/`4` byte length header |
| `transport.lengthEncoding` | `binary-big-endian` or decimal `ascii`; omit when `headerBytes` is `0` |
| `transport.maxMessageBytes` | Upper bound checked before allocation or payload acceptance |
| `lengthType` | `fixed`, `llvar`, or `lllvar` |
| `length` | Exact byte count for fixed fields; maximum for variable fields |
| `encoding` | `ascii` or opaque `binary` payload validation |
| `content` | `numeric`, printable `text`, or unrestricted `bytes` validation |
| `sensitive` | Metadata for host redaction/logging policy; the codec never logs fields itself |
| `messages.<MTI>.required` | Fields that must be present for that MTI |

Load once during application startup and fail deployment if the profile is invalid:

```go
file, err := os.Open("profiles/my-acquirer.json")
if err != nil {
    return err
}
defer file.Close()
profile, err := iso8583.LoadProfileJSON(file)
```

```csharp
using var file = File.OpenRead("profiles/my-acquirer.json");
var profile = Profile.LoadJson(file);
```

```cpp
std::ifstream file("profiles/my-acquirer.json");
const auto profile = iso8583::profile::load_json(file);
```

For an inbound transaction, unframe first, parse with the generated codec specification, then apply semantic profile validation:

```go
payload, err := profile.Transport.Unframe(networkFrame)
if err != nil {
    return err
}
message, err := iso8583.Parse(payload, profile.Spec)
if err != nil {
    return err
}
if err := profile.Validate(message); err != nil {
    return err
}
```

For outbound data, call `Validate`, encode with the profile's `Spec`/`MessageSpec`/`message_spec()`, then call `AppendFrame`, `Frame`/`TryWriteFrame`, or `append_frame`. Validation intentionally remains explicit so applications can distinguish malformed wire data from a well-formed message that violates business/profile rules.

Profiles are strict: unknown JSON properties, unsupported MTIs, undefined required fields, duplicate requirements, invalid lengths, non-ASCII data, non-numeric numeric fields, malformed frames, and oversized payloads are rejected. The `sensitive` flag is descriptive metadata; the host must enforce redaction and access controls.

Packed BCD, EBCDIC, TLV subfields, MAC generation/verification, encryption, and key management require institution-specific adapters. They are deliberately not inferred from configuration because silently choosing those rules would be unsafe.

### Encoding internals

1. Validate the four-byte MTI and every present field against the configured specification.
2. Scan fields 65-128 to decide whether a secondary bitmap is required.
3. Append the MTI and reserve 8 or 16 zeroed bitmap bytes.
4. Visit present fields in ascending order, set their bitmap bits, write any LLVAR/LLLVAR prefix, and append their payloads.
5. On an error, do not return a partial message. Go and C++ restore the caller's destination to its original length; C# validates the required length before writing.

### Decoding internals

1. Require enough bytes for the MTI and primary bitmap.
2. Read bit 1 to determine whether a complete secondary bitmap is available.
3. Visit each set bit in field order and look up its field specification.
4. Parse and validate variable-length prefixes, then bounds-check before reading payload bytes.
5. Reject undefined fields, malformed lengths, truncation, and trailing bytes.

Strict trailing-byte rejection catches mismatched field dictionaries early. If a network adds a transport header or length prefix, remove that framing before calling `Parse`.

### Memory and concurrency

- **Go:** `AppendTo` reuses caller-owned slice capacity. Parsed fields are zero-copy views into the input buffer, so that buffer must remain unchanged while the message is in use.
- **C#:** `TryWrite` targets a caller-provided `Span<byte>`. Parsing copies field payloads into the message, decoupling their lifetime from the input span.
- **C++:** `append_to` reuses `std::vector` capacity. Parsing stores owned strings in the returned message.
- C# and C++ specifications expose no mutation API. Go's `Spec.Fields` is exported for direct access, so applications must treat it as immutable after startup.
- Messages are mutable and should be scoped to one transaction unless externally synchronized.

## Financial authorization example

The test suite models a card-present purchase using synthetic values only:

```mermaid
sequenceDiagram
    participant POS as POS terminal
    participant Acquirer as Acquiring institution
    POS->>Acquirer: 0100 authorization request ($125.00, STAN 123456)
    Acquirer-->>POS: 0110 response (code 00, authorization A12345)
```

The request carries a synthetic test PAN, processing code, amount, timestamps, merchant context, acquirer ID, RRN, terminal/merchant IDs, and USD currency code. The response echoes the transaction identifiers and adds field 38 (authorization ID) and field 39 (`00`, approved in this example profile). The test verifies both wire round trips and STAN/RRN correlation; it does not contact a payment network or perform issuer risk checks.

Run the scenario in Go:

```sh
cd go
go test ./iso8583 -run TestCardPurchaseAuthorizationFlow -v
```

## Layout

| Directory | Package | Minimum target |
| --- | --- | --- |
| `go/` | `github.com/michaelnwanna/iso8583-core/go/iso8583` | Go 1.23 |
| `csharp/` | `Iso8583.Core` | .NET 8 |
| `cpp/` | `iso8583::core` | C++17 |

## How to use the library

The same workflow applies in every language: define a profile, construct a message, set byte fields, encode it, and parse the resulting payload with the same profile. The examples use synthetic values.

### Go

Add the module:

```sh
go get github.com/michaelnwanna/iso8583-core/go
```

```go
import "github.com/michaelnwanna/iso8583-core/go/iso8583"

spec, err := iso8583.NewSpec(map[int]iso8583.FieldSpec{
    2: {LengthType: iso8583.LLVAR, Length: 19},
    3: {LengthType: iso8583.Fixed, Length: 6},
    4: {LengthType: iso8583.Fixed, Length: 12},
})
if err != nil {
    return err
}

request, err := iso8583.NewMessage("0100")
if err != nil {
    return err
}
if err := request.SetField(2, []byte("4111111111111111")); err != nil {
    return err
}
_ = request.SetField(3, []byte("000000"))
_ = request.SetField(4, []byte("000000012500"))

wire, err := request.AppendTo(make([]byte, 0, 128), spec)
if err != nil {
    return err
}

decoded, err := iso8583.Parse(wire, spec)
if err != nil {
    return err
}
amount := string(decoded.Fields[4]) // "000000012500" means 125.00 for a 2-decimal currency.
```

`AppendTo` can append to an existing transport buffer. Preserve the input byte slice while using fields from a parsed message because parsing is zero-copy.

Run tests and benchmarks:

```sh
cd go
go test ./...
go test -run '^$' -bench . -benchmem ./...
```

### C#

Until a NuGet package is published, reference `csharp/src/Iso8583.Core/Iso8583.Core.csproj` from the consuming solution.

```csharp
using System.Text;
using Iso8583.Core;

var spec = new MessageSpec(new Dictionary<int, FieldSpec>
{
    [2] = new(LengthType.Llvar, 19),
    [3] = new(LengthType.Fixed, 6),
    [4] = new(LengthType.Fixed, 12)
});

var request = new Message("0100");
request.SetField(2, "4111111111111111"u8);
request.SetField(3, "000000"u8);
request.SetField(4, "000000012500"u8);

Span<byte> buffer = stackalloc byte[128];
if (!request.TryWrite(buffer, spec, out var bytesWritten))
{
    throw new InvalidOperationException("Destination buffer is too small.");
}

var wire = buffer[..bytesWritten];
var decoded = Message.Parse(wire, spec);
var amount = Encoding.ASCII.GetString(decoded.GetField(4).Span);
```

Use `Encode` when convenience matters more than one output allocation. Use `TryWrite` in a hot path or when writing into a pooled/network buffer.

Run the test executable:

```sh
dotnet run --project csharp/tests/Iso8583.Core.Tests -c Release
```

### C++

Add the repository with `add_subdirectory`, then link the exported target:

```cmake
add_subdirectory(path/to/Iso8583lib/cpp)
target_link_libraries(your_target PRIVATE iso8583::core)
```

```cpp
#include <cstdint>
#include <vector>

#include "iso8583/codec.hpp"

const iso8583::spec spec({
    {2, {iso8583::length_type::llvar, 19}},
    {3, {iso8583::length_type::fixed, 6}},
    {4, {iso8583::length_type::fixed, 12}},
});

iso8583::message request("0100");
request.set_field(2, "4111111111111111");
request.set_field(3, "000000");
request.set_field(4, "000000012500");

std::vector<std::uint8_t> wire;
wire.reserve(128);
request.append_to(wire, spec);

const auto decoded = iso8583::parse(wire, spec);
const auto amount = decoded.field(4);
```

Catch `std::invalid_argument` for malformed messages or values and `std::out_of_range` for invalid field numbers. Reserve or reuse vector capacity in transaction loops.

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

ISO 8583 deployments differ by network and version. Define the exact field dictionary required by your host and test it against approved network vectors. The included PAN and identifiers are synthetic test values.

Do not log PAN, PIN, track, CVV, cryptograms, or key material. Production systems also need transport framing, timeouts, idempotency, reversal handling, key management, MAC verification, encryption, audit controls, observability with redaction, and PCI DSS controls. This project is a codec foundation, not a PCI DSS compliance boundary or a network-certified message profile.

## License

MIT