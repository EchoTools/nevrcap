package codec

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
)

// BUGS.md SEC-001 — decompression bomb.
//
// BAC: a tiny (few-KB) capture file whose decompressed stream contains a huge
// number of minimal valid frames must NOT be readable to exhaustion by
// default-configured readers without bound: the reader returns a clear budget
// error (ErrMaxDecodedBytes / ErrMaxFrameCount) once the configured budget is
// exceeded, instead of decoding gigabytes from kilobytes. The budgets are
// documented, configurable options; a caller may raise or disable them
// explicitly (WithoutLimits) so a legitimate giant capture stays readable.

// writeBombTape writes a v2 tape of n minimal valid frames and returns the
// on-disk (compressed) file size.
func writeBombTape(t *testing.T, path string, n int) int64 {
	t.Helper()
	w, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.WriteHeader(&capturepb.CaptureHeader{}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	for i := range n {
		frame := &capturepb.Frame{
			FrameIndex: uint32(i), //nolint:gosec // loop bound is a small test constant
			Payload: &capturepb.Frame_EchoArena{
				EchoArena: &capturepb.EchoArenaFrame{},
			},
		}
		if err := w.WriteFrame(frame); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	return info.Size()
}

// readAllFrames reads the header then frames until an error, returning the
// frame count and the error. A budget error may surface as early as
// ReadHeader (when the budget is smaller than the stream's zstd window).
func readAllFrames(t *testing.T, r *Reader) (int, error) {
	t.Helper()
	if _, err := r.ReadHeader(); err != nil {
		return 0, err
	}
	count := 0
	for {
		_, err := r.ReadFrame()
		if err != nil {
			return count, err
		}
		count++
	}
}

func TestReader_SEC001_FrameCountBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.tape")
	// Frames now carry a sequential frame_index (R2 writer contract), so a
	// bomb built through the Writer is slightly less compressible than the
	// all-identical-frames version; 50k frames still fit under the tiny-file
	// ceiling while vastly exceeding every budget tested here.
	size := writeBombTape(t, path, 50_000)
	if size > 64<<10 {
		t.Fatalf("bomb tape is %d bytes on disk; expected a tiny (<64 KiB) file", size)
	}

	r, err := NewReader(path, WithMaxFrameCount(1000))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	count, err := readAllFrames(t, r)
	if !errors.Is(err, ErrMaxFrameCount) {
		t.Fatalf("want ErrMaxFrameCount, got %v after %d frames", err, count)
	}
	if count != 1000 {
		t.Fatalf("read %d frames before budget error; want exactly the budget (1000)", count)
	}
}

func TestReader_SEC001_DecodedBytesBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.tape")
	size := writeBombTape(t, path, 50_000)

	r, err := NewReader(path, WithMaxDecodedBytes(64<<10))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	count, err := readAllFrames(t, r)
	if !errors.Is(err, ErrMaxDecodedBytes) {
		t.Fatalf("want ErrMaxDecodedBytes, got %v after %d frames", err, count)
	}
	t.Logf("bomb: %d bytes on disk, budget error after %d frames", size, count)
}

func TestReader_SEC001_DefaultLimitsApplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "small.tape")
	writeBombTape(t, path, 5000)

	r, err := NewReader(path)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	if got, want := r.Limits(), DefaultLimits(); got != want {
		t.Fatalf("default reader Limits() = %+v; want %+v", got, want)
	}

	// A capture within the default budgets reads to normal EOF.
	count, err := readAllFrames(t, r)
	if err != io.EOF {
		t.Fatalf("want io.EOF, got %v", err)
	}
	if count != 5000 {
		t.Fatalf("read %d frames; want 5000", count)
	}
}

func TestReader_SEC001_WithoutLimitsOptOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "small.tape")
	writeBombTape(t, path, 5000)

	// Explicit opt-out: a legitimate giant capture must remain readable.
	r, err := NewReader(path, WithoutLimits())
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	if lim := r.Limits(); lim.MaxDecodedBytes > 0 || lim.MaxFrameCount > 0 {
		t.Fatalf("WithoutLimits left limits enabled: %+v", lim)
	}
	count, err := readAllFrames(t, r)
	if err != io.EOF || count != 5000 {
		t.Fatalf("opt-out read: %d frames, err %v; want 5000, io.EOF", count, err)
	}
}

