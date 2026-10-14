package iso8583

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type DataEncoding string

const (
	ASCII  DataEncoding = "ascii"
	Binary DataEncoding = "binary"
	BCD    DataEncoding = "bcd"
)

type FieldPrefixEncoding string

const (
	ASCIIPrefix FieldPrefixEncoding = "ascii"
	BCDPrefix   FieldPrefixEncoding = "bcd"
)

type ContentType string

const (
	Numeric ContentType = "numeric"
	Text    ContentType = "text"
	Bytes   ContentType = "bytes"
)

type FieldDefinition struct {
	FieldSpec
	Name           string
	Encoding       DataEncoding
	PrefixEncoding FieldPrefixEncoding
	Content        ContentType
	Sensitive      bool
}

type MessageRule struct {
	Required []int
}

type LengthEncoding string

const (
	BinaryBigEndian LengthEncoding = "binary-big-endian"
	ASCIILength     LengthEncoding = "ascii"
)

type TransportSpec struct {
	HeaderBytes     int
	LengthEncoding  LengthEncoding
	MaxMessageBytes int
}

type Profile struct {
	Name      string
	Spec      Spec
	Fields    [MaxField + 1]FieldDefinition
	Messages  map[string]MessageRule
	Transport TransportSpec
}

type profileDocument struct {
	Name      string                     `json:"name"`
	Transport TransportSpec              `json:"transport"`
	Fields    map[int]fieldDocument      `json:"fields"`
	Messages  map[string]messageDocument `json:"messages"`
}

type fieldDocument struct {
	Name           string              `json:"name"`
	LengthType     string              `json:"lengthType"`
	Length         int                 `json:"length"`
	Encoding       DataEncoding        `json:"encoding"`
	LengthEncoding FieldPrefixEncoding `json:"lengthEncoding"`
	Content        ContentType         `json:"content"`
	Sensitive      bool                `json:"sensitive"`
}

type messageDocument struct {
	Required []int `json:"required"`
}

