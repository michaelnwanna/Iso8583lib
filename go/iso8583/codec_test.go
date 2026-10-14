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
