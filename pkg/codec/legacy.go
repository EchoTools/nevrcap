package codec

import (
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
}

// NewLegacyReader creates a new reader for .tape / .nevrcap files.
func NewLegacyReader(filename string) (*LegacyReader, error) {
	return NewLegacyReaderWithProgress(filename, nil)
}

// NewLegacyReaderWithProgress creates a new reader that also copies
// compressed bytes to progress (e.g. for a progress bar).
func NewLegacyReaderWithProgress(filename string, progress io.Writer) (*LegacyReader, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	var src io.Reader = file
	if progress != nil {
		src = io.TeeReader(file, progress)
	}

	decoder, err := zstd.NewReader(src)
	if err != nil {
		file.Close()
		return nil, err
	}

	return &LegacyReader{
		file:    file,
		decoder: decoder,
		reader:  decoder,
	}, nil
}

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

	err = proto.Unmarshal(data, frame)
	if err != nil {
		return false, err
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

	// Read message data
	data := make([]byte, length)
	_, err := io.ReadFull(z.reader, data)
	return data, err
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
