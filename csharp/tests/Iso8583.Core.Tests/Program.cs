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

static void Assert(bool condition, string message)
{
    if (!condition)
    {
        throw new Exception(message);
    }
}