// writeLegacyBomb writes a v1 legacy stream (varint-delimited protos, zstd)
// of n minimal frames.
func writeLegacyBomb(t *testing.T, path string, n int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	enc, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatalf("zstd: %v", err)
	}
	writeDelimited := func(m proto.Message) {
		data, err := proto.Marshal(m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var buf [10]byte
		l := uint64(len(data))
		i := 0
		for l >= 0x80 {
			buf[i] = byte(l) | 0x80
			l >>= 7
			i++
		}
		buf[i] = byte(l)
		i++
		if _, err := enc.Write(buf[:i]); err != nil {
			t.Fatalf("write len: %v", err)
		}
		if _, err := enc.Write(data); err != nil {
			t.Fatalf("write msg: %v", err)
		}
	}
	writeDelimited(&telemetry.TelemetryHeader{})
	frame := &telemetry.LobbySessionStateFrame{}
	for range n {
		writeDelimited(frame)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("close enc: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
}

func TestLegacyReader_SEC001_FrameCountBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.nevrcap")
	writeLegacyBomb(t, path, 100_000)

	r, err := NewLegacyReader(path, WithMaxFrameCount(500))
	if err != nil {
		t.Fatalf("NewLegacyReader: %v", err)
	}
	defer r.Close()

	if _, err := r.ReadHeader(); err != nil {
		t.Fatalf("ReadHeader: %v", err)
	}
	count := 0
	for {
		if _, err = r.ReadFrame(); err != nil {
			break
		}
		count++
	}
	if !errors.Is(err, ErrMaxFrameCount) {
		t.Fatalf("want ErrMaxFrameCount, got %v after %d frames", err, count)
	}
	if count != 500 {
		t.Fatalf("read %d frames before budget error; want 500", count)
	}
}

func TestLegacyReader_SEC001_DecodedBytesBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.nevrcap")
	writeLegacyBomb(t, path, 100_000)

	r, err := NewLegacyReader(path, WithMaxDecodedBytes(16<<10))
	if err != nil {
		t.Fatalf("NewLegacyReader: %v", err)
	}
	defer r.Close()

	// A budget error may surface as early as ReadHeader (budget smaller than
	// the stream's declared decompressed size).
	count := 0
	_, err2 := r.ReadHeader()
	for err2 == nil {
		if _, err2 = r.ReadFrame(); err2 == nil {
			count++
		}
	}
	if !errors.Is(err2, ErrMaxDecodedBytes) {
		t.Fatalf("want ErrMaxDecodedBytes, got %v after %d frames", err2, count)
	}
}

// writeEchoReplayBomb writes a zip with an .echoreplay member of n identical
// minimal valid lines.
func writeEchoReplayBomb(t *testing.T, path string, n int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	zw := zip.NewWriter(f)
	member, err := zw.Create("bomb.echoreplay")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	line := "2024/10/20 19:20:09.000\t{}\n"
	var buf bytes.Buffer
	buf.Grow(len(line) * min(n, 4096))
	written := 0
	for written < n {
		buf.Reset()
		batch := min(4096, n-written)
		buf.WriteString(strings.Repeat(line, batch))
		if _, err := member.Write(buf.Bytes()); err != nil {
			t.Fatalf("zip write: %v", err)
		}
		written += batch
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestEchoReplay_SEC001_FrameCountBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.echoreplay")
	writeEchoReplayBomb(t, path, 50_000)

	r, err := NewEchoReplayReader(path, WithMaxFrameCount(200))
	if err != nil {
		t.Fatalf("NewEchoReplayReader: %v", err)
	}
	defer r.Close()

	frames, err := r.ReadFrames()
	if !errors.Is(err, ErrMaxFrameCount) {
		t.Fatalf("want ErrMaxFrameCount, got %v (frames returned: %d)", err, len(frames))
	}
}

func TestEchoReplay_SEC001_DecodedBytesBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bomb.echoreplay")
	writeEchoReplayBomb(t, path, 50_000)

	r, err := NewEchoReplayReader(path, WithMaxDecodedBytes(8<<10))
	if err != nil {
		t.Fatalf("NewEchoReplayReader: %v", err)
	}
	defer r.Close()

	frames, err := r.ReadFrames()
	if !errors.Is(err, ErrMaxDecodedBytes) {
		t.Fatalf("want ErrMaxDecodedBytes, got %v (frames returned: %d)", err, len(frames))
	}
}
