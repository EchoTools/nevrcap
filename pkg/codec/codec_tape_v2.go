package codec

import (
	"errors"
	"io"
	"os"

	telemetryv2 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
)

const (
	// DefaultKeyframeInterval is how often keyframes are indexed in the footer.
	DefaultKeyframeInterval = 100
)

// TapeV2Writer writes telemetry v2 captures in the envelope stream format.
//
// Wire format:
//
//	[Zstd frame]
//	  [varint len][Envelope{CaptureHeader}]   — exactly one
//	  [varint len][Envelope{Frame}]           — repeated
//	  ...
//	  [varint len][Envelope{CaptureFooter}]   — exactly one
//	[Zstd trailer]
//
// The footer records frame count, duration, byte offsets in the uncompressed
// stream, keyframe and event indexes. Because the stream is Zstd-compressed,
// byte-offset seeking requires decompressing from the start; true random
// access would need Zstd seekable frames (future work).
type TapeV2Writer struct {
	file       *os.File
	encoder    *zstd.Encoder
	eventIndex map[telemetryv2.EventType][]uint32
	keyframes  []*telemetryv2.KeyframeEntry
	// bytesWritten tracks uncompressed bytes for footer_offset.
	bytesWritten     uint64
	frameCount       uint32
	lastTimestampMs  uint32
	keyframeInterval uint32
}

// NewTapeV2Writer creates a writer for v2 tape files.
func NewTapeV2Writer(filename string) (*TapeV2Writer, error) {
	return NewTapeV2WriterWithKeyframeInterval(filename, DefaultKeyframeInterval)
}

// NewTapeV2WriterWithKeyframeInterval creates a writer with a custom keyframe interval.
func NewTapeV2WriterWithKeyframeInterval(filename string, keyframeInterval uint32) (*TapeV2Writer, error) {
	file, err := os.Create(filename) //nolint:gosec // filename is caller-provided path
	if err != nil {
		return nil, err
	}

	encoder, err := zstd.NewWriter(file, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}

	return &TapeV2Writer{
		file:             file,
		encoder:          encoder,
		keyframeInterval: keyframeInterval,
		eventIndex:       make(map[telemetryv2.EventType][]uint32),
	}, nil
}

// WriteHeader writes the capture header as the first envelope.
func (w *TapeV2Writer) WriteHeader(header *telemetryv2.CaptureHeader) error {
	env := &telemetryv2.Envelope{
		Message: &telemetryv2.Envelope_Header{Header: header},
	}
	return w.writeEnvelope(env)
}

// WriteFrame writes a frame envelope and updates indexes.
func (w *TapeV2Writer) WriteFrame(frame *telemetryv2.Frame) error {
	frameIndex := w.frameCount

	// Track keyframe offset before writing.
	if frameIndex%w.keyframeInterval == 0 {
		w.keyframes = append(w.keyframes, &telemetryv2.KeyframeEntry{
			FrameIndex: frameIndex,
			ByteOffset: w.bytesWritten,
		})
	}

	// Track events from Echo Arena frames.
	if ea := frame.GetEchoArena(); ea != nil {
		for _, evt := range ea.GetEvents() {
			eventType := classifyEvent(evt)
			if eventType != telemetryv2.EventType_EVENT_TYPE_UNSPECIFIED {
				w.eventIndex[eventType] = append(w.eventIndex[eventType], frameIndex)
			}
		}
	}

	env := &telemetryv2.Envelope{
		Message: &telemetryv2.Envelope_Frame{Frame: frame},
	}
	if err := w.writeEnvelope(env); err != nil {
		return err
	}

	w.frameCount++
	w.lastTimestampMs = frame.GetTimestampOffsetMs()
	return nil
}