func LoadProfileJSON(reader io.Reader) (Profile, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var document profileDocument
	if err := decoder.Decode(&document); err != nil {
		return Profile{}, fmt.Errorf("iso8583: decode profile: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return Profile{}, err
	}
	return buildProfile(document)
}

func ParseProfileJSON(data []byte) (Profile, error) {
	return LoadProfileJSON(bytes.NewReader(data))
}

func (profile Profile) Validate(message Message) error {
	mti := string(message.MTI[:])
	rule, ok := profile.Messages[mti]
	if !ok {
		return fmt.Errorf("iso8583: MTI %q is not allowed by profile %q", mti, profile.Name)
	}
	for _, number := range rule.Required {
		if len(message.Fields[number]) == 0 {
			return fmt.Errorf("iso8583: field %d is required for MTI %s", number, mti)
		}
	}
	for number := 2; number <= MaxField; number++ {
		value := message.Fields[number]
		if len(value) == 0 {
			continue
		}
		definition := profile.Fields[number]
		if definition.Length == 0 {
			return fmt.Errorf("iso8583: field %d is not defined by profile %q", number, profile.Name)
		}
		if definition.LengthType == Fixed && len(value) != definition.Length {
			return fmt.Errorf("iso8583: field %d requires %d bytes", number, definition.Length)
		}
		if definition.LengthType != Fixed && len(value) > definition.Length {
			return fmt.Errorf("iso8583: field %d exceeds %d bytes", number, definition.Length)
		}
		if err := validateContent(number, value, definition); err != nil {
			return err
		}
	}
	return nil
}

func (transport TransportSpec) AppendFrame(destination, payload []byte) ([]byte, error) {
	if len(payload) > transport.MaxMessageBytes {
		return destination, fmt.Errorf("iso8583: payload exceeds %d bytes", transport.MaxMessageBytes)
	}
	start := len(destination)
	destination = append(destination, make([]byte, transport.HeaderBytes)...)
	if err := transport.writeLength(destination[start:], len(payload)); err != nil {
		return destination[:start], err
	}
	destination = append(destination, payload...)
	return destination, nil
}

func (transport TransportSpec) Unframe(frame []byte) ([]byte, error) {
	if transport.HeaderBytes == 0 {
		if len(frame) > transport.MaxMessageBytes {
			return nil, fmt.Errorf("iso8583: payload exceeds %d bytes", transport.MaxMessageBytes)
		}
		return frame, nil
	}
	if len(frame) < transport.HeaderBytes {
		return nil, errors.New("iso8583: transport header is truncated")
	}
	length, err := transport.readLength(frame[:transport.HeaderBytes])
	if err != nil {
		return nil, err
	}
	if length > transport.MaxMessageBytes {
		return nil, fmt.Errorf("iso8583: framed payload exceeds %d bytes", transport.MaxMessageBytes)
	}
	if len(frame)-transport.HeaderBytes != length {
		return nil, fmt.Errorf("iso8583: frame declares %d payload bytes, got %d", length, len(frame)-transport.HeaderBytes)
	}
	return frame[transport.HeaderBytes:], nil
}

func (transport TransportSpec) TryReadFrame(data []byte) (payload []byte, consumed int, complete bool, err error) {
	if transport.HeaderBytes == 0 {
		if len(data) == 0 {
			return nil, 0, false, nil
		}
		if len(data) > transport.MaxMessageBytes {
			return nil, 0, false, fmt.Errorf("iso8583: payload exceeds %d bytes", transport.MaxMessageBytes)
		}
		return data, len(data), true, nil
	}
	if len(data) < transport.HeaderBytes {
		return nil, 0, false, nil
	}
	length, err := transport.readLength(data[:transport.HeaderBytes])
	if err != nil {
		return nil, 0, false, err
	}
	if length > transport.MaxMessageBytes {
		return nil, 0, false, fmt.Errorf("iso8583: framed payload exceeds %d bytes", transport.MaxMessageBytes)
	}
	required := transport.HeaderBytes + length
	if len(data) < required {
		return nil, 0, false, nil
	}
	return data[transport.HeaderBytes:required], required, true, nil
}

func buildProfile(document profileDocument) (Profile, error) {
	if document.Name == "" {
		return Profile{}, errors.New("iso8583: profile name is required")
	}
	if err := validateTransport(document.Transport); err != nil {
		return Profile{}, err
	}
	fields := make(map[int]FieldSpec, len(document.Fields))
	profile := Profile{Name: document.Name, Messages: make(map[string]MessageRule), Transport: document.Transport}
	for number, field := range document.Fields {
		lengthType, err := parseLengthType(field.LengthType)
		if err != nil {
			return Profile{}, fmt.Errorf("iso8583: field %d: %w", number, err)
		}
		if field.Name == "" || (field.Encoding != ASCII && field.Encoding != Binary && field.Encoding != BCD) ||
			(field.Content != Numeric && field.Content != Text && field.Content != Bytes) {
			return Profile{}, fmt.Errorf("iso8583: field %d has invalid metadata", number)
		}
		if field.Encoding == Binary && field.Content != Bytes {
			return Profile{}, fmt.Errorf("iso8583: binary field %d must use bytes content", number)
		}
		if field.Encoding == BCD && field.Content != Numeric {
			return Profile{}, fmt.Errorf("iso8583: BCD field %d must use numeric content", number)
		}
		prefixEncoding := field.LengthEncoding
		if prefixEncoding == "" {
			prefixEncoding = ASCIIPrefix
		}
		if prefixEncoding != ASCIIPrefix && prefixEncoding != BCDPrefix {
			return Profile{}, fmt.Errorf("iso8583: field %d has invalid length encoding", number)
		}
		wireEncoding := Raw
		if field.Encoding == BCD {
			wireEncoding = PackedBCD
		}
		lengthEncoding := ASCIIFieldLength
		if prefixEncoding == BCDPrefix {
			lengthEncoding = BCDFieldLength
		}
		definition := FieldDefinition{
			FieldSpec: FieldSpec{LengthType: lengthType, Length: field.Length, WireEncoding: wireEncoding, LengthEncoding: lengthEncoding},
			Name:      field.Name, Encoding: field.Encoding, PrefixEncoding: prefixEncoding,
			Content: field.Content, Sensitive: field.Sensitive,
		}
		if number < 2 || number > MaxField {
			return Profile{}, fmt.Errorf("iso8583: field number %d is out of range", number)
		}
		profile.Fields[number] = definition
		fields[number] = definition.FieldSpec
	}
	spec, err := NewSpec(fields)
	if err != nil {
		return Profile{}, err
	}
	profile.Spec = spec
	for mti, rule := range document.Messages {
		if len(mti) != 4 {
			return Profile{}, fmt.Errorf("iso8583: invalid MTI rule %q", mti)
		}
		seen := make(map[int]bool, len(rule.Required))
		for _, number := range rule.Required {
			if number < 2 || number > MaxField || profile.Fields[number].Length == 0 {
				return Profile{}, fmt.Errorf("iso8583: MTI %s requires undefined field %d", mti, number)
			}
			if seen[number] {
				return Profile{}, fmt.Errorf("iso8583: MTI %s repeats required field %d", mti, number)
			}
			seen[number] = true
		}
		profile.Messages[mti] = MessageRule{Required: append([]int(nil), rule.Required...)}
	}
	if len(profile.Messages) == 0 {
		return Profile{}, errors.New("iso8583: profile must define at least one MTI")
	}
	return profile, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("iso8583: profile contains multiple JSON values")
		}
		return fmt.Errorf("iso8583: decode profile: %w", err)
	}
	return nil
}

