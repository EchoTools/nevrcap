package nevrcap

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/echotools/nevr-common/v4/gen/go/apigame"
	"github.com/echotools/nevr-common/v4/gen/go/rtapi"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	EchoReplayTimeFormat = "2006/01/02 15:04:05.000"
)

var (
	ErrCodecNotConfiguredForWriting = fmt.Errorf("codec not configured for writing")
)

// Use protojson marshaling for compatibility with existing format
var echoReplayerMarshaler = &protojson.MarshalOptions{
	UseProtoNames:   false,
	UseEnumNumbers:  true,
	EmitUnpopulated: true,
}

// EchoReplayCodec handles .echoreplay file format (zip format)
type EchoReplayCodec struct {
	filename    string
	zipWriter   *zip.Writer
	zipReader   *zip.ReadCloser
	file        *os.File
	frameBuffer *bytes.Buffer

	// Streaming state
	scanner     *bufio.Scanner
	frameIndex  uint32
	replayFile  io.ReadCloser
	unmarshaler *protojson.UnmarshalOptions
	// Reusable buffer for timestamp parsing to avoid allocations
	timestampBuf [len(EchoReplayTimeFormat)]byte
}

// EchoReplayFrame represents a frame in the .echoreplay format
type EchoReplayFrame struct {
	Timestamp   string                       `json:"timestamp"`
	Session     *apigame.SessionResponse     `json:"session"`
	PlayerBones *apigame.PlayerBonesResponse `json:"user_bones,omitempty"`
}

// NewEchoReplayCodecWriter creates a new EchoReplay codec for writing
func NewEchoReplayCodecWriter(filename string) (*EchoReplayCodec, error) {
	file, err := os.Create(filename)
	if err != nil {
		return nil, err
	}

	zipWriter := zip.NewWriter(file)

	return &EchoReplayCodec{
		filename:    filename,
		file:        file,
		zipWriter:   zipWriter,
		frameBuffer: &bytes.Buffer{},
	}, nil
}

// NewEchoReplayFileReader creates a new EchoReplay codec for reading
func NewEchoReplayFileReader(filename string) (*EchoReplayCodec, error) {
	zipReader, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}

	codec := &EchoReplayCodec{
		filename:  filename,
		zipReader: zipReader,
		unmarshaler: &protojson.UnmarshalOptions{
			DiscardUnknown: false,
		},
	}

	// Initialize the scanner for streaming
	if err := codec.initScanner(); err != nil {
		zipReader.Close()
		return nil, err
	}

	return codec, nil
}

// initScanner initializes the scanner for streaming frame reads
func (e *EchoReplayCodec) initScanner() error {
	var replayFile *zip.File

	// Look for files in order of preference:
	// 1. File with same name as zip (without .zip extension)
	// 4. Any .echoreplay file
	baseFilename := filepath.Base(e.filename)
	if ext := filepath.Ext(baseFilename); ext != "" {
		baseFilename = baseFilename[:len(baseFilename)-len(ext)]
	}

	for _, file := range e.zipReader.File {
		if file.Name == baseFilename {
			replayFile = file
			break
		}
	}

	if replayFile == nil {
		for _, file := range e.zipReader.File {
			if filepath.Ext(file.Name) == ".echoreplay" {
				replayFile = file
				break
			}
		}
	}

	if replayFile == nil {
		return fmt.Errorf("no `.echoreplay` file found in zip")
	}

	reader, err := replayFile.Open()
	if err != nil {
		return err
	}

	e.replayFile = reader
	e.scanner = bufio.NewScanner(reader)
	e.frameIndex = 0

	return nil
}

// WriteFrame writes a frame to the .echoreplay file using optimized buffer operations
func (e *EchoReplayCodec) WriteFrame(frame *rtapi.LobbySessionStateFrame) error {
	if e.zipWriter == nil {
		return ErrCodecNotConfiguredForWriting
	}

	// Use the optimized writeReplayFrame method
	e.WriteReplayFrame(e.frameBuffer, frame)
	return nil
}

// WriteFrameBatch writes multiple frames efficiently in a single operation
func (e *EchoReplayCodec) WriteFrameBatch(frames []*rtapi.LobbySessionStateFrame) error {
	if e.zipWriter == nil {
		return ErrCodecNotConfiguredForWriting
	}

	for _, frame := range frames {
		e.WriteReplayFrame(e.frameBuffer, frame)
	}
	return nil
}

// FlushBuffer forces a flush of the internal buffer (useful for periodic flushing)
func (e *EchoReplayCodec) FlushBuffer() error {
	if e.zipWriter == nil {
		return ErrCodecNotConfiguredForWriting
	}

	// For this implementation, we buffer everything until Finalize()
	// This could be enhanced to support intermediate flushing if needed
	return nil
}

