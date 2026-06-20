package codec

import (
	"errors"
	"io"
	"os"
	"sort"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
)

const (
	// DefaultKeyframeInterval is how often keyframes are indexed in the footer.
	DefaultKeyframeInterval = 100
)

// Writer writes tape captures in the envelope stream format.
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
type Writer struct {
	file       *os.File
	encoder    *zstd.Encoder
	eventIndex map[capturepb.EventType][]uint32
	keyframes  []*capturepb.KeyframeEntry
	// bytesWritten tracks uncompressed bytes for footer_offset.
	bytesWritten     uint64
	frameCount       uint32
	lastTimestampMs  uint32
	keyframeInterval uint32
}

// NewWriter creates a writer for tape files.
func NewWriter(filename string) (*Writer, error) {
	return NewWriterWithKeyframeInterval(filename, DefaultKeyframeInterval)
}

// NewWriterWithKeyframeInterval creates a writer with a custom keyframe interval.
func NewWriterWithKeyframeInterval(filename string, keyframeInterval uint32) (*Writer, error) {
	file, err := os.Create(filename) //nolint:gosec // filename is caller-provided path
	if err != nil {
		return nil, err
	}

	encoder, err := zstd.NewWriter(file, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}

	return &Writer{
		file:             file,
		encoder:          encoder,
		keyframeInterval: keyframeInterval,
		eventIndex:       make(map[capturepb.EventType][]uint32),
	}, nil
}

// WriteHeader writes the capture header as the first envelope.
func (w *Writer) WriteHeader(header *capturepb.CaptureHeader) error {
	env := &capturepb.Envelope{
		Message: &capturepb.Envelope_Header{Header: header},
	}
	return w.writeEnvelope(env)
}

// WriteFrame writes a frame envelope and updates indexes.
func (w *Writer) WriteFrame(frame *capturepb.Frame) error {
	frameIndex := w.frameCount

	// Track keyframe offset before writing.
	if frameIndex%w.keyframeInterval == 0 {
		w.keyframes = append(w.keyframes, &capturepb.KeyframeEntry{
			FrameIndex: frameIndex,
			ByteOffset: w.bytesWritten,
		})
	}

	// Track events from Echo Arena frames.
	if ea := frame.GetEchoArena(); ea != nil {
		for _, evt := range ea.GetEvents() {
			eventType := classifyEvent(evt)
			if eventType != capturepb.EventType_EVENT_TYPE_UNSPECIFIED {
				w.eventIndex[eventType] = append(w.eventIndex[eventType], frameIndex)
			}
		}
	}

	env := &capturepb.Envelope{
		Message: &capturepb.Envelope_Frame{Frame: frame},
	}
	if err := w.writeEnvelope(env); err != nil {
		return err
	}

	w.frameCount++
	w.lastTimestampMs = frame.GetTimestampOffsetMs()
	return nil
}

