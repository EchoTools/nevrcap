package codec

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"testing"

	telemetry "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
)

func TestWriteReplayFrame_NilFrame(t *testing.T) {
	e := &EchoReplay{scratchBuf: make([]byte, 0, 64)}
	var buf bytes.Buffer
	n := e.WriteReplayFrame(&buf, nil)
	if n != 0 {
		t.Errorf("WriteReplayFrame(nil frame) = %d, want 0", n)
	}
}

func TestWriteReplayFrame_NilTimestamp(t *testing.T) {
	e := &EchoReplay{scratchBuf: make([]byte, 0, 64)}
	frame := &telemetry.LobbySessionStateFrame{
		FrameIndex: 1,
		// Timestamp intentionally nil
	}
	var buf bytes.Buffer
	n := e.WriteReplayFrame(&buf, frame)
	if n != 0 {
		t.Errorf("WriteReplayFrame(nil timestamp) = %d, want 0", n)
	}
}

func TestHasNext_FalseAfterEOF(t *testing.T) {
	tmpFile := t.TempDir() + "/eof_test.echoreplay"

	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("eof_test")
	if err != nil {
		t.Fatal(err)
	}
	// One valid line
	w.Write([]byte("2023/01/01 12:00:00.000\t{\"session_id\":\"1\"}\n"))
	zw.Close()
	f.Close()

	reader, err := NewEchoReplayReader(tmpFile)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer reader.Close()

	if !reader.HasNext() {
		t.Fatal("HasNext should be true before reading")
	}

	// Read the one frame
	_, err = reader.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}

	// HasNext might still be true here (hasn't hit EOF yet)
	// Read again to trigger EOF
	_, err = reader.ReadFrame()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}

	if reader.HasNext() {
		t.Error("HasNext should be false after EOF")
	}
}

func TestMaxMessageSize_Legacy(t *testing.T) {
	// Write a varint that encodes a length > MaxMessageSize
	// then verify readDelimitedMessage rejects it.
	tmpFile := t.TempDir() + "/huge.nevrcap"

	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatal(err)
	}

	// Write a raw (not zstd-compressed) file won't work with LegacyReader
	// because it expects zstd. Instead, test the readDelimitedMessage
	// function directly with a crafted reader.
	// We'll encode a varint for MaxMessageSize+1 followed by no data.
	var varintBuf [10]byte
	length := uint64(MaxMessageSize + 1)
	i := 0
	for length >= 0x80 {
		varintBuf[i] = byte(length) | 0x80
		length >>= 7
		i++
	}
	varintBuf[i] = byte(length)
	i++

	f.Close()

	// Create a LegacyReader with a custom reader to test readDelimitedMessage.
	lr := &LegacyReader{
		reader: bytes.NewReader(varintBuf[:i]),
	}

	_, err = lr.readDelimitedMessage()
	if err == nil {
		t.Fatal("expected error for oversized message, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("exceeds maximum")) {
		t.Errorf("expected 'exceeds maximum' error, got: %v", err)
	}
}