// Close writes the footer, closes the zstd encoder, and closes the file.
func (w *TapeV2Writer) Close() error {
	// Build event index entries.
	var eventEntries []*telemetryv2.EventIndexEntry
	for eventType, frameIndices := range w.eventIndex {
		eventEntries = append(eventEntries, &telemetryv2.EventIndexEntry{
			EventType:    eventType,
			FrameIndices: frameIndices,
		})
	}

	footer := &telemetryv2.CaptureFooter{
		FrameCount:    w.frameCount,
		DurationMs:    w.lastTimestampMs,
		FooterOffset:  w.bytesWritten,
		KeyframeIndex: w.keyframes,
		EventIndex:    eventEntries,
	}

	env := &telemetryv2.Envelope{
		Message: &telemetryv2.Envelope_Footer{Footer: footer},
	}

	var firstErr error

	if err := w.writeEnvelope(env); err != nil {
		firstErr = err
	}

	if err := w.encoder.Close(); err != nil && firstErr == nil {
		firstErr = err
	}

	if err := w.file.Close(); err != nil && firstErr == nil {
		firstErr = err
	}

	return firstErr
}

// writeEnvelope marshals and writes a length-delimited envelope.
func (w *TapeV2Writer) writeEnvelope(env *telemetryv2.Envelope) error {
	data, err := proto.Marshal(env)
	if err != nil {
		return err
	}

	// Write varint length prefix.
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

	n, err := w.encoder.Write(buf[:i])
	w.bytesWritten += uint64(max(n, 0)) //nolint:gosec // n is non-negative on success
	if err != nil {
		return err
	}

	n, err = w.encoder.Write(data)
	w.bytesWritten += uint64(max(n, 0)) //nolint:gosec // n is non-negative on success
	return err
}

// classifyEvent returns the EventType for an EchoEvent based on its oneof case.
func classifyEvent(evt *telemetryv2.EchoEvent) telemetryv2.EventType {
	switch evt.GetEvent().(type) {
	case *telemetryv2.EchoEvent_RoundStarted:
		return telemetryv2.EventType_EVENT_TYPE_ROUND_STARTED
	case *telemetryv2.EchoEvent_RoundPaused:
		return telemetryv2.EventType_EVENT_TYPE_ROUND_PAUSED
	case *telemetryv2.EchoEvent_RoundUnpaused:
		return telemetryv2.EventType_EVENT_TYPE_ROUND_UNPAUSED
	case *telemetryv2.EchoEvent_RoundEnded:
		return telemetryv2.EventType_EVENT_TYPE_ROUND_ENDED
	case *telemetryv2.EchoEvent_MatchEnded:
		return telemetryv2.EventType_EVENT_TYPE_MATCH_ENDED
	case *telemetryv2.EchoEvent_ScoreboardUpdated:
		return telemetryv2.EventType_EVENT_TYPE_SCOREBOARD_UPDATED
	case *telemetryv2.EchoEvent_PlayerJoined:
		return telemetryv2.EventType_EVENT_TYPE_PLAYER_JOINED
	case *telemetryv2.EchoEvent_PlayerLeft:
		return telemetryv2.EventType_EVENT_TYPE_PLAYER_LEFT
	case *telemetryv2.EchoEvent_PlayerSwitchedTeam:
		return telemetryv2.EventType_EVENT_TYPE_PLAYER_SWITCHED_TEAM
	case *telemetryv2.EchoEvent_EmotePlayed:
		return telemetryv2.EventType_EVENT_TYPE_EMOTE_PLAYED
	case *telemetryv2.EchoEvent_DiscPossessionChanged:
		return telemetryv2.EventType_EVENT_TYPE_DISC_POSSESSION_CHANGED
	case *telemetryv2.EchoEvent_DiscThrown:
		return telemetryv2.EventType_EVENT_TYPE_DISC_THROWN
	case *telemetryv2.EchoEvent_DiscCaught:
		return telemetryv2.EventType_EVENT_TYPE_DISC_CAUGHT
	case *telemetryv2.EchoEvent_GoalScored:
		return telemetryv2.EventType_EVENT_TYPE_GOAL_SCORED
	case *telemetryv2.EchoEvent_PlayerGoal:
		return telemetryv2.EventType_EVENT_TYPE_PLAYER_GOAL
	case *telemetryv2.EchoEvent_PlayerSave:
		return telemetryv2.EventType_EVENT_TYPE_PLAYER_SAVE
	case *telemetryv2.EchoEvent_PlayerStun:
		return telemetryv2.EventType_EVENT_TYPE_PLAYER_STUN
	case *telemetryv2.EchoEvent_PlayerPass:
		return telemetryv2.EventType_EVENT_TYPE_PLAYER_PASS
	case *telemetryv2.EchoEvent_PlayerSteal:
		return telemetryv2.EventType_EVENT_TYPE_PLAYER_STEAL
	case *telemetryv2.EchoEvent_PlayerBlock:
		return telemetryv2.EventType_EVENT_TYPE_PLAYER_BLOCK
	case *telemetryv2.EchoEvent_PlayerInterception:
		return telemetryv2.EventType_EVENT_TYPE_PLAYER_INTERCEPTION
	case *telemetryv2.EchoEvent_PlayerAssist:
		return telemetryv2.EventType_EVENT_TYPE_PLAYER_ASSIST
	case *telemetryv2.EchoEvent_PlayerShotTaken:
		return telemetryv2.EventType_EVENT_TYPE_PLAYER_SHOT_TAKEN
	case *telemetryv2.EchoEvent_GenericEvent:
		return telemetryv2.EventType_EVENT_TYPE_GENERIC
	default:
		return telemetryv2.EventType_EVENT_TYPE_UNSPECIFIED
	}
}

