package iso8583

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

type conformanceVector struct {
	name    string
	mti     string
	fields  map[int][]byte
	payload []byte
	frame   []byte
}

func TestSharedConformanceVectors(t *testing.T) {
	profile := loadExampleProfile(t)
	for _, vector := range loadConformanceVectors(t) {
		t.Run(vector.name, func(t *testing.T) {
			message, err := NewMessage(vector.mti)
			if err != nil {
				t.Fatal(err)
			}
			for number, value := range vector.fields {
				if err := message.SetField(number, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := profile.Validate(message); err != nil {
				t.Fatal(err)
			}

			payload, err := message.AppendTo(nil, profile.Spec)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(payload, vector.payload) {
				t.Fatalf("payload = %X, want %X", payload, vector.payload)
			}
			frame, err := profile.Transport.AppendFrame(nil, payload)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(frame, vector.frame) {
				t.Fatalf("frame = %X, want %X", frame, vector.frame)
			}

			decoded, err := Parse(vector.payload, profile.Spec)
			if err != nil {
				t.Fatal(err)
			}
			if err := profile.Validate(decoded); err != nil {
				t.Fatal(err)
			}
			for number, want := range vector.fields {
				if !bytes.Equal(decoded.Fields[number], want) {
					t.Fatalf("field %d = %q, want %q", number, decoded.Fields[number], want)
				}
			}
		})
	}
}

func loadConformanceVectors(t testing.TB) []conformanceVector {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	file, err := os.Open(filepath.Join(filepath.Dir(source), "..", "..", "testdata", "conformance.vectors"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var vectors []conformanceVector
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 5 {
			t.Fatalf("invalid conformance vector %q", line)
		}
		vector := conformanceVector{name: parts[0], mti: parts[1], fields: make(map[int][]byte)}
		for _, pair := range strings.Split(parts[2], ";") {
			field := strings.SplitN(pair, "=", 2)
			if len(field) != 2 {
				t.Fatalf("invalid field pair %q", pair)
			}
			number, err := strconv.Atoi(field[0])
			if err != nil {
				t.Fatal(err)
			}
			vector.fields[number] = []byte(field[1])
		}
		vector.payload, err = hex.DecodeString(parts[3])
		if err != nil {
			t.Fatal(err)
		}
		vector.frame, err = hex.DecodeString(parts[4])
		if err != nil {
			t.Fatal(err)
		}
		vectors = append(vectors, vector)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(vectors) == 0 {
		t.Fatal("no conformance vectors loaded")
	}
	return vectors
}
