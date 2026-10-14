using System.Buffers;
using System.Text;

namespace Iso8583.Core;

public enum LengthType : byte
{
    Fixed,
    Llvar,
    Lllvar
}

public readonly record struct FieldSpec(LengthType LengthType, int Length);

public sealed class MessageSpec
{
    internal const int MaxField = 128;
    private readonly FieldSpec[] fields = new FieldSpec[MaxField + 1];

    public MessageSpec(IReadOnlyDictionary<int, FieldSpec> definitions)
    {
        foreach (var (number, field) in definitions)
        {
            CheckNumber(number);
            if (field.Length <= 0 ||
                field.LengthType is LengthType.Llvar && field.Length > 99 ||
                field.LengthType is LengthType.Lllvar && field.Length > 999)
            {
                throw new ArgumentException($"Field {number} has an invalid specification.", nameof(definitions));
            }

            fields[number] = field;
        }
    }

    internal FieldSpec this[int number] => fields[number];

    internal static void CheckNumber(int number)
    {
        if (number is < 2 or > MaxField)
        {
            throw new ArgumentOutOfRangeException(nameof(number), "Field number must be between 2 and 128.");
        }
    }
}

public sealed class Message
{
    private readonly byte[] mti = new byte[4];
    private readonly byte[]?[] fields = new byte[MessageSpec.MaxField + 1][];

    public Message(string mti)
    {
        ArgumentNullException.ThrowIfNull(mti);
        if (Encoding.ASCII.GetByteCount(mti) != 4 || mti.Any(character => character > 0x7f))
        {
            throw new ArgumentException("MTI must contain exactly 4 ASCII bytes.", nameof(mti));
        }

        Encoding.ASCII.GetBytes(mti, this.mti);
    }

    public string Mti => Encoding.ASCII.GetString(mti);

    public ReadOnlyMemory<byte> GetField(int number)
    {
        MessageSpec.CheckNumber(number);
        return fields[number] ?? ReadOnlyMemory<byte>.Empty;
    }

    public void SetField(int number, ReadOnlySpan<byte> value)
    {
        MessageSpec.CheckNumber(number);
        fields[number] = value.ToArray();
    }

    public int GetEncodedLength(MessageSpec spec)
    {
        ArgumentNullException.ThrowIfNull(spec);
        var length = 4 + BitmapLength();
        for (var number = 2; number <= MessageSpec.MaxField; number++)
        {
            var value = fields[number];
            if (value is null || value.Length == 0)
            {
                continue;
            }

            var field = ValidateField(spec, number, value.Length);
            length += value.Length + PrefixLength(field.LengthType);
        }

        return length;
    }

    public bool TryWrite(Span<byte> destination, MessageSpec spec, out int bytesWritten)
    {
        var required = GetEncodedLength(spec);
        if (destination.Length < required)
        {
            bytesWritten = 0;
            return false;
        }

        mti.CopyTo(destination);
        var bitmapLength = BitmapLength();
        var bitmap = destination.Slice(4, bitmapLength);
        bitmap.Clear();
        if (bitmapLength == 16)
        {
            bitmap[0] |= 0x80;
        }

        var offset = 4 + bitmapLength;
        for (var number = 2; number <= MessageSpec.MaxField; number++)
        {
            var value = fields[number];
            if (value is null || value.Length == 0)
            {
                continue;
            }

            var field = spec[number];
            var bit = number - 1;
            bitmap[bit / 8] |= (byte)(1 << (7 - bit % 8));
            var prefix = PrefixLength(field.LengthType);
            if (prefix != 0)
            {
                WriteLength(destination.Slice(offset, prefix), value.Length);
                offset += prefix;
            }

            value.CopyTo(destination[offset..]);
            offset += value.Length;
        }

        bytesWritten = offset;
        return true;
    }

    public byte[] Encode(MessageSpec spec)
    {
        var output = new byte[GetEncodedLength(spec)];
        _ = TryWrite(output, spec, out _);
        return output;
    }

    public static Message Parse(ReadOnlySpan<byte> data, MessageSpec spec)
    {
        ArgumentNullException.ThrowIfNull(spec);
        if (data.Length < 12)
        {
            throw new FormatException("Message is shorter than the MTI and primary bitmap.");
        }

        var output = new Message(Encoding.ASCII.GetString(data[..4]));
        var bitmapLength = (data[4] & 0x80) != 0 ? 16 : 8;
        if (data.Length < 4 + bitmapLength)
        {
            throw new FormatException("Secondary bitmap is truncated.");
        }

        var bitmap = data.Slice(4, bitmapLength);
        var offset = 4 + bitmapLength;
        for (var number = 2; number <= bitmapLength * 8; number++)
        {
            var bit = number - 1;
            if ((bitmap[bit / 8] & (1 << (7 - bit % 8))) == 0)
            {
                continue;
            }

            var field = spec[number];
            if (field.Length == 0)
            {
                throw new FormatException($"Field {number} is not defined.");
            }

            var prefix = PrefixLength(field.LengthType);
            var length = field.Length;
            if (prefix != 0)
            {
                length = ParseLength(data, offset, prefix);
                offset += prefix;
                if (length > field.Length)
                {
                    throw new FormatException($"Field {number} exceeds its maximum length.");
                }
            }

            if (offset > data.Length || length > data.Length - offset)
            {
                throw new FormatException($"Field {number} is truncated.");
            }

            output.fields[number] = data.Slice(offset, length).ToArray();
            offset += length;
        }

        if (offset != data.Length)
        {
            throw new FormatException($"Message has {data.Length - offset} trailing bytes.");
        }

        return output;
    }

    private int BitmapLength()
    {
        for (var number = 65; number <= MessageSpec.MaxField; number++)
        {
            if (fields[number] is { Length: > 0 })
            {
                return 16;
            }
        }

        return 8;
    }

    private FieldSpec ValidateField(MessageSpec spec, int number, int valueLength)
    {
        var field = spec[number];
        if (field.Length == 0)
        {
            throw new InvalidOperationException($"Field {number} is not defined.");
        }

        if (field.LengthType == LengthType.Fixed && valueLength != field.Length ||
            field.LengthType != LengthType.Fixed && valueLength > field.Length)
        {
            throw new InvalidOperationException($"Field {number} has an invalid length.");
        }

        return field;
    }

    private static int PrefixLength(LengthType type) => type switch
    {
        LengthType.Llvar => 2,
        LengthType.Lllvar => 3,
        _ => 0
    };

    private static void WriteLength(Span<byte> destination, int length)
    {
        for (var index = destination.Length - 1; index >= 0; index--)
        {
            destination[index] = (byte)('0' + length % 10);
            length /= 10;
        }
    }

    private static int ParseLength(ReadOnlySpan<byte> data, int offset, int digits)
    {
        if (offset > data.Length || digits > data.Length - offset)
        {
            throw new FormatException("Field length is truncated.");
        }

        var length = 0;
        foreach (var digit in data.Slice(offset, digits))
        {
            if (digit is < (byte)'0' or > (byte)'9')
            {
                throw new FormatException("Field length is not decimal.");
            }

            length = length * 10 + digit - '0';
        }

        return length;
    }
}