package iso8583

import (
	"bytes"
	"testing"
)

func testSpec(t *testing.T) Spec {
	t.Helper()
	spec, err := NewSpec(map[int]FieldSpec{
		2:  {LengthType: LLVAR, Length: 19},
		3:  {LengthType: Fixed, Length: 6},
		4:  {LengthType: Fixed, Length: 12},
		70: {LengthType: Fixed, Length: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestRoundTripWithSecondaryBitmap(t *testing.T) {
	message, err := NewMessage("0800")
	if err != nil {
		t.Fatal(err)
	}
	for number, value := range map[int]string{2: "4761739001010010", 3: "000000", 4: "000000001000", 70: "301"} {
		if err := message.SetField(number, []byte(value)); err != nil {
			t.Fatal(err)
		}
	}

	encoded, err := message.AppendTo(make([]byte, 0, 128), testSpec(t))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Parse(encoded, testSpec(t))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.MTI != message.MTI {
		t.Fatalf("MTI = %q, want %q", decoded.MTI, message.MTI)
	}
	for _, number := range []int{2, 3, 4, 70} {
		if !bytes.Equal(decoded.Fields[number], message.Fields[number]) {
			t.Fatalf("field %d = %q, want %q", number, decoded.Fields[number], message.Fields[number])
		}
	}
}

func TestRejectsTruncatedField(t *testing.T) {
	message, _ := NewMessage("0200")
	_ = message.SetField(3, []byte("000000"))
	encoded, err := message.AppendTo(nil, testSpec(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(encoded[:len(encoded)-1], testSpec(t)); err == nil {
		t.Fatal("Parse accepted a truncated field")
	}
}

func TestCardPurchaseAuthorizationFlow(t *testing.T) {
	spec, err := NewSpec(map[int]FieldSpec{
		2:  {LengthType: LLVAR, Length: 19},
		3:  {LengthType: Fixed, Length: 6},
		4:  {LengthType: Fixed, Length: 12},
		7:  {LengthType: Fixed, Length: 10},
		11: {LengthType: Fixed, Length: 6},
		12: {LengthType: Fixed, Length: 6},
		13: {LengthType: Fixed, Length: 4},
		14: {LengthType: Fixed, Length: 4},
		18: {LengthType: Fixed, Length: 4},
		22: {LengthType: Fixed, Length: 3},
		25: {LengthType: Fixed, Length: 2},
		32: {LengthType: LLVAR, Length: 11},
		37: {LengthType: Fixed, Length: 12},
		38: {LengthType: Fixed, Length: 6},
		39: {LengthType: Fixed, Length: 2},
		41: {LengthType: Fixed, Length: 8},
		42: {LengthType: Fixed, Length: 15},
		49: {LengthType: Fixed, Length: 3},
	})
	if err != nil {
		t.Fatal(err)
	}

	request, _ := NewMessage("0100")
	requestFields := map[int]string{
		2: "4111111111111111", 3: "000000", 4: "000000012500",
		7: "1014123456", 11: "123456", 12: "123456", 13: "1014",
		14: "2912", 18: "5411", 22: "051", 25: "00", 32: "12345678901",
		37: "628812345678", 41: "TERM0001", 42: "MERCHANT0000001", 49: "840",
	}
	for number, value := range requestFields {
		if err := request.SetField(number, []byte(value)); err != nil {
			t.Fatal(err)
		}
	}

	wireRequest, err := request.AppendTo(make([]byte, 0, 256), spec)
	if err != nil {
		t.Fatal(err)
	}
	acquirerRequest, err := Parse(wireRequest, spec)
	if err != nil {
		t.Fatal(err)
	}
	if string(acquirerRequest.MTI[:]) != "0100" || string(acquirerRequest.Fields[4]) != "000000012500" {
		t.Fatal("acquirer did not receive the expected $125.00 authorization request")
	}
	if string(acquirerRequest.Fields[41]) != "TERM0001" || string(acquirerRequest.Fields[42]) != "MERCHANT0000001" {
		t.Fatal("terminal or merchant identity did not round trip")
	}

	response, _ := NewMessage("0110")
	responseFields := map[int]string{
		3: "000000", 4: "000000012500", 11: "123456", 37: "628812345678",
		38: "A12345", 39: "00", 41: "TERM0001", 42: "MERCHANT0000001", 49: "840",
	}
	for number, value := range responseFields {
		if err := response.SetField(number, []byte(value)); err != nil {
			t.Fatal(err)
		}
	}

	wireResponse, err := response.AppendTo(make([]byte, 0, 128), spec)
	if err != nil {
		t.Fatal(err)
	}
	terminalResponse, err := Parse(wireResponse, spec)
	if err != nil {
		t.Fatal(err)
	}
	if string(terminalResponse.MTI[:]) != "0110" || string(terminalResponse.Fields[39]) != "00" {
		t.Fatal("terminal did not receive an approved authorization response")
	}
	if string(terminalResponse.Fields[38]) != "A12345" || string(terminalResponse.Fields[37]) != requestFields[37] {
		t.Fatal("authorization or retrieval reference did not correlate")
	}
}

func BenchmarkRoundTrip(b *testing.B) {
	spec := testSpec(&testing.T{})
	message, _ := NewMessage("0200")
	_ = message.SetField(2, []byte("4761739001010010"))
	_ = message.SetField(3, []byte("000000"))
	buffer := make([]byte, 0, 128)
	b.ReportAllocs()
	for range b.N {
		encoded, err := message.AppendTo(buffer[:0], spec)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := Parse(encoded, spec); err != nil {
			b.Fatal(err)
		}
	}
}
