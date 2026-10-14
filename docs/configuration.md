# Host configuration guide

This guide describes how a host application configures and operates ISO 8583 Core. The profile is a deployment-time contract between your application and a specific acquirer, issuer, switch, or processor.

The included profile is illustrative. Replace it with values from the implementation guide supplied by your institution. ISO 8583 editions and network profiles are not interchangeable.

## Configuration lifecycle

1. Create one versioned JSON profile per endpoint contract.
2. Validate it against [`profile.schema.json`](../profiles/profile.schema.json) during development or deployment.
3. Load it once when the application starts. Treat loading errors as deployment failures.
4. Share the loaded profile across transactions. Do not mutate its generated codec specification.
5. For inbound data: extract one frame, unframe it, parse it, then validate it.
6. For outbound data: build a message, validate it, encode it, then frame it.
7. Deploy profile changes with approved conformance vectors and an explicit version/rollback plan.

## Schema reference

### Root object

| Property | Required | Description |
| --- | --- | --- |
| `name` | Yes | Non-empty, versioned identifier such as `bank-switch-v3` |
| `transport` | Yes | Payload length-header configuration and maximum size |
| `fields` | Yes | Definitions keyed by decimal field number from 2 through 128 |
| `messages` | Yes | Required-field rules keyed by four-character MTI |

Unknown properties are rejected by every implementation. This catches misspelled configuration rather than silently ignoring it.

### Transport

```json
{
    "headerBytes": 2,
    "lengthEncoding": "binary-big-endian",
    "maxMessageBytes": 4096
}
```

`headerBytes` accepts `0`, `2`, or `4`:

- `0`: no library-managed length header. Omit `lengthEncoding`.
- `2` or `4`: prefix the ISO 8583 payload with that many length bytes.

`lengthEncoding` accepts:

- `binary-big-endian`: unsigned network-byte-order length.
- `ascii`: zero-padded decimal length occupying the configured header width.

The declared length counts only the ISO 8583 payload, not the header itself. `maxMessageBytes` is enforced for inbound and outbound data. `Unframe`/`unframe` validates one complete frame. `TryReadFrame`/`try_read_frame` instead examines a receive buffer, returns incomplete without consuming bytes, or returns one zero-copy payload plus the consumed byte count so the caller can retain a partial or following frame.

### Fields

```json
"2": {
    "name": "Primary account number",
    "lengthType": "llvar",
    "length": 19,
    "encoding": "ascii",
    "content": "numeric",
    "sensitive": true
}
```

| Property | Values | Behavior |
| --- | --- | --- |
| `name` | Non-empty string | Operational name available as metadata |
| `lengthType` | `fixed`, `llvar`, `lllvar` | Selects exact length or a 2/3-digit logical length prefix |
| `length` | Positive integer | Exact logical units for `fixed`; maximum for variable fields |
| `encoding` | `ascii`, `binary`, `bcd` | ASCII validates bytes, binary is opaque, BCD packs numeric digits |
| `lengthEncoding` | `ascii`, `bcd`, optional | Encodes an LLVAR/LLLVAR length as text or packed decimal; defaults to ASCII |
| `content` | `numeric`, `text`, `bytes` | Digits only, printable ASCII, or unrestricted bytes |
| `sensitive` | Boolean, optional | Marks data that the host must redact and protect |

`binary` encoding must use `bytes` content and `bcd` must use `numeric`. LLVAR supports a maximum logical length of 99 and LLLVAR 999. ASCII prefixes occupy two or three bytes; BCD prefixes occupy one or two bytes. A BCD field's `length` counts digits, not packed bytes. Odd-digit values are left-padded with a zero nibble on the wire, and the decoder rejects non-decimal or non-zero padding nibbles.

Message APIs continue to accept and return unpacked ASCII digits for BCD fields. Convert EBCDIC and encode nested TLV structures before setting those fields; decode those institution-specific forms after parsing.

### Message rules

```json
"0100": {
    "required": [2, 3, 4, 7, 11, 22, 37, 41, 42, 49]
}
```

Only configured MTIs are accepted by profile validation. Every required field must also exist in `fields`. Duplicate and undefined required field numbers are rejected when the profile loads.

Required-field rules define structural presence, not every business condition. Rules such as “field 35 is required only for magnetic-stripe entry” belong in the host's business/profile extension layer.

## Go

### Startup

```go
file, err := os.Open("profiles/acquirer.json")
if err != nil {
    return err
}
defer file.Close()

profile, err := iso8583.LoadProfileJSON(file)
if err != nil {
    return fmt.Errorf("load ISO 8583 profile: %w", err)
}
```

Use `ParseProfileJSON` when configuration already exists as a byte slice.

### Inbound

