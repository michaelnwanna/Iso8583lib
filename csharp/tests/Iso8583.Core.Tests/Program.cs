using System.Text;
using Iso8583.Core;

var spec = new MessageSpec(new Dictionary<int, FieldSpec>
{
    [2] = new(LengthType.Llvar, 19),
    [3] = new(LengthType.Fixed, 6),
    [4] = new(LengthType.Fixed, 12),
    [70] = new(LengthType.Fixed, 3)
});

var original = new Message("0800");
original.SetField(2, "4761739001010010"u8);
original.SetField(3, "000000"u8);
original.SetField(4, "000000001000"u8);
original.SetField(70, "301"u8);

var encoded = original.Encode(spec);
var decoded = Message.Parse(encoded, spec);

Assert(decoded.Mti == "0800", "MTI did not round trip");
Assert(Encoding.ASCII.GetString(decoded.GetField(2).Span) == "4761739001010010", "Field 2 did not round trip");
Assert(Encoding.ASCII.GetString(decoded.GetField(70).Span) == "301", "Field 70 did not round trip");

try
{
    _ = Message.Parse(encoded.AsSpan(0, encoded.Length - 1), spec);
    throw new Exception("Truncated field was accepted.");
}
catch (FormatException)
{
}

var financialSpec = new MessageSpec(new Dictionary<int, FieldSpec>
{
    [2] = new(LengthType.Llvar, 19), [3] = new(LengthType.Fixed, 6),
    [4] = new(LengthType.Fixed, 12), [7] = new(LengthType.Fixed, 10),
    [11] = new(LengthType.Fixed, 6), [12] = new(LengthType.Fixed, 6),
    [13] = new(LengthType.Fixed, 4), [14] = new(LengthType.Fixed, 4),
    [18] = new(LengthType.Fixed, 4), [22] = new(LengthType.Fixed, 3),
    [25] = new(LengthType.Fixed, 2), [32] = new(LengthType.Llvar, 11),
    [37] = new(LengthType.Fixed, 12), [38] = new(LengthType.Fixed, 6),
    [39] = new(LengthType.Fixed, 2), [41] = new(LengthType.Fixed, 8),
    [42] = new(LengthType.Fixed, 15), [49] = new(LengthType.Fixed, 3)
});
var request = new Message("0100");
request.SetField(2, "4111111111111111"u8);
request.SetField(3, "000000"u8);
request.SetField(4, "000000012500"u8);
request.SetField(7, "1014123456"u8);
request.SetField(11, "123456"u8);
request.SetField(12, "123456"u8);
request.SetField(13, "1014"u8);
request.SetField(14, "2912"u8);
request.SetField(18, "5411"u8);
request.SetField(22, "051"u8);
request.SetField(25, "00"u8);
request.SetField(32, "12345678901"u8);
request.SetField(37, "628812345678"u8);
request.SetField(41, "TERM0001"u8);
request.SetField(42, "MERCHANT0000001"u8);
request.SetField(49, "840"u8);

var acquirerRequest = Message.Parse(request.Encode(financialSpec), financialSpec);
Assert(acquirerRequest.Mti == "0100", "Acquirer received the wrong MTI");
Assert(Encoding.ASCII.GetString(acquirerRequest.GetField(4).Span) == "000000012500", "Amount did not round trip");
Assert(Encoding.ASCII.GetString(acquirerRequest.GetField(41).Span) == "TERM0001", "Terminal ID did not round trip");

var response = new Message("0110");
response.SetField(3, "000000"u8);
response.SetField(4, "000000012500"u8);
response.SetField(11, "123456"u8);
response.SetField(37, "628812345678"u8);
response.SetField(38, "A12345"u8);
response.SetField(39, "00"u8);
response.SetField(41, "TERM0001"u8);
response.SetField(42, "MERCHANT0000001"u8);
response.SetField(49, "840"u8);

var terminalResponse = Message.Parse(response.Encode(financialSpec), financialSpec);
Assert(terminalResponse.Mti == "0110", "Terminal received the wrong MTI");
Assert(Encoding.ASCII.GetString(terminalResponse.GetField(39).Span) == "00", "Authorization was not approved");
Assert(Encoding.ASCII.GetString(terminalResponse.GetField(38).Span) == "A12345", "Authorization ID did not round trip");
Assert(terminalResponse.GetField(37).Span.SequenceEqual(acquirerRequest.GetField(37).Span), "RRN did not correlate");

using var profileFile = File.OpenRead(Path.Combine(AppContext.BaseDirectory, "example-acquirer.json"));
var configuredProfile = Profile.LoadJson(profileFile);
configuredProfile.Validate(request);
var configuredPayload = request.Encode(configuredProfile.Spec);
var configuredFrame = configuredProfile.Transport.Frame(configuredPayload);
var configuredMessage = Message.Parse(configuredProfile.Transport.Unframe(configuredFrame), configuredProfile.Spec);
configuredProfile.Validate(configuredMessage);
Assert(configuredProfile.GetFieldDefinition(2).Sensitive, "PAN metadata is not marked sensitive");

