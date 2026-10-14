# Conformance and robustness testing

The test strategy has three layers:

1. Unit and scenario tests verify local API behavior and the synthetic financial authorization flow.
2. Shared golden vectors verify byte-for-byte compatibility across Go, C#, and C++.
3. Go fuzz targets mutate untrusted wire/configuration inputs and check invariants.

## Golden vector format

[`conformance.vectors`](../testdata/conformance.vectors) is line-oriented and intentionally requires no parser dependency:

```text
name|MTI|field=value;field=value|payload-hex|framed-hex
```

- Empty lines and lines beginning with `#` are ignored.
- `name` must be unique and stable.
- `MTI` is the four-byte message type.
- Fields are decimal numbers with ASCII values, ordered numerically for readability.
- Payload hex is uppercase hexadecimal for the MTI, bitmap(s), length prefixes, and fields.
- Framed hex is the configured transport header followed by the exact payload.
- `|`, `;`, and `=` are reserved delimiters and cannot appear in field values.

The current file uses [`example-acquirer.json`](../profiles/example-acquirer.json) and includes ASCII plus packed-BCD wire cases. If a vector needs arbitrary binary field data or delimiter characters, version the format before changing parser behavior in all three languages.

## Adding a vector

1. Obtain or construct a synthetic, non-sensitive message covered by the example profile.
2. Calculate expected bitmap and wire bytes independently of the codec under test. Do not copy output from one implementation and call it authoritative without review.
3. Add one line to the shared file.
4. Run all three test suites. Each suite loads the same line and checks encode, frame, parse, and field recovery.
5. Review the hex diff carefully. A changed existing vector is a wire compatibility change and should not be accepted merely to make a test pass.

Never commit production PANs, track data, cryptograms, PIN blocks, MAC keys, or institution-owned certification vectors without permission.

## Running tests

### Go

```sh
cd go
go test -race ./...
go vet ./...
```

### C#

```sh
dotnet run --project csharp/tests/Iso8583.Core.Tests -c Release
```

### C++

```sh
cmake -S cpp -B cpp/build -DCMAKE_BUILD_TYPE=Release
cmake --build cpp/build --parallel
ctest --test-dir cpp/build --output-on-failure
```

## Fuzzing

Go's built-in fuzzing is used because it is reproducible, dependency-free, and persists newly discovered crash inputs in the standard corpus format.

### Payload parser

```sh
cd go
go test ./iso8583 -run '^$' -fuzz '^FuzzParse$' -fuzztime 1m
```

Seeds include every golden payload. If random data parses and passes profile validation, canonical re-encoding must preserve the MTI and every field, and a second encoding must produce identical canonical bytes. The original bytes may differ when the input contains a redundant empty secondary bitmap.

### Profile parser

```sh
go test ./iso8583 -run '^$' -fuzz '^FuzzParseProfileJSON$' -fuzztime 1m
```

Seeds include the valid example profile and malformed minimal documents. Any accepted profile must preserve construction invariants.

### Transport parser

```sh
go test ./iso8583 -run '^$' -fuzz '^FuzzTransportUnframe$' -fuzztime 1m
```

Seeds include all golden frames. Any accepted frame must survive unframe/frame without changing bytes.

GitHub Actions runs each target for five seconds on every push and pull request. Longer fuzz campaigns should run before releases and after parser changes.

## Failure handling

When fuzzing writes a failing corpus entry:

1. Keep the generated input and reproduce it with the exact command printed by Go.
2. Decide whether the failure is a crash, excessive resource use, or a violated canonicalization invariant.
3. Fix the parser boundary rather than filtering only that input.
4. Keep the minimized regression corpus entry unless it contains sensitive data.
5. Rerun unit, conformance, race, and fuzz checks.

Cross-language native fuzz harnesses are not yet included. C# and C++ still receive malformed-input unit coverage and all shared golden vectors; dedicated SharpFuzz/libFuzzer jobs are a future hardening step.