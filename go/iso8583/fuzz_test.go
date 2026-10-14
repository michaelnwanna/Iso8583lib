package iso8583

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func FuzzParse(f *testing.F) {
	profile := loadExampleProfile(f)
	for _, vector := range loadConformanceVectors(f) {
		f.Add(vector.payload)
	}
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		message, err := Parse(data, profile.Spec)
		if err != nil || profile.Validate(message) != nil {
			return
		}
		encoded, err := message.AppendTo(nil, profile.Spec)
		if err != nil {
			t.Fatalf("parsed message could not be re-encoded: %v", err)
		}
		if !bytes.Equal(encoded, data) {
			t.Fatalf("parse/encode changed payload: %X != %X", encoded, data)
		}
	})
}

func FuzzParseProfileJSON(f *testing.F) {
	_, source, _, _ := runtime.Caller(0)
	valid, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "profiles", "example-acquirer.json"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))

	f.Fuzz(func(t *testing.T, data []byte) {
		profile, err := ParseProfileJSON(data)
		if err == nil && (profile.Name == "" || len(profile.Messages) == 0 || profile.Transport.MaxMessageBytes <= 0) {
			t.Fatal("accepted profile violates construction invariants")
		}
	})
}

func FuzzTransportUnframe(f *testing.F) {
	profile := loadExampleProfile(f)
	for _, vector := range loadConformanceVectors(f) {
		f.Add(vector.frame)
	}
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, frame []byte) {
		payload, err := profile.Transport.Unframe(frame)
		if err != nil {
			return
		}
		reframed, err := profile.Transport.AppendFrame(nil, payload)
		if err != nil {
			t.Fatalf("accepted frame could not be rebuilt: %v", err)
		}
		if !bytes.Equal(reframed, frame) {
			t.Fatalf("unframe/frame changed data: %X != %X", reframed, frame)
		}
	})
}
