package codec

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
)

const (
	// ExtTape is the default file extension for nevrcap captures.
	ExtTape = ".tape"

	// ExtNevrcap is the legacy file extension, still accepted as input.
	ExtNevrcap = ".nevrcap"

	// MaxMessageSize is the upper bound on a single varint-delimited protobuf
	// message. Prevents corrupted files from triggering multi-gigabyte allocations.
	// 256 MB is generous for any realistic telemetry frame.
	MaxMessageSize = 256 * 1024 * 1024
)

// IsValidExtension reports whether ext (including the leading dot) is a
// recognized nevrcap file extension.
func IsValidExtension(ext string) bool {
	return ext == ExtTape || ext == ExtNevrcap
}

// DefaultOutputFilename returns the given path with its extension replaced
// by the default output extension (.tape). If the path has no extension,
// .tape is appended.
func DefaultOutputFilename(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return path + ExtTape
	}
	return path[:len(path)-len(ext)] + ExtTape
}

// LegacyReader reads from Zstd-compressed .tape / .nevrcap files using the
// v1 length-delimited protobuf format (no envelope wrapper).
type LegacyReader struct {
	file    *os.File
	decoder *zstd.Decoder
	reader  io.Reader

	// limits bounds hostile-input resource use (SEC-001); see Limits.
	limits Limits
	// framesRead counts frames returned, checked against limits.MaxFrameCount.
	framesRead int64
}

// NewLegacyReader creates a new reader for .tape / .nevrcap files. Reads are
// bounded by DefaultLimits (SEC-001 decompression-bomb guard); pass
// ReaderOptions to raise or disable the budgets.
func NewLegacyReader(filename string, opts ...ReaderOption) (*LegacyReader, error) {
	return NewLegacyReaderWithProgress(filename, nil, opts...)
}

// NewLegacyReaderWithProgress creates a new reader that also copies
// compressed bytes to progress (e.g. for a progress bar).
func NewLegacyReaderWithProgress(filename string, progress io.Writer, opts ...ReaderOption) (*LegacyReader, error) {
	limits := applyReaderOptions(opts)

	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	var src io.Reader = file
	if progress != nil {
		src = io.TeeReader(file, progress)
	}

	// SEC-001: cap the decoder's own memory (streaming window) alongside the
	// total decoded-bytes budget enforced by budgetReader.
	var zstdOpts []zstd.DOption
	if limits.MaxDecodedBytes > 0 {
		zstdOpts = append(zstdOpts, zstd.WithDecoderMaxMemory(uint64(limits.MaxDecodedBytes)))
	}
	decoder, err := zstd.NewReader(src, zstdOpts...)
	if err != nil {
		file.Close()
		return nil, err
	}

	return &LegacyReader{
		file:    file,
		decoder: decoder,
		reader:  newBudgetReader(decoder, limits.MaxDecodedBytes),
		limits:  limits,
	}, nil
}

// Limits returns the resource budgets in effect for this reader.
func (z *LegacyReader) Limits() Limits { return z.limits }

// ReadHeader reads the nevrcap header from the file.
func (z *LegacyReader) ReadHeader() (*telemetry.TelemetryHeader, error) {
	data, err := z.readDelimitedMessage()
	if err != nil {
		return nil, err
	}

	header := &telemetry.TelemetryHeader{}
	err = proto.Unmarshal(data, header)
	if err != nil {
		return nil, err
	}

	return header, nil
}

// ReadFrame reads a frame from the file.
func (z *LegacyReader) ReadFrame() (*telemetry.LobbySessionStateFrame, error) {
	data, err := z.readDelimitedMessage()
	if err != nil {
		return nil, err
	}

	frame := &telemetry.LobbySessionStateFrame{}
	err = proto.Unmarshal(data, frame)
	if err != nil {
		return nil, err
	}

	// SEC-001: bound the number of frames a capture may yield.
	z.framesRead++
	if err := z.limits.checkFrameBudget(z.framesRead); err != nil {
		return nil, fmt.Errorf("legacy tape: %w", err)
	}

	return frame, nil
}

// ReadFrameTo reads a frame into the provided frame object.
func (z *LegacyReader) ReadFrameTo(frame *telemetry.LobbySessionStateFrame) (bool, error) {
	data, err := z.readDelimitedMessage()
	if err != nil {
		if err == io.EOF {
			return false, err
		}
		return false, err
	}

	proto.Reset(frame)
	err = proto.Unmarshal(data, frame)
	if err != nil {
		return false, err
	}

	// SEC-001: bound the number of frames a capture may yield.
	z.framesRead++
	if err := z.limits.checkFrameBudget(z.framesRead); err != nil {
		return false, fmt.Errorf("legacy tape: %w", err)
	}

	return true, nil
}

// readDelimitedMessage reads a length-delimited protobuf message.
func (z *LegacyReader) readDelimitedMessage() ([]byte, error) {
	// Read varint length
	var length uint64
	var shift uint
	var b [1]byte // reuse the same byte array
	for {
		if _, err := z.reader.Read(b[:]); err != nil {
			return nil, err
		}

		length |= uint64(b[0]&0x7F) << shift
		if b[0]&0x80 == 0 {
			break
		}
		shift += 7
		if shift >= 64 {
			return nil, io.ErrUnexpectedEOF
		}
	}

	if length > MaxMessageSize {
		return nil, fmt.Errorf("message size %d exceeds maximum %d", length, MaxMessageSize)
	}

	// Read message data. SEC-002: allocation is bounded by bytes actually
	// present in the stream, not by the declared length.
	return readMessageBody(z.reader, length)
}

// Close closes the decoder and underlying file.
func (z *LegacyReader) Close() error {
	if z.decoder != nil {
		z.decoder.Close()
	}

	if z.file != nil {
		return z.file.Close()
	}

	return nil
}
