package codec

import (
	"strconv"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The engine and protojson disagree on how to spell a float.
//
// Every float the engine emits is exactly a float32 — verified by reproducing
// the source token from float32(value) on 38,700/38,700 tokens of the committed
// sample, 133,707/133,707 of a January 2026 capture, and 100% of three dal1
// captures. But the engine formats with 8 significant digits, trailing zeros
// trimmed to at least one decimal place, while protojson emits the shortest
// float64 round-trip. So -1.309 comes back as -1.309000015258789: identical
// value, different bytes.
//
// The rewrite is key-directed rather than purely lexical because protojson
// writes a zero double and a zero int32 identically ("0"), while the engine
// writes "0.0" and "0". The float-typed JSON keys are derived from the proto
// descriptors at init, so the set cannot drift from the schema.

// engineFloatKeys is the set of JSON keys whose values are float- or
// double-typed anywhere in the engine schema. Verified to have no collisions:
// no key is float-typed in one message and integer-typed in another, so a key
// alone determines whether its value needs the engine spelling.
var engineFloatKeys = buildEngineFloatKeys()

func buildEngineFloatKeys() map[string]bool {
	keys := make(map[string]bool, 48)
	seen := make(map[protoreflect.FullName]bool)

	var walk func(d protoreflect.MessageDescriptor)
	walk = func(d protoreflect.MessageDescriptor) {
		if seen[d.FullName()] {
			return
		}
		seen[d.FullName()] = true
		fields := d.Fields()
		for i := range fields.Len() {
			f := fields.Get(i)
			switch f.Kind() {
			case protoreflect.DoubleKind, protoreflect.FloatKind:
				keys[f.JSONName()] = true
			case protoreflect.MessageKind, protoreflect.GroupKind:
				walk(f.Message())
			}
		}
	}
	walk((&enginev1.SessionResponse{}).ProtoReflect().Descriptor())
	walk((&enginev1.PlayerBonesResponse{}).ProtoReflect().Descriptor())
	return keys
}

// appendEngineFloat writes v using the engine's spelling: 8 significant digits,
// trailing zeros trimmed but never leaving a bare integer, and exponent form
// written bare — "9.6339078e-5", "1.5345009e24" — where Go emits "e-05" and
// "e+24".
//
// The engine chooses exponent form by the same rule %g uses, so which values get
// it is already correct; only the exponent's own spelling differs.
func appendEngineFloat(dst []byte, v float64) []byte {
	start := len(dst)
	dst = strconv.AppendFloat(dst, v, 'g', 8, 64)

	dot, exp := -1, -1
	for i := start; i < len(dst); i++ {
		switch dst[i] {
		case 'e', 'E':
			exp = i
		case '.':
			if exp < 0 {
				dot = i
			}
		}
	}

	if exp < 0 {
		if dot < 0 {
			return append(dst, '.', '0')
		}
		end := len(dst)
		for end > dot+2 && dst[end-1] == '0' {
			end--
		}
		return dst[:end]
	}

	// Rewrite the exponent in place: drop '+' and any leading zeros, keeping a
	// '-' and at least one digit.
	mantissa := dst[:exp+1]
	digits := dst[exp+1:]
	neg := len(digits) > 0 && digits[0] == '-'
	if len(digits) > 0 && (digits[0] == '+' || digits[0] == '-') {
		digits = digits[1:]
	}
	for len(digits) > 1 && digits[0] == '0' {
		digits = digits[1:]
	}

	out := mantissa
	if neg {
		out = append(out, '-')
	}
	return append(out, digits...)
}

// FixEngineFloatFormatting rewrites float values in engine-format JSON so their
// spelling matches what the game writes, leaving every other byte untouched.
// Values are located by key, so an integer field that happens to hold 0 is not
// turned into 0.0.
func FixEngineFloatFormatting(data []byte) []byte {
	out := make([]byte, 0, len(data)+len(data)/8)
	i := 0
	for i < len(data) {
		c := data[i]
		if c != '"' {
			out = append(out, c)
			i++
			continue
		}

		// A quoted token: copy it, then decide whether it names a float field.
		keyStart := i + 1
		j := i + 1
		for j < len(data) && (data[j] != '"' || isEscaped(data, j)) {
			j++
		}
		if j >= len(data) {
			return append(out, data[i:]...)
		}
		key := data[keyStart:j]
		out = append(out, data[i:j+1]...)
		i = j + 1

		if i >= len(data) || data[i] != ':' || !engineFloatKeys[string(key)] {
			continue
		}
		out = append(out, ':')
		i++

		// Value: a scalar, or an array of them.
		if i < len(data) && data[i] == '[' {
			out = append(out, '[')
			i++
			for i < len(data) && data[i] != ']' {
				if data[i] == ',' || data[i] == ' ' {
					out = append(out, data[i])
					i++
					continue
				}
				out, i = appendReformatted(out, data, i)
			}
			continue
		}
		for i < len(data) && data[i] == ' ' {
			out = append(out, ' ')
			i++
		}
		out, i = appendReformatted(out, data, i)
	}
	return out
}

// appendReformatted copies one JSON number starting at i, respelled
// engine-style. Anything that does not parse as a number is copied verbatim, so
// null and unexpected tokens pass through unchanged.
func appendReformatted(dst, data []byte, i int) ([]byte, int) {
	start := i
	for i < len(data) {
		c := data[i]
		if (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' {
			i++
			continue
		}
		break
	}
	if i == start {
		// Not a number (e.g. "null"): copy one byte and let the caller advance.
		return append(dst, data[i]), i + 1
	}
	v, err := strconv.ParseFloat(string(data[start:i]), 64)
	if err != nil {
		return append(dst, data[start:i]...), i
	}
	return appendEngineFloat(dst, v), i
}
