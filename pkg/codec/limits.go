// Hostile-input resource guards for the tape decoders.
//
// BUGS.md SEC-001 (decompression bomb) and SEC-002 (allocate-before-verify)
// document the attacks these guards close. The principle is audit-not-restrict:
// every limit is documented, configurable, and disableable, so a legitimate
// giant capture stays readable by explicit opt-in — but a crafted few-KB file
// can no longer force gigabytes of allocation by default.
package codec

import (
	"fmt"
	"io"
	"slices"
)

const (
	// maxEagerMessageAlloc bounds the buffer allocated for a length-delimited
	// message BEFORE any of its bytes have been read (SEC-002). Messages larger
	// than this are still read in full — the buffer grows in steps of this size
	// as bytes actually arrive, so allocation tracks bytes present in the
	// stream, not an attacker's declared length.
	//
	// Source: measured message sizes on the repo samples (testdata/): the v2
	// tape averages ~1.8 KB/frame decoded (sample.tape.golden: 1,862,589 bytes
	// / 1023 frames) and the v1/echoreplay lane ~8.5 KB/frame
	// (sample.echoreplay: 8,757,915 bytes decoded / 1023 frames). 1 MiB is
	// >100x the largest observed per-message size.
	maxEagerMessageAlloc = 1 << 20 // 1 MiB
)

// readMessageBody reads exactly length bytes from r.
//
// SEC-002 guard: the buffer is never eagerly sized to the declared length.
// Up to maxEagerMessageAlloc bytes are allocated up front; beyond that the
// buffer grows only as bytes are actually read, so a tiny stream declaring a
// 256 MiB message costs a bounded allocation plus a clean error — not 256 MiB.
func readMessageBody(r io.Reader, length uint64) ([]byte, error) {
	if length <= maxEagerMessageAlloc {
		data := make([]byte, length)
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, err
		}
		return data, nil
	}

	data := make([]byte, 0, maxEagerMessageAlloc)
	for remaining := length; remaining > 0; {
		chunk := int(min(remaining, maxEagerMessageAlloc)) //nolint:gosec // bounded by maxEagerMessageAlloc (1 MiB), always fits int
		start := len(data)
		data = slices.Grow(data, chunk)[:start+chunk]
		if _, err := io.ReadFull(r, data[start:]); err != nil {
			// A mid-message EOF is an unexpected end of the message, even when
			// ReadFull saw zero bytes of this chunk.
			if err == io.EOF && start > 0 {
				err = io.ErrUnexpectedEOF
			}
			return nil, fmt.Errorf("message truncated after %d of %d declared bytes: %w", start, length, err)
		}
		remaining -= uint64(chunk)
	}
	return data, nil
}