foreach (var vector in LoadVectors(Path.Combine(AppContext.BaseDirectory, "conformance.vectors")))
{
    var vectorMessage = new Message(vector.Mti);
    foreach (var (number, value) in vector.Fields)
    {
        vectorMessage.SetField(number, Encoding.ASCII.GetBytes(value));
    }

    configuredProfile.Validate(vectorMessage);
    var vectorPayload = vectorMessage.Encode(configuredProfile.Spec);
    Assert(vectorPayload.AsSpan().SequenceEqual(vector.Payload), $"{vector.Name}: payload differs from golden vector");
    Assert(configuredProfile.Transport.Frame(vectorPayload).AsSpan().SequenceEqual(vector.Frame), $"{vector.Name}: frame differs from golden vector");

    var vectorDecoded = Message.Parse(vector.Payload, configuredProfile.Spec);
    configuredProfile.Validate(vectorDecoded);
    foreach (var (number, value) in vector.Fields)
    {
        Assert(vectorDecoded.GetField(number).Span.SequenceEqual(Encoding.ASCII.GetBytes(value)), $"{vector.Name}: field {number} differs");
    }
}

var incompleteRequest = new Message("0100");
incompleteRequest.SetField(2, "4111111111111111"u8);
try
{
    configuredProfile.Validate(incompleteRequest);
    throw new Exception("Profile accepted a request missing required fields.");
}
catch (InvalidOperationException)
{
}

var bcdProfile = Profile.ParseJson("""
{
  "name":"bcd",
  "transport":{"headerBytes":2,"lengthEncoding":"binary-big-endian","maxMessageBytes":128},
  "fields":{"2":{"name":"PAN","lengthType":"llvar","length":19,"encoding":"bcd","lengthEncoding":"bcd","content":"numeric"}},
  "messages":{"0100":{"required":[2]}}
}
"""u8);
var bcdMessage = new Message("0100");
bcdMessage.SetField(2, "123456789012345"u8);
bcdProfile.Validate(bcdMessage);
var bcdWire = bcdMessage.Encode(bcdProfile.Spec);
Assert(bcdWire.AsSpan(12).SequenceEqual(new byte[] { 0x15, 0x01, 0x23, 0x45, 0x67, 0x89, 0x01, 0x23, 0x45 }),
    "Packed BCD wire data differs");
Assert(Message.Parse(bcdWire, bcdProfile.Spec).GetField(2).Span.SequenceEqual("123456789012345"u8),
    "Packed BCD field did not round trip");

var firstFrame = bcdProfile.Transport.Frame("first"u8);
var secondFrame = bcdProfile.Transport.Frame("second"u8);
var stream = firstFrame.Concat(secondFrame).ToArray();
Assert(!bcdProfile.Transport.TryReadFrame(stream.AsSpan(0, 4), out _, out _), "Partial frame was reported complete");
Assert(bcdProfile.Transport.TryReadFrame(stream, out var firstPayload, out var firstConsumed) &&
    firstPayload.SequenceEqual("first"u8) && firstConsumed == firstFrame.Length, "First stream frame differs");
Assert(bcdProfile.Transport.TryReadFrame(stream.AsSpan(firstConsumed), out var secondPayload, out var secondConsumed) &&
    secondPayload.SequenceEqual("second"u8) && secondConsumed == secondFrame.Length, "Second stream frame differs");

static List<ConformanceVector> LoadVectors(string path)
{
    var vectors = new List<ConformanceVector>();
    foreach (var line in File.ReadLines(path))
    {
        if (line.Length == 0 || line.StartsWith('#'))
        {
            continue;
        }

        var parts = line.Split('|');
        if (parts.Length != 5)
        {
            throw new FormatException($"Invalid conformance vector: {line}");
        }

        var fields = new Dictionary<int, string>();
        foreach (var pair in parts[2].Split(';'))
        {
            var separator = pair.IndexOf('=');
            if (separator <= 0)
            {
                throw new FormatException($"Invalid vector field: {pair}");
            }

            fields.Add(int.Parse(pair.AsSpan(0, separator)), pair[(separator + 1)..]);
        }

        vectors.Add(new ConformanceVector(
            parts[0], parts[1], fields, Convert.FromHexString(parts[3]), Convert.FromHexString(parts[4])));
    }

    return vectors;
}

static void Assert(bool condition, string message)
{
    if (!condition)
    {
        throw new Exception(message);
    }
}

internal sealed record ConformanceVector(
    string Name,
    string Mti,
    IReadOnlyDictionary<int, string> Fields,
    byte[] Payload,
    byte[] Frame);