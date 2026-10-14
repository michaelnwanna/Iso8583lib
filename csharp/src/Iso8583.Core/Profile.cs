using System.Buffers.Binary;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace Iso8583.Core;

public enum FieldContent : byte { Numeric, Text, Bytes }
public enum LengthHeaderEncoding : byte { None, BinaryBigEndian, Ascii }

public sealed record FieldDefinition(
    FieldSpec FieldSpec,
    string Name,
    FieldEncoding Encoding,
    FieldContent Content,
    bool Sensitive);

public sealed record MessageRule(IReadOnlyList<int> Required);

public sealed class TransportSpec
{
    internal TransportSpec(int headerBytes, LengthHeaderEncoding lengthEncoding, int maxMessageBytes)
    {
        HeaderBytes = headerBytes;
        LengthEncoding = lengthEncoding;
        MaxMessageBytes = maxMessageBytes;
    }

    public int HeaderBytes { get; }
    public LengthHeaderEncoding LengthEncoding { get; }
    public int MaxMessageBytes { get; }

    public byte[] Frame(ReadOnlySpan<byte> payload)
    {
        var frame = new byte[HeaderBytes + payload.Length];
        _ = TryWriteFrame(payload, frame, out _);
        return frame;
    }

    public bool TryWriteFrame(ReadOnlySpan<byte> payload, Span<byte> destination, out int bytesWritten)
    {
        if (payload.Length > MaxMessageBytes)
        {
            throw new InvalidOperationException($"Payload exceeds {MaxMessageBytes} bytes.");
        }

        var required = HeaderBytes + payload.Length;
        if (destination.Length < required)
        {
            bytesWritten = 0;
            return false;
        }

        WriteLength(destination[..HeaderBytes], payload.Length);
        payload.CopyTo(destination[HeaderBytes..]);
        bytesWritten = required;
        return true;
    }

    public ReadOnlySpan<byte> Unframe(ReadOnlySpan<byte> frame)
    {
        if (HeaderBytes == 0)
        {
            if (frame.Length > MaxMessageBytes)
            {
                throw new FormatException($"Payload exceeds {MaxMessageBytes} bytes.");
            }

            return frame;
        }

        if (frame.Length < HeaderBytes)
        {
            throw new FormatException("Transport header is truncated.");
        }

        var length = ReadLength(frame[..HeaderBytes]);
        if (length > MaxMessageBytes)
        {
            throw new FormatException($"Framed payload exceeds {MaxMessageBytes} bytes.");
        }

        if (frame.Length - HeaderBytes != length)
        {
            throw new FormatException($"Frame declares {length} payload bytes, got {frame.Length - HeaderBytes}.");
        }

        return frame[HeaderBytes..];
    }

    public bool TryReadFrame(ReadOnlySpan<byte> data, out ReadOnlySpan<byte> payload, out int consumed)
    {
        payload = default;
        consumed = 0;
        if (HeaderBytes == 0)
        {
            if (data.IsEmpty)
            {
                return false;
            }
            if (data.Length > MaxMessageBytes)
            {
                throw new FormatException($"Payload exceeds {MaxMessageBytes} bytes.");
            }
            payload = data;
            consumed = data.Length;
            return true;
        }
        if (data.Length < HeaderBytes)
        {
            return false;
        }
        var length = ReadLength(data[..HeaderBytes]);
        if (length > MaxMessageBytes)
        {
            throw new FormatException($"Framed payload exceeds {MaxMessageBytes} bytes.");
        }
        var required = HeaderBytes + length;
        if (data.Length < required)
        {
            return false;
        }
        payload = data.Slice(HeaderBytes, length);
        consumed = required;
        return true;
    }

    private void WriteLength(Span<byte> header, int length)
    {
        if (HeaderBytes == 0)
        {
            return;
        }

        if (LengthEncoding == LengthHeaderEncoding.BinaryBigEndian)
        {
            if (HeaderBytes == 2)
            {
                if (length > ushort.MaxValue)
                {
                    throw new InvalidOperationException("Payload does not fit the transport header.");
                }

                BinaryPrimitives.WriteUInt16BigEndian(header, (ushort)length);
            }
            else
            {
                BinaryPrimitives.WriteUInt32BigEndian(header, (uint)length);
            }

            return;
        }

        var maximum = 1;
        for (var index = 0; index < HeaderBytes; index++)
        {
            maximum *= 10;
        }

        if (length >= maximum)
        {
            throw new InvalidOperationException("Payload does not fit the ASCII transport header.");
        }

        for (var index = HeaderBytes - 1; index >= 0; index--)
        {
            header[index] = (byte)('0' + length % 10);
            length /= 10;
        }
    }

