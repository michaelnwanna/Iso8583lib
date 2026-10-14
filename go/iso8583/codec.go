package iso8583

import (
	"errors"
	"fmt"
)

const MaxField = 128

type LengthType uint8

const (
	Fixed LengthType = iota
	LLVAR
	LLLVAR
)

type FieldEncoding uint8

const (
	Raw FieldEncoding = iota
	PackedBCD
)

type FieldLengthEncoding uint8

const (
	ASCIIFieldLength FieldLengthEncoding = iota
	BCDFieldLength
)

type FieldSpec struct {
	LengthType     LengthType
	Length         int
	WireEncoding   FieldEncoding
	LengthEncoding FieldLengthEncoding
}

type Spec struct {
	Fields [MaxField + 1]FieldSpec
}

func NewSpec(fields map[int]FieldSpec) (Spec, error) {
	var spec Spec
	for number, field := range fields {
		if number < 2 || number > MaxField {
			return Spec{}, fmt.Errorf("iso8583: field number %d is out of range", number)
		}
		if field.Length <= 0 {
			return Spec{}, fmt.Errorf("iso8583: field %d has invalid length", number)
		}
		if field.LengthType > LLLVAR || field.WireEncoding > PackedBCD || field.LengthEncoding > BCDFieldLength ||
			(field.LengthType == LLVAR && field.Length > 99) || (field.LengthType == LLLVAR && field.Length > 999) {
			return Spec{}, fmt.Errorf("iso8583: field %d has invalid length type or maximum", number)
		}
		if field.LengthType == Fixed && field.LengthEncoding != ASCIIFieldLength {
			return Spec{}, fmt.Errorf("iso8583: fixed field %d cannot define a length encoding", number)
		}
		spec.Fields[number] = field
	}
	return spec, nil
}

type Message struct {
	MTI    [4]byte
	Fields [MaxField + 1][]byte
}

func NewMessage(mti string) (Message, error) {
	if len(mti) != 4 {
		return Message{}, errors.New("iso8583: MTI must contain exactly 4 bytes")
	}
	var message Message
	copy(message.MTI[:], mti)
	return message, nil
}

func (message *Message) SetField(number int, value []byte) error {
	if number < 2 || number > MaxField {
		return fmt.Errorf("iso8583: field number %d is out of range", number)
	}
	message.Fields[number] = value
	return nil
}

func (message Message) AppendTo(destination []byte, spec Spec) ([]byte, error) {
	bitmapBytes := 8
	for number := 65; number <= MaxField; number++ {
		if len(message.Fields[number]) != 0 {
			bitmapBytes = 16
			break
		}
	}

	start := len(destination)
	destination = append(destination, message.MTI[:]...)
	destination = append(destination, make([]byte, bitmapBytes)...)
	bitmap := destination[start+4 : start+4+bitmapBytes]
	if bitmapBytes == 16 {
		bitmap[0] |= 0x80
	}

	for number := 2; number <= MaxField; number++ {
		value := message.Fields[number]
		if len(value) == 0 {
			continue
		}
		field := spec.Fields[number]
		if field.Length == 0 {
			return destination[:start], fmt.Errorf("iso8583: field %d is not defined", number)
		}
		if field.LengthType == Fixed && len(value) != field.Length {
			return destination[:start], fmt.Errorf("iso8583: field %d requires %d bytes", number, field.Length)
		}
		if field.LengthType != Fixed && len(value) > field.Length {
			return destination[:start], fmt.Errorf("iso8583: field %d exceeds %d bytes", number, field.Length)
		}
		if field.WireEncoding == PackedBCD && !decimal(value) {
			return destination[:start], fmt.Errorf("iso8583: field %d is not numeric", number)
		}

		bitmap = destination[start+4 : start+4+bitmapBytes]
		index := number - 1
		bitmap[index/8] |= 1 << (7 - index%8)
		if field.LengthType != Fixed {
			destination = appendFieldLength(destination, len(value), field)
		}
		if field.WireEncoding == PackedBCD {
			destination = appendBCD(destination, value)
		} else {
			destination = append(destination, value...)
		}
	}
	return destination, nil
}

