package codec

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/klauspost/compress/zstd"
)

// Per-block container layout — opt-in, and OFF by default.
//
// WHY THIS EXISTS. Writer's own doc comment concedes the defect: "Because the
// stream is Zstd-compressed, byte-offset seeking requires decompressing from
// the start." One zstd.NewWriter wraps the whole file, so every
// KeyframeEntry.ByteOffset is a position in the DECOMPRESSED stream and
// nothing can seek to it. The keyframe index shipped; the property it exists
// for did not. This file supplies the property.
//
// THE LAYOUT. Compression moves inside the container: instead of one zstd
// stream over the whole file, the file becomes a sequence of INDEPENDENT zstd
// frames, and a block boundary falls on a keyframe. One decision does two jobs
// — the keyframe interval is also the block size — rather than two numbers
// drifting apart.
//
//	[zstd frame]  header block                — the CaptureHeader alone
//	[zstd frame]  block 0                     — opens at keyframe, N frames
//	[zstd frame]  block 1                     — opens at the next keyframe
//	...
//	[zstd frame]  footer block                — the CaptureFooter alone
//	[skippable]   seek table (seekable.go)    — block sizes, locatable from EOF
//
// The header gets its own block so that rewriting it — the metadata edit that
// a strip or a re-derivation performs — recompresses one small block instead of
// the first N frames with it. The footer gets its own block for the same
// reason and because it is written last.
//
// WHAT THIS COSTS EXISTING READERS: nothing. zstd decoders are specified to
// decode concatenated frames as one continuous stream and to skip skippable
// frames, so the shipped sequential Reader reads a per-block tape unchanged.
// That is not inherited from the spec — it is verified against the pinned
// klauspost/compress in zstd_container_behavior_test.go, and those tests stay
// as gates.
//
// WHAT IT CHANGES ON DISK: the byte layout, and the meaning of
// KeyframeEntry.ByteOffset, which in this mode is the offset of the block in
// the COMPRESSED file rather than in the decompressed stream. That is a format
// change and it is therefore opt-in: NewWriter and
// NewWriterWithKeyframeInterval keep producing exactly the layout they always
// produced. Nothing writes the new layout unless a caller asks for it by name.

// WriterOption configures a Writer. Options that change the on-disk layout say
// so in their own doc comment; the zero-option writer is byte-for-byte the
// layout tape has always written.
type WriterOption func(*writerConfig)

// writerConfig is the folded result of the options passed to
// NewWriterWithOptions.
type writerConfig struct {
	dict             []byte
	level            zstd.EncoderLevel
	keyframeInterval uint32
	perBlock         bool
}

// WithKeyframeInterval sets how often a keyframe is recorded, in frames. Zero
// is clamped to DefaultKeyframeInterval (GH #23: it reached
// `frameIndex % keyframeInterval` and panicked with a divide by zero).
//
// Under WithPerBlockCompression this interval is also the block size, so it
// sets seek granularity as well as index density.
func WithKeyframeInterval(n uint32) WriterOption {
	return func(c *writerConfig) { c.keyframeInterval = n }
}

// WithCompressionLevel sets the zstd encoder level. The default is
// zstd.SpeedFastest, which is what the shipped writer has always used.
func WithCompressionLevel(level zstd.EncoderLevel) WriterOption {
	return func(c *writerConfig) { c.level = level }
}

// WithPerBlockCompression writes the capture as independent per-block zstd
// frames plus a seek table, instead of one continuous zstd stream.
//
// THIS CHANGES THE ON-DISK LAYOUT and redefines KeyframeEntry.ByteOffset to a
// compressed-file offset. It is opt-in for exactly that reason. What it buys:
// a keyframe offset becomes a servable byte range, and reaching frame N costs
// one block instead of the whole file.
func WithPerBlockCompression() WriterOption {
	return func(c *writerConfig) { c.perBlock = true }
}

// WithDictionary compresses with a trained zstd dictionary. Telemetry is close
// to a dictionary's ideal case — many small payloads with identical field
// structure and no shared window — which is what makes per-block compression
// cheap rather than costly.
//
// A dictionary is a PERMANENT OBLIGATION: a capture written with dictionary D
// needs D forever to be read. zstd records the dictionary's id in every frame
// header it writes, so a reader without it fails loudly rather than returning
// wrong bytes (verified in zstd_container_behavior_test.go), but the bytes
// themselves must be kept somewhere the reader can find. Passing nil is the
// same as not passing the option.
func WithDictionary(dict []byte) WriterOption {
	return func(c *writerConfig) { c.dict = dict }
}