// GetBufferSize returns the current size of the internal buffer
func (e *EchoReplayCodec) GetBufferSize() int {
	if e.frameBuffer == nil {
		return 0
	}
	return e.frameBuffer.Len()
}

// WriteReplayFrame writes a frame using optimized buffer operations (same approach as writer_replay_file.go)
func (e *EchoReplayCodec) WriteReplayFrame(dst *bytes.Buffer, frame *rtapi.LobbySessionStateFrame) int {
	// Get a JSON buffer from the pool

	sessionData, err := echoReplayerMarshaler.Marshal(frame.GetSession())
	if err != nil {
		return 0
	}

	bonesData, err := json.Marshal(frame.GetPlayerBones())
	if err != nil {
		return 0
	}

	return e.writeReplayLine(dst, frame.Timestamp.AsTime(), sessionData, bonesData)
}

// Finalize writes the buffered data to the zip file and closes it
func (e *EchoReplayCodec) Finalize() error {
	if e.zipWriter == nil {
		return ErrCodecNotConfiguredForWriting
	}

	// Create the main replay file in the zip - use the filename
	baseFilename := filepath.Base(e.filename)
	replayFile, err := e.zipWriter.Create(baseFilename)
	if err != nil {
		return err
	}

	// Write the buffered frame data
	_, err = io.Copy(replayFile, e.frameBuffer)
	if err != nil {
		return err
	}

	return err
}

// ReadFrame reads the next frame from the .echoreplay file
func (e *EchoReplayCodec) ReadFrame() (*rtapi.LobbySessionStateFrame, error) {
	if e.scanner == nil {
		return nil, fmt.Errorf("codec not configured for reading or already closed")
	}

	for e.scanner.Scan() {
		line := e.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		frame, err := e.parseFrameLine(line)
		if err != nil {
			continue // Skip invalid lines
		}

		frame.FrameIndex = e.frameIndex
		e.frameIndex++
		return frame, nil
	}

	if err := e.scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	return nil, io.EOF
}

// HasNext checks if there are more frames to read
func (e *EchoReplayCodec) HasNext() bool {
	return e.scanner != nil && e.scanner.Err() == nil
}

func (e *EchoReplayCodec) writeReplayLine(dst *bytes.Buffer, ts time.Time, session, bones []byte) int {
	// Format is "2006/01/02 15:04:05.000\t<json session data>\t<json bones data>\r\n"
	timestamp := ts.Format(EchoReplayTimeFormat)

	dataSize := len(timestamp) + 1 + len(session) + 1 + 1 + len(bones) + 2 // timestamp + tab + session + tab + bones + \r\n

	dst.Grow(dataSize)
	dst.WriteString(timestamp)
	dst.WriteByte('\t') // Tab separator
	dst.Write(session)
	dst.WriteByte('\t') // Tab separator
	dst.WriteByte(' ')  // space
	dst.Write(bones)
	dst.WriteString("\r\n") // Carriage return + newline

	return dataSize
}

// parseFrameLine parses a single line into a frame
func (e *EchoReplayCodec) parseFrameLine(line []byte) (*rtapi.LobbySessionStateFrame, error) {
	parts := bytes.Split(line, []byte("\t"))
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid line format")
	}

	// Parse timestamp
	timestampStr := string(parts[0])
	timestamp, err := time.Parse(EchoReplayTimeFormat, timestampStr)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp format: %s", timestampStr)
	}

	// Parse session data
	sessionResponse := &apigame.SessionResponse{}
	if err := e.unmarshaler.Unmarshal(parts[1], sessionResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	// Parse player bones data
	bonesResponse := &apigame.PlayerBonesResponse{}
	if err := e.unmarshaler.Unmarshal(parts[2], bonesResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal player bones data: %w", err)
	}

	// Create frame
	frame := &rtapi.LobbySessionStateFrame{
		Timestamp: timestamppb.New(timestamp),
		Session:   sessionResponse,
	}

	// Parse user bones if present (parts[2])
	if len(parts) > 2 && len(parts[2]) > 0 {
		userBones := &apigame.PlayerBonesResponse{}
		if err := e.unmarshaler.Unmarshal(parts[2], userBones); err == nil {
			frame.PlayerBones = userBones
		}
	}

	return frame, nil
}

