package codec

import (
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
)

// FuzzParseFrameLine feeds arbitrary bytes into the echoreplay line parser.
// The function must never panic or loop infinitely -- it should either return
// a valid frame or an error.
func FuzzParseFrameLine(f *testing.F) {
	// Seed corpus with representative inputs.
	f.Add([]byte("2024/01/15 12:00:00.000\t{}"))
	f.Add([]byte("2024/01/15 12:00:00.000\t{}\t {}"))
	f.Add([]byte("2024/01/15 12:00:00.000\t{\"gameStatus\":\"playing\"}"))
	f.Add([]byte("not a timestamp\t{}"))
	f.Add([]byte(""))
	f.Add([]byte("\t"))
	f.Add([]byte("\t\t"))
	f.Add([]byte("2024/01/15 12:00:00.000"))
	f.Add([]byte("2024/01/15 12:00:00.000\t{\"teams\":[{\"players\":[{\"name\":\"test\",\"userid\":12345}]}]}"))
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte("2024/01/15 12:00:00.000\t\x00\x01\x02"))

	f.Fuzz(func(t *testing.T, data []byte) {
		e := &EchoReplay{
			unmarshaler: &protojson.UnmarshalOptions{DiscardUnknown: true},
		}
		frame, err := e.parseFrameLine(data)
		if err != nil {
			// Error is fine -- the parser rejected malformed input.
			return
		}
		// If it succeeded, the frame must have a non-nil session.
		if frame == nil {
			t.Fatal("parseFrameLine returned nil frame without error")
		}
		if frame.Session == nil {
			t.Fatal("parseFrameLine returned frame with nil Session")
		}
	})
}

// FuzzFixProtojsonUint64Encoding exercises the uint64 fix with arbitrary byte
// inputs. It must never panic. If the input was valid JSON, the output must
// also be valid JSON.
func FuzzFixProtojsonUint64Encoding(f *testing.F) {
	f.Add([]byte(`{"userid":"12345","rules_changed_at":"99"}`))
	f.Add([]byte(`{"userid":12345}`))
	f.Add([]byte(`{"userid":"abc"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`"userid":"`))
	f.Add([]byte(`{"nested":{"userid":"0"}}`))
	f.Add([]byte(``))

	f.Fuzz(func(t *testing.T, data []byte) {
		result := FixProtojsonUint64Encoding(data)
		if result == nil {
			t.Fatal("FixProtojsonUint64Encoding returned nil")
		}
		// If input was valid JSON, output must also be valid JSON.
		if json.Valid(data) && !json.Valid(result) {
			t.Errorf("valid JSON input produced invalid JSON output.\ninput:  %q\noutput: %q", data, result)
		}
	})
}

// FuzzFixExponentNotation exercises the exponent notation fixer with arbitrary
// byte inputs. It must never panic.
func FuzzFixExponentNotation(f *testing.F) {
	f.Add([]byte(`{"value":1e-6}`))
	f.Add([]byte(`{"value":1.5e3}`))
	f.Add([]byte(`{"value":0.0}`))
	f.Add([]byte(`{"str":"1e-6 inside string"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(``))
	f.Add([]byte(`1e-999999`))
	f.Add([]byte(`-1.23456789e+12`))

	f.Fuzz(func(t *testing.T, data []byte) {
		result := FixExponentNotation(data)
		if result == nil {
			t.Fatal("FixExponentNotation returned nil")
		}
	})
}
