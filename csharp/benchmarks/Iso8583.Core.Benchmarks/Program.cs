using System.Diagnostics;
using Iso8583.Core;

var iterations = args.Length == 0 ? 1_000_000 : int.Parse(args[0]);
if (iterations <= 0)
{
    throw new ArgumentOutOfRangeException(nameof(iterations));
}

var spec = new MessageSpec(new Dictionary<int, FieldSpec>
{
    [2] = new(LengthType.Llvar, 19),
    [3] = new(LengthType.Fixed, 6),
    [4] = new(LengthType.Fixed, 12),
    [11] = new(LengthType.Fixed, 6),
    [41] = new(LengthType.Fixed, 8)
});
var message = new Message("0100");
message.SetField(2, "4111111111111111"u8);
message.SetField(3, "000000"u8);
message.SetField(4, "000000012500"u8);
message.SetField(11, "123456"u8);
message.SetField(41, "TERM0001"u8);
var buffer = new byte[128];
_ = message.TryWrite(buffer, spec, out var payloadLength);

for (var index = 0; index < 10_000; index++)
{
    _ = message.TryWrite(buffer, spec, out _);
    _ = Message.Parse(buffer.AsSpan(0, payloadLength), spec);
}

var allocatedBefore = GC.GetAllocatedBytesForCurrentThread();
var stopwatch = Stopwatch.StartNew();
for (var index = 0; index < iterations; index++)
{
    if (!message.TryWrite(buffer, spec, out var written))
    {
        throw new InvalidOperationException("Benchmark buffer is too small.");
    }
    _ = Message.Parse(buffer.AsSpan(0, written), spec);
}
stopwatch.Stop();
var allocated = GC.GetAllocatedBytesForCurrentThread() - allocatedBefore;
var nanoseconds = stopwatch.Elapsed.TotalNanoseconds / iterations;

Console.WriteLine($"roundtrip: {nanoseconds:F1} ns/op, {allocated / (double)iterations:F1} B/op, {iterations} iterations");