// applyWriterOptions folds opts over the shipped defaults.
func applyWriterOptions(opts []WriterOption) writerConfig {
	c := writerConfig{
		keyframeInterval: DefaultKeyframeInterval,
		level:            zstd.SpeedFastest,
	}
	for _, opt := range opts {
		opt(&c)
	}
	if c.keyframeInterval == 0 {
		c.keyframeInterval = DefaultKeyframeInterval
	}
	return c
}

// blockWriter accumulates envelope bytes for the block being built and emits
// each finished block as its own zstd frame.
//
// It compresses with EncodeAll on a reusable encoder rather than opening a
// zstd.Writer per block: a block is complete in memory before it is written,
// and constructing an encoder per keyframe would dominate the cost of a
// one-second interval.
type blockWriter struct {
	file *os.File
	enc  *zstd.Encoder
	// buf holds the uncompressed envelope bytes of the block in progress. It
	// is a pointer so the struct's pointer-bearing prefix stays compact
	// (govet fieldalignment); a Buffer by value drags the seek table entries
	// past it.
	buf *bytes.Buffer
	// entries is the seek table under construction, one entry per block
	// already written.
	entries []seekTableEntry
	// offset is the number of bytes already written to the file, which is
	// where the next block will begin. This is the number that makes a
	// keyframe offset servable.
	offset uint64
}

// newBlockWriter constructs the block machinery for a per-block capture.
func newBlockWriter(file *os.File, c writerConfig) (*blockWriter, error) {
	opts := []zstd.EOption{zstd.WithEncoderLevel(c.level)}
	if len(c.dict) > 0 {
		opts = append(opts, zstd.WithEncoderDict(c.dict))
	}
	// A nil destination means this encoder is used only through EncodeAll.
	enc, err := zstd.NewWriter(nil, opts...)
	if err != nil {
		return nil, fmt.Errorf("tape: per-block encoder: %w", err)
	}
	return &blockWriter{file: file, enc: enc, buf: &bytes.Buffer{}}, nil
}

// write appends raw bytes to the block in progress. It never fails: the bytes
// go to memory, and the only I/O happens in flush.
func (b *blockWriter) write(p []byte) {
	b.buf.Write(p) //nolint:errcheck // bytes.Buffer.Write never returns an error
}

// flush closes the block in progress, writing it as one independent zstd frame
// and recording its seek table entry. A block with nothing in it is not
// written: an empty zstd frame would occupy a seek table slot that maps to no
// frames, so a reader stepping blocks would see a hole.
func (b *blockWriter) flush() error {
	if b.buf.Len() == 0 {
		return nil
	}
	raw := b.buf.Bytes()
	if len(raw) > math.MaxUint32 {
		return fmt.Errorf("tape: block of %d uncompressed bytes exceeds the seek table's uint32 field: %w",
			len(raw), ErrSeekTableCorrupt)
	}
	compressed := b.enc.EncodeAll(raw, nil)
	if len(compressed) > math.MaxUint32 {
		return fmt.Errorf("tape: block of %d compressed bytes exceeds the seek table's uint32 field: %w",
			len(compressed), ErrSeekTableCorrupt)
	}
	n, err := b.file.Write(compressed)
	b.offset += uint64(max(n, 0)) //nolint:gosec // n is non-negative on success
	if err != nil {
		return fmt.Errorf("tape: writing block: %w", err)
	}
	b.entries = append(b.entries, seekTableEntry{
		compressedSize:   uint32(len(compressed)), //nolint:gosec // bounded above
		decompressedSize: uint32(len(raw)),        //nolint:gosec // bounded above
	})
	b.buf.Reset()
	return nil
}

// finish flushes any remaining block and appends the seek table. After it
// returns the file is a complete per-block capture.
func (b *blockWriter) finish() error {
	if err := b.flush(); err != nil {
		return err
	}
	table, err := appendSeekTable(nil, b.entries)
	if err != nil {
		return err
	}
	if _, err := b.file.Write(table); err != nil {
		return errors.Join(fmt.Errorf("tape: writing seek table: %w", err), b.enc.Close())
	}
	if err := b.enc.Close(); err != nil {
		return fmt.Errorf("tape: closing per-block encoder: %w", err)
	}
	return nil
}
