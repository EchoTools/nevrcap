package codec

import (
	"bytes"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"google.golang.org/protobuf/proto"
)

// Fuzz targets for the length-delimited stream parsers (SEC-001/SEC-002
// hardening). The parsers must never panic and never allocate proportionally
// to an attacker-declared length; they consume the decompressed byte stream,
// so the fuzzer drives them directly (white-box, post-zstd) where its
// mutations have full effect.

// fuzzBudget keeps hostile inputs bounded during fuzzing, matching how the
// readers are wired in production (budgetReader between decoder and parser).
const fuzzBudget = 1 << 20 // 1 MiB

// FuzzReadEnvelope feeds arbitrary bytes into the v2 envelope stream parser.
func FuzzReadEnvelope(f *testing.F) {
	// Seed: a valid header envelope followed by a valid empty frame envelope.
	var seed bytes.Buffer
	for _, env := range []*capturepb.Envelope{
		{Message: &capturepb.Envelope_Header{Header: &capturepb.CaptureHeader{}}},
		{Message: &capturepb.Envelope_Frame{Frame: &capturepb.Frame{}}},
	} {
		data, err := proto.Marshal(env)
		if err != nil {
			f.Fatalf("marshal seed: %v", err)
		}
		var lenBuf [10]byte
		l := uint64(len(data))
		i := 0
		for l >= 0x80 {
			lenBuf[i] = byte(l) | 0x80
			l >>= 7
			i++
		}
		lenBuf[i] = byte(l)
		seed.Write(lenBuf[:i+1])
		seed.Write(data)
	}
	f.Add(seed.Bytes())
	// Seed: varint declaring MaxMessageSize (256 MiB) then EOF (SEC-002).
	f.Add([]byte{0x80, 0x80, 0x80, 0x80, 0x01})
	// Seed: over-long varint (shift overflow path).
	f.Add(bytes.Repeat([]byte{0xFF}, 11))

	f.Fuzz(func(t *testing.T, data []byte) {
		r := &Reader{reader: newBudgetReader(bytes.NewReader(data), fuzzBudget)}
		for {
			if _, err := r.readEnvelope(); err != nil {
				break
			}
		}
	})
}

// FuzzReadDelimitedMessage feeds arbitrary bytes into the v1 legacy
// length-delimited stream parser.
func FuzzReadDelimitedMessage(f *testing.F) {
	f.Add([]byte{0x00})                         // zero-length message
	f.Add([]byte{0x03, 0x0A, 0x01, 0x41})       // small delimited payload
	f.Add([]byte{0x80, 0x80, 0x80, 0x80, 0x01}) // 256 MiB claim, no body (SEC-002)
	f.Add(bytes.Repeat([]byte{0xFF}, 11))       // varint shift overflow
	f.Add([]byte{0x05, 0x01, 0x02})             // truncated body

	f.Fuzz(func(t *testing.T, data []byte) {
		z := &LegacyReader{reader: newBudgetReader(bytes.NewReader(data), fuzzBudget)}
		for {
			if _, err := z.readDelimitedMessage(); err != nil {
				break
			}
		}
	})
}