    private int ReadLength(ReadOnlySpan<byte> header)
    {
        if (LengthEncoding == LengthHeaderEncoding.BinaryBigEndian)
        {
            return HeaderBytes == 2
                ? BinaryPrimitives.ReadUInt16BigEndian(header)
                : checked((int)BinaryPrimitives.ReadUInt32BigEndian(header));
        }

        var length = 0;
        foreach (var digit in header)
        {
            if (digit is < (byte)'0' or > (byte)'9')
            {
                throw new FormatException("Transport length is not decimal.");
            }

            length = length * 10 + digit - '0';
        }

        return length;
    }
}

public sealed class Profile
{
    private static readonly JsonSerializerOptions JsonOptions = new()
    {
        PropertyNameCaseInsensitive = false,
        UnmappedMemberHandling = JsonUnmappedMemberHandling.Disallow
    };

    private readonly FieldDefinition?[] fields;
    private readonly IReadOnlyDictionary<string, MessageRule> messages;

    private Profile(string name, MessageSpec spec, FieldDefinition?[] fields,
        IReadOnlyDictionary<string, MessageRule> messages, TransportSpec transport)
    {
        Name = name;
        Spec = spec;
        this.fields = fields;
        this.messages = messages;
        Transport = transport;
    }

    public string Name { get; }
    public MessageSpec Spec { get; }
    public IReadOnlyDictionary<string, MessageRule> Messages => messages;
    public TransportSpec Transport { get; }

    public static Profile LoadJson(Stream stream)
    {
        ArgumentNullException.ThrowIfNull(stream);
        try
        {
            var document = JsonSerializer.Deserialize<ProfileDocument>(stream, JsonOptions)
                ?? throw new FormatException("Profile JSON is empty.");
            return Build(document);
        }
        catch (JsonException error)
        {
            throw new FormatException("Profile JSON is invalid.", error);
        }
    }

    public static Profile ParseJson(ReadOnlySpan<byte> json)
    {
        try
        {
            var document = JsonSerializer.Deserialize<ProfileDocument>(json, JsonOptions)
                ?? throw new FormatException("Profile JSON is empty.");
            return Build(document);
        }
        catch (JsonException error)
        {
            throw new FormatException("Profile JSON is invalid.", error);
        }
    }

    public FieldDefinition GetFieldDefinition(int number)
    {
        MessageSpec.CheckNumber(number);
        return fields[number] ?? throw new KeyNotFoundException($"Field {number} is not defined by profile {Name}.");
    }

    public void Validate(Message message)
    {
        ArgumentNullException.ThrowIfNull(message);
        if (!messages.TryGetValue(message.Mti, out var rule))
        {
            throw new InvalidOperationException($"MTI {message.Mti} is not allowed by profile {Name}.");
        }

        foreach (var number in rule.Required)
        {
            if (message.GetField(number).IsEmpty)
            {
                throw new InvalidOperationException($"Field {number} is required for MTI {message.Mti}.");
            }
        }

        for (var number = 2; number <= MessageSpec.MaxField; number++)
        {
            var value = message.GetField(number).Span;
            if (value.IsEmpty)
            {
                continue;
            }

            var definition = fields[number]
                ?? throw new InvalidOperationException($"Field {number} is not defined by profile {Name}.");
            ValidateField(number, value, definition);
        }
    }

