package iso8583

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func loadExampleProfile(t testing.TB) Profile {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	file, err := os.Open(filepath.Join(filepath.Dir(source), "..", "..", "profiles", "example-acquirer.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	profile, err := LoadProfileJSON(file)
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func TestProfileValidatesAndFramesAuthorization(t *testing.T) {
	profile := loadExampleProfile(t)
	request, _ := NewMessage("0100")
	for number, value := range map[int]string{
		2: "4111111111111111", 3: "000000", 4: "000000012500", 7: "1014123456",
		11: "123456", 22: "051", 37: "628812345678", 41: "TERM0001",
		42: "MERCHANT0000001", 49: "840",
	} {
		_ = request.SetField(number, []byte(value))
	}
	if err := profile.Validate(request); err != nil {
		t.Fatal(err)
	}
	payload, err := request.AppendTo(nil, profile.Spec)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := profile.Transport.AppendFrame(nil, payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(frame) != len(payload)+2 || !bytes.Equal(frame[2:], payload) {
		t.Fatal("binary transport frame does not contain the encoded payload")
	}
	unframed, err := profile.Transport.Unframe(frame)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Parse(unframed, profile.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := profile.Validate(decoded); err != nil {
		t.Fatal(err)
	}
}

func TestProfileRejectsMissingRequiredAndInvalidContent(t *testing.T) {
	profile := loadExampleProfile(t)
	request, _ := NewMessage("0100")
	_ = request.SetField(2, []byte("not-a-pan"))
	if err := profile.Validate(request); err == nil {
		t.Fatal("profile accepted a request missing required fields")
	}

	for number, value := range map[int]string{
		3: "000000", 4: "000000012500", 7: "1014123456", 11: "123456",
		22: "051", 37: "628812345678", 41: "TERM0001", 42: "MERCHANT0000001", 49: "840",
	} {
		_ = request.SetField(number, []byte(value))
	}
	if err := profile.Validate(request); err == nil {
		t.Fatal("profile accepted non-numeric PAN content")
	}
	_ = request.SetField(2, []byte("4111111111111111"))
	_ = request.SetField(4, []byte("12500"))
	if err := profile.Validate(request); err == nil {
		t.Fatal("profile accepted an invalid fixed field length")
	}
}

func TestProfileRejectsUnknownConfiguration(t *testing.T) {
	_, err := ParseProfileJSON([]byte(`{
		"name":"bad", "transport":{"headerBytes":0,"maxMessageBytes":10},
		"fields":{}, "messages":{"0100":{"required":[]}}, "unexpected":true
	}`))
	if err == nil {
		t.Fatal("profile accepted an unknown configuration property")
	}
}

func TestTransportRejectsLengthMismatchAndSupportsNoHeader(t *testing.T) {
	transport := TransportSpec{HeaderBytes: 2, LengthEncoding: BinaryBigEndian, MaxMessageBytes: 128}
	if _, err := transport.Unframe([]byte{0, 2, 'a'}); err == nil {
		t.Fatal("transport accepted a mismatched declared length")
	}

	plain := TransportSpec{MaxMessageBytes: 128}
	payload := []byte("payload")
	framed, err := plain.AppendFrame(nil, payload)
	if err != nil {
		t.Fatal(err)
	}
	unframed, err := plain.Unframe(framed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unframed, payload) {
		t.Fatal("headerless transport changed the payload")
	}
}