// ReadTo reads frames into the provided slice and returns the number of frames read.
// This avoids allocations by reusing the caller's slice.
// Returns the number of frames read and any error encountered.
// If the slice is filled before EOF, it returns the count with no error.
func (e *EchoReplayCodec) ReadTo(frames []*rtapi.LobbySessionStateFrame) (int, error) {
	if e.scanner == nil {
		return 0, fmt.Errorf("codec not configured for reading or already closed")
	}

	count := 0
	for count < len(frames) {
		frame, err := e.ReadFrame()
		if err != nil {
			if err == io.EOF {
				return count, io.EOF
			}
			return count, err
		}
		frames[count] = frame
		count++
	}

	return count, nil
}

// ReadFrames reads all frames from the .echoreplay file
func (e *EchoReplayCodec) ReadFrames() ([]*rtapi.LobbySessionStateFrame, error) {
	var frames []*rtapi.LobbySessionStateFrame

	for {
		frame, err := e.ReadFrame()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		frames = append(frames, frame)
	}

	return frames, nil
}

// Close closes the codec and underlying files
func (e *EchoReplayCodec) Close() error {
	var err error

	if e.replayFile != nil {
		if closeErr := e.replayFile.Close(); closeErr != nil {
			err = closeErr
		}
		e.replayFile = nil
		e.scanner = nil
	}

	if e.zipWriter != nil {
		if finErr := e.Finalize(); finErr != nil && err == nil {
			err = finErr
		}
		if closeErr := e.zipWriter.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}

	if e.zipReader != nil {
		if closeErr := e.zipReader.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}

	if e.file != nil {
		if closeErr := e.file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}

	return err
}

// ReadFrameTo reads the next frame into the provided frame object to avoid allocations.
// Returns true if a frame was read, false if EOF or error.
// The frame parameter must be non-nil.
func (e *EchoReplayCodec) ReadFrameTo(frame *rtapi.LobbySessionStateFrame) (bool, error) {
	if e.scanner == nil {
		return false, fmt.Errorf("codec not configured for reading or already closed")
	}

	for e.scanner.Scan() {
		line := e.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		if err := e.parseFrameLineTo(line, frame); err != nil {
			continue // Skip invalid lines
		}

		frame.FrameIndex = e.frameIndex
		e.frameIndex++
		return true, nil
	}

	if err := e.scanner.Err(); err != nil {
		return false, fmt.Errorf("scanner error: %w", err)
	}

	return false, io.EOF
}

// parseFrameLineTo parses a single line into the provided frame object
func (e *EchoReplayCodec) parseFrameLineTo(line []byte, frame *rtapi.LobbySessionStateFrame) error {
	// Find tab positions to avoid bytes.Split allocation
	firstTab := bytes.IndexByte(line, '\t')
	if firstTab == -1 {
		return fmt.Errorf("invalid line format")
	}

	secondTab := bytes.IndexByte(line[firstTab+1:], '\t')
	if secondTab == -1 {
		return fmt.Errorf("invalid line format")
	}
	secondTab += firstTab + 1

	// Parse timestamp - reuse buffer to avoid allocation
	if firstTab > len(e.timestampBuf) {
		return fmt.Errorf("timestamp too long")
	}
	copy(e.timestampBuf[:], line[:firstTab])
	timestamp, err := time.ParseInLocation(EchoReplayTimeFormat, string(e.timestampBuf[:firstTab]), time.Local)
	if err != nil {
		return fmt.Errorf("invalid timestamp format")
	}

	// Parse session data (between first and second tab)
	sessionBytes := line[firstTab+1 : secondTab]
	if frame.Session == nil {
		frame.Session = &apigame.SessionResponse{}
	}
	if err := json.Unmarshal(sessionBytes, frame.Session); err != nil {
		return fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	// Parse player bones data if present (after second tab)
	bonesBytes := line[secondTab+1:]
	// Skip leading space if present
	if len(bonesBytes) > 0 && bonesBytes[0] == ' ' {
		bonesBytes = bonesBytes[1:]
	}

	if len(bonesBytes) > 0 {
		if frame.PlayerBones == nil {
			frame.PlayerBones = &apigame.PlayerBonesResponse{}
		}
		if err := json.Unmarshal(bonesBytes, frame.PlayerBones); err != nil {
			return fmt.Errorf("failed to unmarshal player bones data: %w", err)
		}
	} else {
		frame.PlayerBones = nil
	}

	// Set timestamp - reuse existing object to avoid allocation
	if frame.Timestamp == nil {
		frame.Timestamp = timestamppb.New(timestamp)
	} else {
		frame.Timestamp.Seconds = timestamp.Unix()
		frame.Timestamp.Nanos = int32(timestamp.Nanosecond())
	}

	return nil
}
