using System.Buffers;
using System.Text;

namespace Iso8583.Core;

public enum LengthType : byte
{
    Fixed,
    Llvar,
    Lllvar
}

public enum FieldEncoding : byte { Ascii, Binary, PackedBcd }
public enum FieldLengthEncoding : byte { Ascii, Bcd }

public readonly record struct FieldSpec(
    LengthType LengthType,
    int Length,
    FieldEncoding Encoding = FieldEncoding.Ascii,
    FieldLengthEncoding LengthEncoding = FieldLengthEncoding.Ascii);

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
                field.LengthType is LengthType.Lllvar && field.Length > 999 ||
                field.LengthType is LengthType.Fixed && field.LengthEncoding != FieldLengthEncoding.Ascii)
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
            length += WireLength(value.Length, field.Encoding) + PrefixLength(field);
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
            var prefix = PrefixLength(field);
            if (prefix != 0)
            {
                WriteLength(destination.Slice(offset, prefix), value.Length, field.LengthEncoding);
                offset += prefix;
            }

            if (field.Encoding == FieldEncoding.PackedBcd)
            {
                WritePackedBcd(value, destination[offset..]);
            }
            else
            {
                value.CopyTo(destination[offset..]);
            }
            offset += WireLength(value.Length, field.Encoding);
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

            var prefix = PrefixLength(field);
            var length = field.Length;
            if (prefix != 0)
            {
                length = ParseLength(data, offset, prefix, field.LengthEncoding);
                offset += prefix;
                if (length > field.Length)
                {
                    throw new FormatException($"Field {number} exceeds its maximum length.");
                }
            }

            var wireLength = WireLength(length, field.Encoding);
            if (offset > data.Length || wireLength > data.Length - offset)
            {
                throw new FormatException($"Field {number} is truncated.");
            }

            output.fields[number] = field.Encoding == FieldEncoding.PackedBcd
                ? ReadPackedBcd(data.Slice(offset, wireLength), length)
                : data.Slice(offset, wireLength).ToArray();
            offset += wireLength;
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

        if (field.Encoding == FieldEncoding.PackedBcd)
        {
            var value = fields[number]!;
            if (value.Any(digit => digit is < (byte)'0' or > (byte)'9'))
            {
                throw new InvalidOperationException($"Field {number} is not numeric.");
            }
        }

        return field;
    }

    private static int PrefixLength(FieldSpec field)
    {
        if (field.LengthType == LengthType.Fixed)
        {
            return 0;
        }
        var digits = field.LengthType == LengthType.Llvar ? 2 : 3;
        return field.LengthEncoding == FieldLengthEncoding.Bcd ? (digits + 1) / 2 : digits;
    }

    private static void WriteLength(Span<byte> destination, int length, FieldLengthEncoding encoding)
    {
        if (encoding == FieldLengthEncoding.Bcd)
        {
            for (var index = destination.Length - 1; index >= 0; index--)
            {
                destination[index] = (byte)(length % 10 | length / 10 % 10 << 4);
                length /= 100;
            }
            return;
        }
        for (var index = destination.Length - 1; index >= 0; index--)
        {
            destination[index] = (byte)('0' + length % 10);
            length /= 10;
        }
    }

    private static int ParseLength(ReadOnlySpan<byte> data, int offset, int bytes, FieldLengthEncoding encoding)
    {
        if (offset > data.Length || bytes > data.Length - offset)
        {
            throw new FormatException("Field length is truncated.");
        }

        var length = 0;
        if (encoding == FieldLengthEncoding.Bcd)
        {
            var bcdLength = 0;
            foreach (var value in data.Slice(offset, bytes))
            {
                if ((value >> 4) > 9 || (value & 0x0f) > 9)
                {
                    throw new FormatException("Field length is not decimal BCD.");
                }
                bcdLength = bcdLength * 100 + (value >> 4) * 10 + (value & 0x0f);
            }
            return bcdLength;
        }

        foreach (var digit in data.Slice(offset, bytes))
        {
            if (digit is < (byte)'0' or > (byte)'9')
            {
                throw new FormatException("Field length is not decimal.");
            }

            length = length * 10 + digit - '0';
        }

        return length;
    }

    private static int WireLength(int digits, FieldEncoding encoding) =>
        encoding == FieldEncoding.PackedBcd ? (digits + 1) / 2 : digits;

    private static void WritePackedBcd(ReadOnlySpan<byte> value, Span<byte> destination)
    {
        var source = 0;
        var target = 0;
        if (value.Length % 2 != 0)
        {
            destination[target++] = (byte)(value[source++] - '0');
        }
        while (source < value.Length)
        {
            destination[target++] = (byte)((value[source++] - '0') << 4 | value[source++] - '0');
        }
    }

    private static byte[] ReadPackedBcd(ReadOnlySpan<byte> data, int digits)
    {
        var output = new byte[digits];
        var target = 0;
        for (var index = 0; index < data.Length; index++)
        {
            var high = data[index] >> 4;
            var low = data[index] & 0x0f;
            if (high > 9 || low > 9 || digits % 2 != 0 && index == 0 && high != 0)
            {
                throw new FormatException("Field contains invalid packed BCD data.");
            }
            if (digits % 2 == 0 || index != 0)
            {
                output[target++] = (byte)('0' + high);
            }
            output[target++] = (byte)('0' + low);
        }
        return output;
    }
}