func Parse(data []byte, spec Spec) (Message, error) {
	if len(data) < 12 {
		return Message{}, errors.New("iso8583: message is shorter than MTI and primary bitmap")
	}
	var message Message
	copy(message.MTI[:], data[:4])
	offset := 12
	bitmap := data[4:12]
	if bitmap[0]&0x80 != 0 {
		if len(data) < 20 {
			return Message{}, errors.New("iso8583: secondary bitmap is truncated")
		}
		bitmap = data[4:20]
		offset = 20
	}

	for number := 2; number <= len(bitmap)*8; number++ {
		index := number - 1
		if bitmap[index/8]&(1<<(7-index%8)) == 0 {
			continue
		}
		field := spec.Fields[number]
		if field.Length == 0 {
			return Message{}, fmt.Errorf("iso8583: field %d is not defined", number)
		}
		length := field.Length
		prefix := fieldPrefixBytes(field)
		if prefix != 0 {
			var err error
			length, err = parseFieldLength(data, offset, field)
			if err != nil || length > field.Length {
				return Message{}, fmt.Errorf("iso8583: invalid field %d length", number)
			}
			offset += prefix
		}
		wireLength := length
		if field.WireEncoding == PackedBCD {
			wireLength = (length + 1) / 2
		}
		if wireLength > len(data)-offset {
			return Message{}, fmt.Errorf("iso8583: field %d is truncated", number)
		}
		if field.WireEncoding == PackedBCD {
			value, err := unpackBCD(data[offset:offset+wireLength], length)
			if err != nil {
				return Message{}, fmt.Errorf("iso8583: invalid field %d BCD data", number)
			}
			message.Fields[number] = value
		} else {
			message.Fields[number] = data[offset : offset+wireLength]
		}
		offset += wireLength
	}
	if offset != len(data) {
		return Message{}, fmt.Errorf("iso8583: %d trailing bytes", len(data)-offset)
	}
	return message, nil
}

func appendLength(destination []byte, length, digits int) []byte {
	position := len(destination)
	destination = append(destination, make([]byte, digits)...)
	for index := position + digits - 1; index >= position; index-- {
		destination[index] = byte('0' + length%10)
		length /= 10
	}
	return destination
}

func parseLength(data []byte, offset, digits int) (int, error) {
	if offset > len(data)-digits {
		return 0, errors.New("truncated length")
	}
	length := 0
	for _, digit := range data[offset : offset+digits] {
		if digit < '0' || digit > '9' {
			return 0, errors.New("non-decimal length")
		}
		length = length*10 + int(digit-'0')
	}
	return length, nil
}

func fieldPrefixBytes(field FieldSpec) int {
	if field.LengthType == Fixed {
		return 0
	}
	digits := 2
	if field.LengthType == LLLVAR {
		digits = 3
	}
	if field.LengthEncoding == BCDFieldLength {
		return (digits + 1) / 2
	}
	return digits
}

func appendFieldLength(destination []byte, length int, field FieldSpec) []byte {
	digits := 2
	if field.LengthType == LLLVAR {
		digits = 3
	}
	if field.LengthEncoding == BCDFieldLength {
		position := len(destination)
		destination = append(destination, make([]byte, (digits+1)/2)...)
		for index := position + (digits+1)/2 - 1; index >= position; index-- {
			destination[index] = byte(length%10) | byte((length/10%10)<<4)
			length /= 100
		}
		return destination
	}
	return appendLength(destination, length, digits)
}

func parseFieldLength(data []byte, offset int, field FieldSpec) (int, error) {
	if field.LengthEncoding == ASCIIFieldLength {
		return parseLength(data, offset, fieldPrefixBytes(field))
	}
	bytes := fieldPrefixBytes(field)
	if offset > len(data)-bytes {
		return 0, errors.New("truncated length")
	}
	length := 0
	for _, value := range data[offset : offset+bytes] {
		if value>>4 > 9 || value&0x0f > 9 {
			return 0, errors.New("non-decimal BCD length")
		}
		length = length*100 + int(value>>4)*10 + int(value&0x0f)
	}
	return length, nil
}

func decimal(value []byte) bool {
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func appendBCD(destination, value []byte) []byte {
	if len(value)%2 != 0 {
		destination = append(destination, value[0]-'0')
		value = value[1:]
	}
	for index := 0; index < len(value); index += 2 {
		destination = append(destination, (value[index]-'0')<<4|(value[index+1]-'0'))
	}
	return destination
}

func unpackBCD(data []byte, digits int) ([]byte, error) {
	output := make([]byte, 0, digits)
	for index, value := range data {
		high, low := value>>4, value&0x0f
		if high > 9 || low > 9 || digits%2 != 0 && index == 0 && high != 0 {
			return nil, errors.New("non-decimal BCD data")
		}
		if digits%2 == 0 || index != 0 {
			output = append(output, '0'+high)
		}
		output = append(output, '0'+low)
	}
	return output, nil
}