// TapeV2Reader reads telemetry v2 captures from the envelope stream format.
type TapeV2Reader struct {
	file    *os.File
	decoder *zstd.Decoder
	reader  io.Reader

	// pendingFooter holds a footer envelope encountered during ReadFrame.
	pendingFooter *telemetryv2.CaptureFooter
}

// NewTapeV2Reader opens a v2 tape file for streaming reads.
func NewTapeV2Reader(filename string) (*TapeV2Reader, error) {
	file, err := os.Open(filename) //nolint:gosec // filename is caller-provided path
	if err != nil {
		return nil, err
	}

	decoder, err := zstd.NewReader(file)
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}

	return &TapeV2Reader{
		file:    file,
		decoder: decoder,
		reader:  decoder,
	}, nil
}

// ReadHeader reads the first envelope, which must be a CaptureHeader.
func (r *TapeV2Reader) ReadHeader() (*telemetryv2.CaptureHeader, error) {
	env, err := r.readEnvelope()
	if err != nil {
		return nil, err
	}

	header := env.GetHeader()
	if header == nil {
		return nil, io.ErrUnexpectedEOF
	}
	return header, nil
}

// ReadFrame reads the next frame envelope. Returns io.EOF when the stream
// ends or a non-frame envelope (the footer) is encountered.
func (r *TapeV2Reader) ReadFrame() (*telemetryv2.Frame, error) {
	env, err := r.readEnvelope()
	if err != nil {
		return nil, err
	}

	frame := env.GetFrame()
	if frame != nil {
		return frame, nil
	}

	// Non-frame envelope — save footer if present and signal end of frames.
	if footer := env.GetFooter(); footer != nil {
		r.pendingFooter = footer
	}
	return nil, io.EOF
}

// ReadFooter returns the capture footer. If ReadFrame already encountered it,
// the cached value is returned. Otherwise reads the next envelope from the stream.
func (r *TapeV2Reader) ReadFooter() (*telemetryv2.CaptureFooter, error) {
	if r.pendingFooter != nil {
		return r.pendingFooter, nil
	}

	env, err := r.readEnvelope()
	if err != nil {
		return nil, err
	}

	footer := env.GetFooter()
	if footer == nil {
		return nil, io.ErrUnexpectedEOF
	}
	return footer, nil
}

// Close closes the decoder and underlying file.
func (r *TapeV2Reader) Close() error {
	r.decoder.Close()
	return r.file.Close()
}

// readEnvelope reads a single length-delimited envelope from the stream.
func (r *TapeV2Reader) readEnvelope() (*telemetryv2.Envelope, error) {
	// Read varint length.
	var length uint64
	var shift uint
	var b [1]byte
	for {
		if _, err := r.reader.Read(b[:]); err != nil {
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

	// Read message data.
	data := make([]byte, length)
	if _, err := io.ReadFull(r.reader, data); err != nil {
		return nil, err
	}

	env := &telemetryv2.Envelope{}
	if err := proto.Unmarshal(data, env); err != nil {
		return nil, err
	}
	return env, nil
}