```go
payload, err := profile.Transport.Unframe(frame)
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

Parsed Go fields reference the payload bytes. Keep the payload unchanged while the message is in use.

For a chunked or coalesced TCP receive buffer:

```go
payload, consumed, complete, err := profile.Transport.TryReadFrame(receiveBuffer)
if err != nil {
    return err
}
if !complete {
    return nil // retain the buffer and read more bytes
}
receiveBuffer = receiveBuffer[consumed:]
```

### Outbound

```go
message, err := iso8583.NewMessage("0110")
if err != nil {
    return err
}
_ = message.SetField(3, []byte("000000"))
// Set every field required by the configured 0110 rule.

if err := profile.Validate(message); err != nil {
    return err
}
payload, err := message.AppendTo(payloadBuffer[:0], profile.Spec)
if err != nil {
    return err
}
frame, err := profile.Transport.AppendFrame(frameBuffer[:0], payload)
```

Reuse buffer capacity in transaction loops. Both append operations preserve the destination's original length when validation fails.

## C#

### Startup

```csharp
using var stream = File.OpenRead("profiles/acquirer.json");
var profile = Profile.LoadJson(stream);
```

Use `Profile.ParseJson(ReadOnlySpan<byte>)` for an in-memory document.

### Inbound

```csharp
var payload = profile.Transport.Unframe(frame);
var message = Message.Parse(payload, profile.Spec);
profile.Validate(message);
```

For streaming input, call `TryReadFrame(buffer, out var payload, out var consumed)`. A `false` result means more bytes are required; on success remove `consumed` bytes after parsing the returned span.

### Outbound

```csharp
profile.Validate(message);

Span<byte> payload = stackalloc byte[512];
if (!message.TryWrite(payload, profile.Spec, out var payloadLength))
{
    throw new InvalidOperationException("Payload buffer is too small.");
}

Span<byte> frame = stackalloc byte[514];
if (!profile.Transport.TryWriteFrame(payload[..payloadLength], frame, out var frameLength))
{
    throw new InvalidOperationException("Frame buffer is too small.");
}
```

`Message.Encode` and `TransportSpec.Frame` are allocating convenience alternatives.

## C++

Include both public headers:

```cpp
#include "iso8583/codec.hpp"
#include "iso8583/profile.hpp"
```

### Startup

```cpp
std::ifstream stream("profiles/acquirer.json");
if (!stream) {
    throw std::runtime_error("profile could not be opened");
}
const auto profile = iso8583::profile::load_json(stream);
```

Use `profile::parse_json(std::string_view)` for an in-memory document.

### Inbound

```cpp
const auto payload = profile.transport().unframe(frame);
const auto message = iso8583::parse(
    reinterpret_cast<const std::uint8_t*>(payload.data()),
    payload.size(),
    profile.message_spec());
profile.validate(message);
```

The returned unframed `string_view` references the frame. Keep the frame alive and unchanged until parsing completes.

For streaming input, call `try_read_frame(data, size, payload, consumed)`. A `false` result means more bytes are required; on success retain the receive buffer while using the returned `string_view`, then remove `consumed` bytes.

### Outbound

```cpp
profile.validate(message);

std::vector<std::uint8_t> payload;
payload.reserve(512);
message.append_to(payload, profile.message_spec());

std::vector<std::uint8_t> frame;
frame.reserve(profile.transport().header_bytes() + payload.size());
profile.transport().append_frame(frame, payload);
```

## Errors and observability

Treat failures in three categories:

| Stage | Examples | Recommended handling |
| --- | --- | --- |
| Startup profile load | Unknown key, invalid field length, undefined required field | Fail startup/deployment; do not serve traffic with a partial profile |
| Wire decoding/framing | Truncation, malformed length, unknown bitmap field, trailing bytes | Reject the message and record a sanitized protocol metric |
| Semantic validation | Unsupported MTI, missing required field, non-numeric content | Return the institution-approved error response when safe and possible |

Never include field values in generic error logs. Use field numbers, MTI, profile name, and a correlation identifier that does not contain account data. The `sensitive` metadata is available to build allowlist-based diagnostics, but redaction is the host application's responsibility.

## Deployment checklist

- Give every profile revision a unique `name` and retain the previous known-good file.
- Validate JSON against the schema and load it in all three target implementations in CI.
- Test approved request/response vectors, including secondary bitmaps and maximum lengths.
- Test malformed lengths, missing required fields, unknown MTIs, and oversized frames.
- Confirm whether the institution's transport length includes or excludes its header.
- Confirm field encodings, especially BCD, EBCDIC, binary MACs, and private TLV fields.
- Never use the example profile or synthetic PAN as a production network contract.
- Roll out profile and application changes atomically when their contracts change together.