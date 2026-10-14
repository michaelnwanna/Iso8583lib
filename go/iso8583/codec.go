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

type FieldSpec struct {
	LengthType LengthType
	Length     int
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
		if field.LengthType > LLLVAR || (field.LengthType == LLVAR && field.Length > 99) || (field.LengthType == LLLVAR && field.Length > 999) {
			return Spec{}, fmt.Errorf("iso8583: field %d has invalid length type or maximum", number)
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

		index := number - 1
		bitmap[index/8] |= 1 << (7 - index%8)
		if field.LengthType == LLVAR {
			destination = appendLength(destination, len(value), 2)
		} else if field.LengthType == LLLVAR {
			destination = appendLength(destination, len(value), 3)
		}
		destination = append(destination, value...)
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
		prefix := 0
		if field.LengthType == LLVAR {
			prefix = 2
		} else if field.LengthType == LLLVAR {
			prefix = 3
		}
		if prefix != 0 {
			var err error
			length, err = parseLength(data, offset, prefix)
			if err != nil || length > field.Length {
				return Message{}, fmt.Errorf("iso8583: invalid field %d length", number)
			}
			offset += prefix
		}
		if length > len(data)-offset {
			return Message{}, fmt.Errorf("iso8583: field %d is truncated", number)
		}
		message.Fields[number] = data[offset : offset+length]
		offset += length
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