// Close writes the footer, closes the zstd encoder, and closes the file.
func (w *Writer) Close() error {
	// Build event index entries in deterministic order.
	types := make([]capturepb.EventType, 0, len(w.eventIndex))
	for t := range w.eventIndex {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })

	var eventEntries []*capturepb.EventIndexEntry
	for _, eventType := range types {
		frameIndices := w.eventIndex[eventType]
		eventEntries = append(eventEntries, &capturepb.EventIndexEntry{
			EventType:    eventType,
			FrameIndices: frameIndices,
		})
	}

	footer := &capturepb.CaptureFooter{
		FrameCount:    w.frameCount,
		DurationMs:    w.lastTimestampMs,
		FooterOffset:  w.bytesWritten,
		KeyframeIndex: w.keyframes,
		EventIndex:    eventEntries,
	}

	env := &capturepb.Envelope{
		Message: &capturepb.Envelope_Footer{Footer: footer},
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
func (w *Writer) writeEnvelope(env *capturepb.Envelope) error {
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
func classifyEvent(evt *capturepb.EchoEvent) capturepb.EventType {
	switch evt.GetEvent().(type) {
	case *capturepb.EchoEvent_RoundStarted:
		return capturepb.EventType_EVENT_TYPE_ROUND_STARTED
	case *capturepb.EchoEvent_RoundPaused:
		return capturepb.EventType_EVENT_TYPE_ROUND_PAUSED
	case *capturepb.EchoEvent_RoundUnpaused:
		return capturepb.EventType_EVENT_TYPE_ROUND_UNPAUSED
	case *capturepb.EchoEvent_RoundEnded:
		return capturepb.EventType_EVENT_TYPE_ROUND_ENDED
	case *capturepb.EchoEvent_MatchEnded:
		return capturepb.EventType_EVENT_TYPE_MATCH_ENDED
	case *capturepb.EchoEvent_ScoreboardUpdated:
		return capturepb.EventType_EVENT_TYPE_SCOREBOARD_UPDATED
	case *capturepb.EchoEvent_PlayerJoined:
		return capturepb.EventType_EVENT_TYPE_PLAYER_JOINED
	case *capturepb.EchoEvent_PlayerLeft:
		return capturepb.EventType_EVENT_TYPE_PLAYER_LEFT
	case *capturepb.EchoEvent_PlayerSwitchedTeam:
		return capturepb.EventType_EVENT_TYPE_PLAYER_SWITCHED_TEAM
	case *capturepb.EchoEvent_EmotePlayed:
		return capturepb.EventType_EVENT_TYPE_EMOTE_PLAYED
	case *capturepb.EchoEvent_DiscPossessionChanged:
		return capturepb.EventType_EVENT_TYPE_DISC_POSSESSION_CHANGED
	case *capturepb.EchoEvent_DiscThrown:
		return capturepb.EventType_EVENT_TYPE_DISC_THROWN
	case *capturepb.EchoEvent_DiscCaught:
		return capturepb.EventType_EVENT_TYPE_DISC_CAUGHT
	case *capturepb.EchoEvent_GoalScored:
		return capturepb.EventType_EVENT_TYPE_GOAL_SCORED
	case *capturepb.EchoEvent_PlayerGoal:
		return capturepb.EventType_EVENT_TYPE_PLAYER_GOAL
	case *capturepb.EchoEvent_PlayerSave:
		return capturepb.EventType_EVENT_TYPE_PLAYER_SAVE
	case *capturepb.EchoEvent_PlayerStun:
		return capturepb.EventType_EVENT_TYPE_PLAYER_STUN
	case *capturepb.EchoEvent_PlayerPass:
		return capturepb.EventType_EVENT_TYPE_PLAYER_PASS
	case *capturepb.EchoEvent_PlayerSteal:
		return capturepb.EventType_EVENT_TYPE_PLAYER_STEAL
	case *capturepb.EchoEvent_PlayerBlock:
		return capturepb.EventType_EVENT_TYPE_PLAYER_BLOCK
	case *capturepb.EchoEvent_PlayerInterception:
		return capturepb.EventType_EVENT_TYPE_PLAYER_INTERCEPTION
	case *capturepb.EchoEvent_PlayerAssist:
		return capturepb.EventType_EVENT_TYPE_PLAYER_ASSIST
	case *capturepb.EchoEvent_PlayerShotTaken:
		return capturepb.EventType_EVENT_TYPE_PLAYER_SHOT_TAKEN
	case *capturepb.EchoEvent_GenericEvent:
		return capturepb.EventType_EVENT_TYPE_GENERIC
	default:
		return capturepb.EventType_EVENT_TYPE_UNSPECIFIED
	}
}

// Reader reads tape captures from the envelope stream format.
type Reader struct {
	file    *os.File
	decoder *zstd.Decoder
	reader  io.Reader

	// pendingFooter holds a footer envelope encountered during ReadFrame.
	pendingFooter *capturepb.CaptureFooter
}

// NewReader opens a tape file for streaming reads.
func NewReader(filename string) (*Reader, error) {
	file, err := os.Open(filename) //nolint:gosec // filename is caller-provided path
	if err != nil {
		return nil, err
	}

	decoder, err := zstd.NewReader(file)
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}

	return &Reader{
		file:    file,
		decoder: decoder,
		reader:  decoder,
	}, nil
}

// ReadHeader reads the first envelope, which must be a CaptureHeader.
func (r *Reader) ReadHeader() (*capturepb.CaptureHeader, error) {
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
func (r *Reader) ReadFrame() (*capturepb.Frame, error) {
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
func (r *Reader) ReadFooter() (*capturepb.CaptureFooter, error) {
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
func (r *Reader) Close() error {
	r.decoder.Close()
	return r.file.Close()
}

// readEnvelope reads a single length-delimited envelope from the stream.
func (r *Reader) readEnvelope() (*capturepb.Envelope, error) {
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

	env := &capturepb.Envelope{}
	if err := proto.Unmarshal(data, env); err != nil {
		return nil, err
	}
	return env, nil
}
