package codecs

import (
	"archive/zip"
	"bufio"
	"bytes"
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

// EchoReplay handles .echoreplay file format (zip format)
type EchoReplay struct {
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
	// Scratch buffer for marshaling
	scratchBuf []byte
}

// EchoReplayFrame represents a frame in the .echoreplay format
type EchoReplayFrame struct {
	Timestamp   string                       `json:"timestamp"`
	Session     *apigame.SessionResponse     `json:"session"`
	PlayerBones *apigame.PlayerBonesResponse `json:"user_bones,omitempty"`
}

// NewEchoReplayWriter creates a new EchoReplay codec for writing
func NewEchoReplayWriter(filename string) (*EchoReplay, error) {
	file, err := os.Create(filename)
	if err != nil {
		return nil, err
	}

	zipWriter := zip.NewWriter(file)

	return &EchoReplay{
		filename:    filename,
		file:        file,
		zipWriter:   zipWriter,
		frameBuffer: &bytes.Buffer{},
	}, nil
}

// NewEchoReplayReader creates a new EchoReplay codec for reading
func NewEchoReplayReader(filename string) (*EchoReplay, error) {
	zipReader, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}

	codec := &EchoReplay{
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
func (e *EchoReplay) initScanner() error {
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
func (e *EchoReplay) WriteFrame(frame *rtapi.LobbySessionStateFrame) error {
	if e.zipWriter == nil {
		return ErrCodecNotConfiguredForWriting
	}

	// Use the optimized writeReplayFrame method
	e.WriteReplayFrame(e.frameBuffer, frame)
	return nil
}

// WriteFrameBatch writes multiple frames efficiently in a single operation
func (e *EchoReplay) WriteFrameBatch(frames []*rtapi.LobbySessionStateFrame) error {
	if e.zipWriter == nil {
		return ErrCodecNotConfiguredForWriting
	}

	for _, frame := range frames {
		e.WriteReplayFrame(e.frameBuffer, frame)
	}
	return nil
}

// FlushBuffer forces a flush of the internal buffer (useful for periodic flushing)
func (e *EchoReplay) FlushBuffer() error {
	if e.zipWriter == nil {
		return ErrCodecNotConfiguredForWriting
	}

	// For this implementation, we buffer everything until Finalize()
	// This could be enhanced to support intermediate flushing if needed
	return nil
}

// GetBufferSize returns the current size of the internal buffer
func (e *EchoReplay) GetBufferSize() int {
	if e.frameBuffer == nil {
		return 0
	}
	return e.frameBuffer.Len()
}

// WriteReplayFrame writes a frame using optimized buffer operations (same approach as writer_replay_file.go)
func (e *EchoReplay) WriteReplayFrame(dst *bytes.Buffer, frame *rtapi.LobbySessionStateFrame) int {
	startLen := dst.Len()

	// 1. Timestamp
	fastFormatTimestamp(e.timestampBuf[:], frame.Timestamp.AsTime())
	dst.Write(e.timestampBuf[:23])

	// 2. Separator
	dst.WriteByte('\t')

	// 3. Session
	var err error
	e.scratchBuf = e.scratchBuf[:0]
	e.scratchBuf, err = echoReplayerMarshaler.MarshalAppend(e.scratchBuf, frame.GetSession())
	if err != nil {
		return 0
	}
	dst.Write(e.scratchBuf)

	// 4. Separator and Space
	dst.WriteByte('\t')
	dst.WriteByte(' ')

	// 5. Player Bones
	e.scratchBuf = e.scratchBuf[:0]
	e.scratchBuf, err = echoReplayerMarshaler.MarshalAppend(e.scratchBuf, frame.GetPlayerBones())
	if err != nil {
		return 0
	}
	dst.Write(e.scratchBuf)

	// 6. Newline
	dst.WriteString("\r\n")

	return dst.Len() - startLen
}

// Finalize writes the buffered data to the zip file and closes it
func (e *EchoReplay) Finalize() error {
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
func (e *EchoReplay) ReadFrame() (*rtapi.LobbySessionStateFrame, error) {
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
func (e *EchoReplay) HasNext() bool {
	return e.scanner != nil && e.scanner.Err() == nil
}

// parseFrameLine parses a single line into a frame
func (e *EchoReplay) parseFrameLine(line []byte) (*rtapi.LobbySessionStateFrame, error) {
	parts := bytes.Split(line, []byte("\t"))
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid line format")
	}

	// Parse timestamp
	timestamp, err := fastParseTimestamp(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp format: %s", string(parts[0]))
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
func (e *EchoReplay) ReadTo(frames []*rtapi.LobbySessionStateFrame) (int, error) {
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
func (e *EchoReplay) ReadFrames() ([]*rtapi.LobbySessionStateFrame, error) {
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
func (e *EchoReplay) Close() error {
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
func (e *EchoReplay) ReadFrameTo(frame *rtapi.LobbySessionStateFrame) (bool, error) {
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
func (e *EchoReplay) parseFrameLineTo(line []byte, frame *rtapi.LobbySessionStateFrame) error {
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

	// Parse timestamp
	tsBytes := line[:firstTab]
	timestamp, err := fastParseTimestamp(tsBytes)
	if err != nil {
		return fmt.Errorf("invalid timestamp format")
	}

	// Parse session data (between first and second tab)
	sessionBytes := line[firstTab+1 : secondTab]
	if frame.Session == nil {
		frame.Session = &apigame.SessionResponse{}
	}
	if err := e.unmarshaler.Unmarshal(sessionBytes, frame.Session); err != nil {
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
		if err := e.unmarshaler.Unmarshal(bonesBytes, frame.PlayerBones); err != nil {
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

func fastParseTimestamp(buf []byte) (time.Time, error) {
	if len(buf) != 23 {
		return time.Time{}, &time.ParseError{Layout: EchoReplayTimeFormat, Value: string(buf), Message: "invalid length"}
	}

	// 2006/01/02 15:04:05.000
	// 01234567890123456789012

	year := int(buf[0]-'0')*1000 + int(buf[1]-'0')*100 + int(buf[2]-'0')*10 + int(buf[3]-'0')
	month := time.Month(int(buf[5]-'0')*10 + int(buf[6]-'0'))
	day := int(buf[8]-'0')*10 + int(buf[9]-'0')
	hour := int(buf[11]-'0')*10 + int(buf[12]-'0')
	min := int(buf[14]-'0')*10 + int(buf[15]-'0')
	sec := int(buf[17]-'0')*10 + int(buf[18]-'0')
	ms := int(buf[20]-'0')*100 + int(buf[21]-'0')*10 + int(buf[22]-'0')

	return time.Date(year, month, day, hour, min, sec, ms*1000000, time.Local), nil
}

func fastFormatTimestamp(dst []byte, t time.Time) {
	// 2006/01/02 15:04:05.000
	year, month, day := t.Date()
	hour, min, sec := t.Clock()
	ms := t.Nanosecond() / 1000000

	// Year
	dst[0] = byte(year/1000) + '0'
	dst[1] = byte((year/100)%10) + '0'
	dst[2] = byte((year/10)%10) + '0'
	dst[3] = byte(year%10) + '0'
	dst[4] = '/'

	// Month
	dst[5] = byte(month/10) + '0'
	dst[6] = byte(month%10) + '0'
	dst[7] = '/'

	// Day
	dst[8] = byte(day/10) + '0'
	dst[9] = byte(day%10) + '0'
	dst[10] = ' '

	// Hour
	dst[11] = byte(hour/10) + '0'
	dst[12] = byte(hour%10) + '0'
	dst[13] = ':'

	// Minute
	dst[14] = byte(min/10) + '0'
	dst[15] = byte(min%10) + '0'
	dst[16] = ':'

	// Second
	dst[17] = byte(sec/10) + '0'
	dst[18] = byte(sec%10) + '0'
	dst[19] = '.'

	// Millisecond
	dst[20] = byte(ms/100) + '0'
	dst[21] = byte((ms/10)%10) + '0'
	dst[22] = byte(ms%10) + '0'
}
