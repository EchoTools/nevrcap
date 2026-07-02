// Hostile-input resource guards for the tape decoders.
//
// BUGS.md SEC-001 (decompression bomb) and SEC-002 (allocate-before-verify)
// document the attacks these guards close. The principle is audit-not-restrict:
// every limit is documented, configurable, and disableable, so a legitimate
// giant capture stays readable by explicit opt-in — but a crafted few-KB file
// can no longer force gigabytes of allocation by default.
package codec

import (
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/klauspost/compress/zstd"
)

const (
	// DefaultMaxDecodedBytes is the default budget for total decoded
	// (uncompressed) bytes per capture read (SEC-001).
	//
	// Source: measured decoded sizes on the repo samples (testdata/): the v2
	// tape decodes to ~1.8 KB/frame (sample.tape.golden: 1,862,589 bytes /
	// 1023 frames) and the v1/echoreplay lane to ~8.5 KB/frame
	// (sample.echoreplay: 8,757,915 bytes / 1023 frames). The largest audited
	// real capture is 22,727 frames (docs/format-design.md §3) ≈ 195 MB
	// decoded on the heavier lane. 8 GiB is ~40x that ceiling — no real
	// capture is refused, while a bomb decoding kilobytes into gigabytes is
	// stopped with a clear error. Raise with WithMaxDecodedBytes or disable
	// with WithoutLimits for a legitimately larger capture.
	DefaultMaxDecodedBytes int64 = 8 << 30 // 8 GiB

	// DefaultMaxFrameCount is the default budget for frames read per capture
	// (SEC-001).
	//
	// Source: the largest audited real capture is 22,727 frames
	// (docs/format-design.md §3); the fastest supported capture rate is
	// 600 Hz (pkg/processing, CLAUDE.md). 10M frames ≈ 4.6 hours of
	// continuous 600 Hz capture, ~440x the largest observed recording. Raise
	// with WithMaxFrameCount or disable with WithoutLimits.
	DefaultMaxFrameCount int64 = 10_000_000
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

var (
	// ErrMaxDecodedBytes is returned when a capture's decoded stream exceeds
	// the configured MaxDecodedBytes budget (SEC-001: decompression bomb).
	ErrMaxDecodedBytes = errors.New("decoded stream exceeds MaxDecodedBytes budget; raise with codec.WithMaxDecodedBytes or disable with codec.WithoutLimits")

	// ErrMaxFrameCount is returned when a capture yields more frames than the
	// configured MaxFrameCount budget (SEC-001: decompression bomb).
	ErrMaxFrameCount = errors.New("frame count exceeds MaxFrameCount budget; raise with codec.WithMaxFrameCount or disable with codec.WithoutLimits")
)

// Limits bounds the resources a reader will spend on a single capture, so a
// tiny hostile file cannot force unbounded memory use (SEC-001). A zero or
// negative field disables that limit — the opt-in escape hatch that keeps a
// legitimate giant capture readable.
type Limits struct {
	// MaxDecodedBytes caps total decoded (uncompressed) bytes read from the
	// capture stream. <= 0 disables the cap.
	MaxDecodedBytes int64
	// MaxFrameCount caps the number of frames read from the capture.
	// <= 0 disables the cap.
	MaxFrameCount int64
}

// DefaultLimits returns the budgets applied when a reader is constructed
// without options. See DefaultMaxDecodedBytes / DefaultMaxFrameCount for the
// sizing rationale.
func DefaultLimits() Limits {
	return Limits{
		MaxDecodedBytes: DefaultMaxDecodedBytes,
		MaxFrameCount:   DefaultMaxFrameCount,
	}
}

// ReaderOption adjusts the resource limits of a capture reader.
type ReaderOption func(*Limits)

// WithMaxDecodedBytes sets the total decoded-bytes budget. n <= 0 disables it.
func WithMaxDecodedBytes(n int64) ReaderOption {
	return func(l *Limits) { l.MaxDecodedBytes = n }
}

// WithMaxFrameCount sets the frame-count budget. n <= 0 disables it.
func WithMaxFrameCount(n int64) ReaderOption {
	return func(l *Limits) { l.MaxFrameCount = n }
}

// WithoutLimits disables every resource limit. Explicit opt-in for reading a
// trusted, legitimately giant capture.
func WithoutLimits() ReaderOption {
	return func(l *Limits) { *l = Limits{} }
}

// applyReaderOptions folds opts over the default limits.
func applyReaderOptions(opts []ReaderOption) Limits {
	limits := DefaultLimits()
	for _, opt := range opts {
		opt(&limits)
	}
	return limits
}

// checkFrameBudget returns ErrMaxFrameCount when framesRead exceeds the
// frame-count budget. framesRead is the count INCLUDING the frame just read.
func (l Limits) checkFrameBudget(framesRead int64) error {
	if l.MaxFrameCount > 0 && framesRead > l.MaxFrameCount {
		return fmt.Errorf("frame %d: %w (budget %d)", framesRead, ErrMaxFrameCount, l.MaxFrameCount)
	}
	return nil
}

// budgetReader enforces a total decoded-bytes budget over r (SEC-001). Once
// the budget is exhausted the next Read fails with ErrMaxDecodedBytes; a
// single Read may overshoot by at most one caller buffer (<= 1 MiB with
// readMessageBody), which keeps the guard O(1) per read.
type budgetReader struct {
	r        io.Reader
	limit    int64
	consumed int64
}

// newBudgetReader wraps r with a decoded-bytes budget. limit <= 0 returns r
// unwrapped (unlimited).
func newBudgetReader(r io.Reader, limit int64) io.Reader {
	if limit <= 0 {
		return r
	}
	return &budgetReader{r: r, limit: limit}
}

func (b *budgetReader) Read(p []byte) (int, error) {
	// Fail BEFORE reading once the budget is spent: returning the error with
	// n > 0 would let io.ReadFull swallow it while data keeps flowing.
	if b.consumed >= b.limit {
		return 0, fmt.Errorf("%d bytes decoded: %w (budget %d)", b.consumed, ErrMaxDecodedBytes, b.limit)
	}
	n, err := b.r.Read(p)
	b.consumed += int64(n)
	// The zstd decoder enforces its own memory bound (WithDecoderMaxMemory,
	// set from the same budget). Translate its limit errors into the
	// documented sentinel so callers have one contract regardless of which
	// layer trips first.
	if err != nil && (errors.Is(err, zstd.ErrDecoderSizeExceeded) || errors.Is(err, zstd.ErrWindowSizeExceeded)) {
		err = fmt.Errorf("zstd decoder limit (%v): %w (budget %d)", err, ErrMaxDecodedBytes, b.limit)
	}
	return n, err
}

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
