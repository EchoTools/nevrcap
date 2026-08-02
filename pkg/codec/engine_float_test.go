package codec

import (
	"archive/zip"
	"bytes"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestAppendEngineFloat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   float64
		want string
	}{
		// Values taken from real captures: the left column is what protojson
		// produces for the float32-narrowed value, the right is what the engine
		// wrote in the source file.
		{-1.309000015258789, "-1.309"},
		{7.373000144958496, "7.3730001"},
		{-10.25100040435791, "-10.251"},
		{0.10400000214576721, "0.104"},
		{0.7090000510215759, "0.70900005"},
		{-0.6970000267028809, "-0.69700003"},
		{-0.045000001788139343, "-0.045000002"},
		// Whole numbers keep a decimal place; the engine never writes a bare "1".
		{1, "1.0"},
		{0, "0.0"},
		{-0, "0.0"},
		{2.5, "2.5"},
		{-12, "-12.0"},
	}
	for _, tt := range tests {
		if got := string(appendEngineFloat(nil, tt.in)); got != tt.want {
			t.Errorf("appendEngineFloat(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestAppendEngineFloatIsIdempotent pins the invariant that actually governs the
// round-trip: a value the ENGINE wrote is already an 8-significant-digit
// decimal, so parsing and respelling it must reproduce the identical string.
//
// Note this is weaker than "8 digits round-trips any float32", which is false —
// a float32 needs up to 9 significant digits in general, and a denormal such as
// 1.02424515e-36 does not survive 8. That case cannot reach us: every value in a
// capture was written by the engine at 8 digits in the first place, so the
// precision was already spent upstream. Reformatting neither adds nor removes
// error.
func TestAppendEngineFloatIsIdempotent(t *testing.T) {
	t.Parallel()

	for i := range 200000 {
		f := math.Float32frombits(uint32(i*7919 + 1)) //nolint:gosec // deterministic bit-pattern sweep
		if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
			continue
		}
		// What the engine would have written for this value.
		once := string(appendEngineFloat(nil, float64(f)))
		v, err := strconv.ParseFloat(once, 64)
		if err != nil {
			t.Fatalf("appendEngineFloat(%v) produced unparseable %q: %v", f, once, err)
		}
		if twice := string(appendEngineFloat(nil, v)); twice != once {
			t.Fatalf("not idempotent: %q -> %q", once, twice)
		}
	}
}

func TestFixEngineFloatFormatting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"float key scalar",
			`{"game_clock":662.8041381835938,"blue_points":1}`,
			`{"game_clock":662.80414,"blue_points":1}`,
		},
		{
			"integer keys are untouched",
			`{"blue_points":0,"total_round_count":3,"err_code":0}`,
			`{"blue_points":0,"total_round_count":3,"err_code":0}`,
		},
		{
			"zero double keeps its decimal",
			`{"left_shoulder_pressed":0,"blue_points":0}`,
			`{"left_shoulder_pressed":0.0,"blue_points":0}`,
		},
		{
			"float arrays",
			`{"position":[-1.309000015258789,7.373000144958496,0]}`,
			`{"position":[-1.309,7.3730001,0.0]}`,
		},
		{
			"a string that merely looks like a key is not rewritten",
			`{"name":"position","blue_points":2}`,
			`{"name":"position","blue_points":2}`,
		},
		{
			"null under a float key passes through",
			`{"disc":null,"blue_points":1}`,
			`{"disc":null,"blue_points":1}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := string(FixEngineFloatFormatting([]byte(tt.in))); got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

// TestAppendEngineFloatMatchesTheEngine is the measurement this whole file rests
// on: take every float literal the engine actually wrote in a real capture,
// parse it, respell it, and require the identical bytes back.
func TestAppendEngineFloatMatchesTheEngine(t *testing.T) {
	t.Parallel()

	src := filepath.Join("..", "..", "testdata", "sample.echoreplay")
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no sample: %v", err)
	}

	zr, err := zip.OpenReader(src)
	if err != nil {
		t.Fatalf("open sample: %v", err)
	}
	defer zr.Close() //nolint:errcheck // read-only test fixture
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatalf("open member: %v", err)
	}
	body, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("read member: %v", err)
	}

	// Scan only the JSON fields of each line, and only outside quoted strings.
	// A naive regex over the whole line matches the timestamp ("00:20:09.501")
	// and the display clock ("11:02.80"), neither of which is a float value.
	//
	// FixExponentNotation is deliberately NOT applied: it expands exponent form
	// to decimal, but the engine USES exponent form for small magnitudes
	// (9.6339078e-5), so applying it would diverge. appendEngineFloat owns the
	// engine's exponent spelling directly.
	var tokens, mismatches int
	for line := range bytes.SplitSeq(body, []byte("\n")) {
		fields := bytes.Split(bytes.TrimRight(line, "\r"), []byte("\t"))
		for _, field := range fields[1:] {
			for _, tok := range floatLiteralsOutsideStrings(field) {
				v, err := strconv.ParseFloat(tok, 64)
				if err != nil {
					continue
				}
				tokens++
				got := string(appendEngineFloat(nil, v))
				if got != tok {
					if mismatches < 5 {
						t.Errorf("engine wrote %q, we produce %q", tok, got)
					}
					mismatches++
				}
			}
		}
	}
	if tokens < 1000 {
		t.Fatalf("only %d float tokens found; the fixture looks wrong", tokens)
	}
	if mismatches > 0 {
		t.Errorf("%d of %d float tokens do not match the engine's spelling", mismatches, tokens)
		return
	}
	t.Logf("all %d float tokens reproduce the engine's spelling exactly", tokens)
}

// floatLiteralsOutsideStrings returns every JSON number containing a decimal
// point that appears outside a quoted string. Bare integers are excluded: they
// are ambiguous between an int field and a whole-valued double, which the engine
// spells differently.
func floatLiteralsOutsideStrings(data []byte) []string {
	var out []string
	inString := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' && !isEscaped(data, i) {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if data[i] != '-' && (data[i] < '0' || data[i] > '9') {
			continue
		}
		start := i
		hasDot := false
		for i < len(data) {
			c := data[i]
			if c >= '0' && c <= '9' || c == '-' || c == '+' || c == 'e' || c == 'E' {
				i++
				continue
			}
			if c == '.' {
				hasDot = true
				i++
				continue
			}
			break
		}
		if hasDot {
			out = append(out, string(data[start:i]))
		}
		i--
	}
	return out
}

func TestEngineFloatKeysDerivedFromSchema(t *testing.T) {
	t.Parallel()

	// Spot-check both directions rather than pinning the whole set, so adding a
	// float field to the proto does not fail this test spuriously.
	for _, k := range []string{"position", "velocity", "game_clock", "arm_speed", "bone_t", "packetlossratio"} {
		if !engineFloatKeys[k] {
			t.Errorf("engineFloatKeys missing float-typed key %q", k)
		}
	}
	for _, k := range []string{"blue_points", "total_round_count", "err_code", "playerid", "userid", "name"} {
		if engineFloatKeys[k] {
			t.Errorf("engineFloatKeys wrongly contains non-float key %q", k)
		}
	}
}

// BenchmarkFloatFixers compares the engine-float rewrite against the exponent
// fixer it replaced, on a real frame's session JSON. Both walk the whole buffer,
// so the question is what the extra key tracking and reformatting cost.
func BenchmarkFloatFixers(b *testing.B) {
	src := filepath.Join("..", "..", "testdata", "sample.echoreplay")
	if _, err := os.Stat(src); err != nil {
		b.Skipf("no sample: %v", err)
	}
	zr, err := zip.OpenReader(src)
	if err != nil {
		b.Fatalf("open sample: %v", err)
	}
	defer zr.Close() //nolint:errcheck // read-only fixture
	rc, err := zr.File[0].Open()
	if err != nil {
		b.Fatalf("open member: %v", err)
	}
	body, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		b.Fatalf("read member: %v", err)
	}
	line := bytes.Split(body, []byte("\n"))[0]
	session := bytes.Split(bytes.TrimRight(line, "\r"), []byte("\t"))[1]

	b.Run("EngineFloatFormatting", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(session)))
		for b.Loop() {
			_ = FixEngineFloatFormatting(session)
		}
	})
	b.Run("ExponentNotation", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(session)))
		for b.Loop() {
			_ = FixExponentNotation(session)
		}
	})
}