    private static Profile Build(ProfileDocument document)
    {
        if (string.IsNullOrWhiteSpace(document.Name))
        {
            throw new FormatException("Profile name is required.");
        }

        var transport = BuildTransport(document.Transport);
        var specifications = new Dictionary<int, FieldSpec>();
        var fields = new FieldDefinition?[MessageSpec.MaxField + 1];
        foreach (var (number, field) in document.Fields)
        {
            MessageSpec.CheckNumber(number);
            var lengthType = field.LengthType switch
            {
                "fixed" => LengthType.Fixed,
                "llvar" => LengthType.Llvar,
                "lllvar" => LengthType.Lllvar,
                _ => throw new FormatException($"Field {number} has an invalid length type.")
            };
            var encoding = field.Encoding switch
            {
                "ascii" => FieldEncoding.Ascii,
                "binary" => FieldEncoding.Binary,
                "bcd" => FieldEncoding.PackedBcd,
                _ => throw new FormatException($"Field {number} has an invalid encoding.")
            };
            var content = field.Content switch
            {
                "numeric" => FieldContent.Numeric,
                "text" => FieldContent.Text,
                "bytes" => FieldContent.Bytes,
                _ => throw new FormatException($"Field {number} has an invalid content type.")
            };
            if (string.IsNullOrWhiteSpace(field.Name) ||
                encoding == FieldEncoding.Binary && content != FieldContent.Bytes ||
                encoding == FieldEncoding.PackedBcd && content != FieldContent.Numeric)
            {
                throw new FormatException($"Field {number} has invalid metadata.");
            }

            var lengthEncoding = field.LengthEncoding switch
            {
                "" or "ascii" => FieldLengthEncoding.Ascii,
                "bcd" => FieldLengthEncoding.Bcd,
                _ => throw new FormatException($"Field {number} has an invalid length encoding.")
            };
            var fieldSpec = new FieldSpec(lengthType, field.Length, encoding, lengthEncoding);
            specifications[number] = fieldSpec;
            fields[number] = new FieldDefinition(fieldSpec, field.Name, encoding, content, field.Sensitive);
        }

        var spec = new MessageSpec(specifications);
        var messages = new Dictionary<string, MessageRule>(StringComparer.Ordinal);
        foreach (var (mti, rule) in document.Messages)
        {
            if (mti.Length != 4)
            {
                throw new FormatException($"MTI rule {mti} is invalid.");
            }

            var seen = new HashSet<int>();
            foreach (var number in rule.Required)
            {
                if (number is < 2 or > MessageSpec.MaxField || fields[number] is null)
                {
                    throw new FormatException($"MTI {mti} requires undefined field {number}.");
                }

                if (!seen.Add(number))
                {
                    throw new FormatException($"MTI {mti} repeats required field {number}.");
                }
            }

            messages[mti] = new MessageRule(rule.Required.ToArray());
        }

        if (messages.Count == 0)
        {
            throw new FormatException("Profile must define at least one MTI.");
        }

        return new Profile(document.Name, spec, fields, messages, transport);
    }

    private static TransportSpec BuildTransport(TransportDocument document)
    {
        if (document.HeaderBytes is not (0 or 2 or 4) || document.MaxMessageBytes <= 0)
        {
            throw new FormatException("Transport headerBytes or maxMessageBytes is invalid.");
        }

        var encoding = document.LengthEncoding switch
        {
            "" when document.HeaderBytes == 0 => LengthHeaderEncoding.None,
            "binary-big-endian" when document.HeaderBytes != 0 => LengthHeaderEncoding.BinaryBigEndian,
            "ascii" when document.HeaderBytes != 0 => LengthHeaderEncoding.Ascii,
            _ => throw new FormatException("Transport lengthEncoding is invalid.")
        };
        return new TransportSpec(document.HeaderBytes, encoding, document.MaxMessageBytes);
    }

    private static void ValidateField(int number, ReadOnlySpan<byte> value, FieldDefinition definition)
    {
        var field = definition.FieldSpec;
        if (field.LengthType == LengthType.Fixed && value.Length != field.Length ||
            field.LengthType != LengthType.Fixed && value.Length > field.Length)
        {
            throw new InvalidOperationException($"Field {number} has an invalid length.");
        }

        foreach (var character in value)
        {
            if (definition.Encoding == FieldEncoding.Ascii && character > 0x7f)
            {
                throw new InvalidOperationException($"Field {number} is not ASCII.");
            }

            if (definition.Content == FieldContent.Numeric && character is < (byte)'0' or > (byte)'9')
            {
                throw new InvalidOperationException($"Field {number} is not numeric.");
            }

            if (definition.Content == FieldContent.Text && character is < 0x20 or > 0x7e)
            {
                throw new InvalidOperationException($"Field {number} is not printable text.");
            }
        }
    }
}

internal sealed class ProfileDocument
{
    [JsonPropertyName("name")] public string Name { get; set; } = "";
    [JsonPropertyName("transport")] public TransportDocument Transport { get; set; } = new();
    [JsonPropertyName("fields")] public Dictionary<int, FieldDocument> Fields { get; set; } = [];
    [JsonPropertyName("messages")] public Dictionary<string, MessageDocument> Messages { get; set; } = [];
}

internal sealed class TransportDocument
{
    [JsonPropertyName("headerBytes")] public int HeaderBytes { get; set; }
    [JsonPropertyName("lengthEncoding")] public string LengthEncoding { get; set; } = "";
    [JsonPropertyName("maxMessageBytes")] public int MaxMessageBytes { get; set; }
}

internal sealed class FieldDocument
{
    [JsonPropertyName("name")] public string Name { get; set; } = "";
    [JsonPropertyName("lengthType")] public string LengthType { get; set; } = "";
    [JsonPropertyName("length")] public int Length { get; set; }
    [JsonPropertyName("encoding")] public string Encoding { get; set; } = "";
    [JsonPropertyName("lengthEncoding")] public string LengthEncoding { get; set; } = "";
    [JsonPropertyName("content")] public string Content { get; set; } = "";
    [JsonPropertyName("sensitive")] public bool Sensitive { get; set; }
}

internal sealed class MessageDocument
{
    [JsonPropertyName("required")] public List<int> Required { get; set; } = [];
}