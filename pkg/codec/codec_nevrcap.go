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

// TapeV1 handles streaming to/from Zstd-compressed .tape / .nevrcap files
type TapeV1 struct {
	file    *os.File
	encoder *zstd.Encoder
	decoder *zstd.Decoder
	writer  io.Writer
	reader  io.Reader
}

// NewTapeV1Writer creates a new Zstd codec for writing .tape / .nevrcap files
func NewTapeV1Writer(filename string) (*TapeV1, error) {
	file, err := os.Create(filename)
	if err != nil {
		return nil, err
	}

	encoder, err := zstd.NewWriter(file, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		file.Close()
		return nil, err
	}

	return &TapeV1{
		file:    file,
		encoder: encoder,
		writer:  encoder,
	}, nil
}

// NewTapeV1Reader creates a new Zstd codec for reading .tape / .nevrcap files
func NewTapeV1Reader(filename string) (*TapeV1, error) {
	return NewTapeV1ReaderWithProgress(filename, nil)
}

func NewTapeV1ReaderWithProgress(filename string, progress io.Writer) (*TapeV1, error) {
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

	return &TapeV1{
		file:    file,
		decoder: decoder,
		reader:  decoder,
	}, nil
}

// WriteHeader writes the nevrcap header to the file
func (z *TapeV1) WriteHeader(header *telemetry.TelemetryHeader) error {
	data, err := proto.Marshal(header)
	if err != nil {
		return err
	}

	// Write length-delimited message
	return z.writeDelimitedMessage(data)
}

// WriteFrame writes a frame to the file
func (z *TapeV1) WriteFrame(frame *telemetry.LobbySessionStateFrame) error {
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}

	// Write length-delimited message
	return z.writeDelimitedMessage(data)
}

// ReadHeader reads the nevrcap header from the file
func (z *TapeV1) ReadHeader() (*telemetry.TelemetryHeader, error) {
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

// ReadFrame reads a frame from the file
func (z *TapeV1) ReadFrame() (*telemetry.LobbySessionStateFrame, error) {
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

// ReadFrameTo reads a frame into the provided frame object
func (z *TapeV1) ReadFrameTo(frame *telemetry.LobbySessionStateFrame) (bool, error) {
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

// writeDelimitedMessage writes a length-delimited protobuf message
func (z *TapeV1) writeDelimitedMessage(data []byte) error {
	// Buffer for varint encoding (max 10 bytes for uint64)
	var buf [10]byte
	length := uint64(len(data))
	i := 0
	for length >= 0x80 {
		buf[i] = byte(length) | 0x80
		length >>= 7
		i++
	}
	buf[i] = byte(length)
	i++

	// Write varint length in a single call
	if _, err := z.writer.Write(buf[:i]); err != nil {
		return err
	}

	// Write message data
	_, err := z.writer.Write(data)
	return err
}

// readDelimitedMessage reads a length-delimited protobuf message
func (z *TapeV1) readDelimitedMessage() ([]byte, error) {
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

// Close closes the codec and underlying file
func (z *TapeV1) Close() error {
	var err error

	if z.encoder != nil {
		err = z.encoder.Close()
	}

	if z.decoder != nil {
		z.decoder.Close()
	}

	if z.file != nil {
		if closeErr := z.file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}

	return err
}
