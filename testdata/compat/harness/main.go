// Command harness is compiled against a HISTORICAL pkg/codec to test backward
// compatibility. It is copied into a tree produced by `git archive <baseline>`
// and built there, so the binary it produces is genuinely the old code — not
// the current code in a costume.
//
// It lives under testdata/ because the go tool ignores that directory entirely:
// `go build ./...`, `go vet ./...` and `go test ./...` never see it, so a file
// that must compile against OTHER versions of pkg/codec cannot break the build
// of this one.
//
// It uses only the pkg/codec surface that every supported baseline has:
// NewWriterWithKeyframeInterval, WriteHeader, WriteFrame, Close, NewReader,
// ReadHeader, ReadFrame, ReadFooter. Nothing here may reference an API added
// after the OLDEST baseline, or the oldest baseline stops building and the
// compat test starts failing for a reason that is not a compatibility break.
//
// Protocol, so the caller parses one line rather than scraping:
//
//	harness write <path>   writes fixedCapture() with default settings
//	harness read  <path>   prints "OK frames=N first=N last=N footer_frames=N footer_keyframes=N"
//	                       or "FAIL <stage>: <err>" and exits 1
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/echotools/tape/v4/pkg/codec"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fixedCapture is the deterministic corpus both sides write. It must stay
// byte-for-byte in step with fixedCaptureHeader/fixedCaptureFrames in
// pkg/codec/compat_baseline_test.go: the byte-identity property compares the
// output of this program against the output of that test, so any drift between
// the two shows up as a compatibility failure that is really a test bug.
//
// Deliberately no metadata map: protobuf does not guarantee map field ordering,
// and a single-entry map only happens to be stable. Nothing here should be
// "probably deterministic".
const (
	fixedCaptureID  = "compat-baseline-corpus"
	fixedSessionID  = "COMPAT-001"
	fixedMapName    = "mpl_arena_a"
	fixedEpochUnix  = 1756600000
	fixedFrameCount = 400
	fixedKeyframes  = 50
)

func fixedHeader() *capturepb.CaptureHeader {
	return &capturepb.CaptureHeader{
		CaptureId:     fixedCaptureID,
		CreatedAt:     timestamppb.New(time.Unix(fixedEpochUnix, 0).UTC()),
		FormatVersion: 2,
		GameHeader: &capturepb.CaptureHeader_EchoArena{
			EchoArena: &capturepb.EchoArenaHeader{
				SessionId: fixedSessionID,
				MapName:   fixedMapName,
				MatchType: capturepb.MatchType_MATCH_TYPE_ARENA,
			},
		},
	}
}

func fixedFrames() []*capturepb.Frame {
	frames := make([]*capturepb.Frame, fixedFrameCount)
	for i := range frames {
		idx := uint32(i)
		frames[i] = &capturepb.Frame{
			FrameIndex:        idx,
			TimestampOffsetMs: idx * 33,
			Payload: &capturepb.Frame_EchoArena{
				EchoArena: &capturepb.EchoArenaFrame{
					GameStatus:   capturepb.GameStatus_GAME_STATUS_PLAYING,
					GameClock:    300 - float32(i)*0.033,
					BluePoints:   int32(i / 100),
					OrangePoints: int32(i / 150),
				},
			},
		}
	}
	return frames
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: harness <write|read> <path>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "write":
		write(os.Args[2])
	case "read":
		read(os.Args[2])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func write(path string) {
	w, err := codec.NewWriterWithKeyframeInterval(path, fixedKeyframes)
	if err != nil {
		fail("NewWriterWithKeyframeInterval", err)
	}
	if err := w.WriteHeader(fixedHeader()); err != nil {
		fail("WriteHeader", err)
	}
	for _, f := range fixedFrames() {
		if err := w.WriteFrame(f); err != nil {
			fail("WriteFrame", err)
		}
	}
	if err := w.Close(); err != nil {
		fail("Close", err)
	}
	fmt.Printf("OK wrote=%s frames=%d\n", path, fixedFrameCount)
}

func read(path string) {
	r, err := codec.NewReader(path)
	if err != nil {
		fail("NewReader", err)
	}
	defer r.Close()

	if _, err := r.ReadHeader(); err != nil {
		fail("ReadHeader", err)
	}

	var n, first, last uint32
	for {
		f, err := r.ReadFrame()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			fail("ReadFrame", err)
		}
		if n == 0 {
			first = f.GetFrameIndex()
		}
		last = f.GetFrameIndex()
		n++
	}

	footer, err := r.ReadFooter()
	if err != nil {
		fail("ReadFooter", err)
	}

	fmt.Printf("OK frames=%d first=%d last=%d footer_frames=%d footer_keyframes=%d\n",
		n, first, last, footer.GetFrameCount(), len(footer.GetKeyframeIndex()))
}

func fail(stage string, err error) {
	fmt.Printf("FAIL %s: %v\n", stage, err)
	os.Exit(1)
}
