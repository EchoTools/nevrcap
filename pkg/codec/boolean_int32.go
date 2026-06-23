package codec

import "bytes"

// booleanNumericPatterns lists the numeric proto fields that the EchoVR
// Spark-format recorder emits as JSON booleans (false/true). protojson.Unmarshal
// rejects a boolean for a numeric field (int32 or double), so on the read path
// these must be coerced to 0/1 before unmarshaling.
//
// These are exactly the fields observed as booleans in Spark-format
// .echoreplay captures but numeric (0 / 0.0) in client-format captures:
//   - *_team_restart_request : int32
//   - *_shoulder_pressed[2]  : double (float64)
//
// Genuinely-boolean proto fields (e.g. possession, blocking, invulnerable,
// stunned, is_emote_playing, private_match, tournament_match, contested) are
// NOT listed here — they parse correctly as booleans and must be left alone.
//
// Each pattern includes the leading quote and trailing `":` so it only matches a
// JSON key, never an arbitrary substring of some value. 0/1 are valid JSON
// numbers for both int32 and double proto fields.
var booleanNumericPatterns = [][]byte{
	[]byte(`"blue_team_restart_request":`),
	[]byte(`"orange_team_restart_request":`),
	[]byte(`"left_shoulder_pressed":`),
	[]byte(`"left_shoulder_pressed2":`),
	[]byte(`"right_shoulder_pressed":`),
	[]byte(`"right_shoulder_pressed2":`),
}

var (
	jsonFalse = []byte("false")
	jsonTrue  = []byte("true")
)

// FixBooleanInt32Fields rewrites boolean-valued occurrences of known numeric
// proto fields to their numeric equivalents (false -> 0, true -> 1).
//
// This mirrors FixProtojsonUint64Encoding / FixExponentNotation: a fast,
// regex-free byte fixer applied to session JSON so Spark-format .echoreplay
// recordings (which emit these numeric fields as booleans) round-trip through
// protojson. Fields already encoded numerically are left untouched.
//
// The name is kept for backward compatibility; it now covers both int32 and
// double proto fields (see booleanNumericPatterns).
//
// It is string-aware only to the extent of requiring the match to be a JSON key
// followed immediately by a boolean literal; a key name appearing inside a
// quoted string value will not be followed by a bare false/true and so is not
// rewritten.
func FixBooleanInt32Fields(data []byte) []byte {
	for _, pattern := range booleanNumericPatterns {
		data = fixBooleanField(data, pattern)
	}
	return data
}

// fixBooleanField finds pattern (e.g. `"blue_team_restart_request":`) and, when
// the value immediately following is the boolean literal `false` or `true`,
// replaces it with `0` or `1` respectively.
func fixBooleanField(data []byte, pattern []byte) []byte {
	offset := 0
	for {
		idx := bytes.Index(data[offset:], pattern)
		if idx == -1 {
			break
		}
		idx += offset
		valStart := idx + len(pattern)

		switch {
		case bytes.HasPrefix(data[valStart:], jsonFalse):
			data = replaceRange(data, valStart, valStart+len(jsonFalse), []byte("0"))
			offset = valStart + 1
		case bytes.HasPrefix(data[valStart:], jsonTrue):
			data = replaceRange(data, valStart, valStart+len(jsonTrue), []byte("1"))
			offset = valStart + 1
		default:
			// Already numeric (or something unexpected): leave it alone.
			offset = valStart
		}
	}
	return data
}

// replaceRange returns data with data[start:end] replaced by repl, on a freshly
// allocated slice so the input is never mutated.
func replaceRange(data []byte, start, end int, repl []byte) []byte {
	out := make([]byte, 0, len(data)-(end-start)+len(repl))
	out = append(out, data[:start]...)
	out = append(out, repl...)
	out = append(out, data[end:]...)
	return out
}