func parseLengthType(value string) (LengthType, error) {
	switch value {
	case "fixed":
		return Fixed, nil
	case "llvar":
		return LLVAR, nil
	case "lllvar":
		return LLLVAR, nil
	default:
		return 0, fmt.Errorf("invalid length type %q", value)
	}
}

func validateTransport(transport TransportSpec) error {
	if transport.HeaderBytes != 0 && transport.HeaderBytes != 2 && transport.HeaderBytes != 4 {
		return errors.New("iso8583: transport headerBytes must be 0, 2, or 4")
	}
	if transport.MaxMessageBytes <= 0 {
		return errors.New("iso8583: transport maxMessageBytes must be positive")
	}
	if transport.HeaderBytes == 0 {
		if transport.LengthEncoding != "" {
			return errors.New("iso8583: lengthEncoding requires a transport header")
		}
		return nil
	}
	if transport.LengthEncoding != BinaryBigEndian && transport.LengthEncoding != ASCIILength {
		return errors.New("iso8583: unsupported transport lengthEncoding")
	}
	return nil
}

func validateContent(number int, value []byte, definition FieldDefinition) error {
	if definition.Encoding == ASCII {
		for _, character := range value {
			if character > 0x7f {
				return fmt.Errorf("iso8583: field %d is not ASCII", number)
			}
		}
	}
	if definition.Content == Numeric {
		for _, character := range value {
			if character < '0' || character > '9' {
				return fmt.Errorf("iso8583: field %d is not numeric", number)
			}
		}
	} else if definition.Content == Text {
		for _, character := range value {
			if character < 0x20 || character > 0x7e {
				return fmt.Errorf("iso8583: field %d is not printable text", number)
			}
		}
	}
	return nil
}

func (transport TransportSpec) writeLength(header []byte, length int) error {
	if transport.HeaderBytes == 0 {
		return nil
	}
	if transport.LengthEncoding == BinaryBigEndian {
		if transport.HeaderBytes == 2 {
			if length > 0xffff {
				return errors.New("iso8583: payload does not fit the transport header")
			}
			binary.BigEndian.PutUint16(header, uint16(length))
		} else {
			binary.BigEndian.PutUint32(header, uint32(length))
		}
		return nil
	}
	maximum := 1
	for range transport.HeaderBytes {
		maximum *= 10
	}
	if length >= maximum {
		return errors.New("iso8583: payload does not fit the ASCII transport header")
	}
	for index := transport.HeaderBytes - 1; index >= 0; index-- {
		header[index] = byte('0' + length%10)
		length /= 10
	}
	return nil
}

func (transport TransportSpec) readLength(header []byte) (int, error) {
	if transport.LengthEncoding == BinaryBigEndian {
		if transport.HeaderBytes == 2 {
			return int(binary.BigEndian.Uint16(header)), nil
		}
		length := binary.BigEndian.Uint32(header)
		if uint64(length) > uint64(^uint(0)>>1) {
			return 0, errors.New("iso8583: transport length overflows int")
		}
		return int(length), nil
	}
	return parseLength(header, 0, transport.HeaderBytes)
}
