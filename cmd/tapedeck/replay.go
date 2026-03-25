package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/echotools/tape/pkg/codec"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
)

func newReplayCommand() *cobra.Command {
	var (
		addr string
		rate float64
	)

	cmd := &cobra.Command{
		Use:   "replay <file.tape>",
		Short: "Replay a tape file over HTTP",
		Long: `Serve a tape file as an HTTP replay, emulating the Echo VR API endpoints.
Clients can poll GET /session and GET /player_bones to receive frame data
at the original capture rate.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReplay(cmd, args[0], addr, rate)
		},
	}

	cmd.Flags().StringVar(&addr, "addr", ":6721", "listen address")
	cmd.Flags().Float64Var(&rate, "rate", 1.0, "playback rate multiplier")

	return cmd
}

type replayState struct {
	header       *capturepb.CaptureHeader
	currentFrame *capturepb.Frame
	mu           sync.RWMutex
	done         bool
}

func (s *replayState) getFrame() *capturepb.Frame {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentFrame
}

func (s *replayState) setFrame(f *capturepb.Frame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentFrame = f
}

func (s *replayState) isDone() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.done
}

func (s *replayState) setDone() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done = true
}

func runReplay(cmd *cobra.Command, filePath, addr string, rate float64) error {
	reader, err := codec.NewReader(filePath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}

	header, err := reader.ReadHeader()
	if err != nil {
		reader.Close() //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("read header: %w", err)
	}

	state := &replayState{header: header}

	marshaler := protojson.MarshalOptions{
		EmitUnpopulated: true,
		UseProtoNames:   false,
	}

	// Start frame playback goroutine.
	go func() {
		defer reader.Close() //nolint:errcheck // best-effort cleanup
		defer state.setDone()

		var prevOffsetMs uint32
		for {
			frame, readErr := reader.ReadFrame()
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					return
				}
				printf(cmd.ErrOrStderr(), "read error: %v\n", readErr)
				return
			}

			// Wait for the inter-frame delay.
			offsetMs := frame.GetTimestampOffsetMs()
			if offsetMs > prevOffsetMs {
				delay := time.Duration(float64(offsetMs-prevOffsetMs)/rate) * time.Millisecond
				time.Sleep(delay)
			}
			prevOffsetMs = offsetMs

			state.setFrame(frame)
		}
	}()

	// HTTP handlers.
	mux := http.NewServeMux()

	mux.HandleFunc("GET /session", func(w http.ResponseWriter, r *http.Request) {
		frame := state.getFrame()
		if frame == nil {
			http.Error(w, "no frame available", http.StatusServiceUnavailable)
			return
		}

		ea := frame.GetEchoArena()
		if ea == nil {
			http.Error(w, "no echo arena data", http.StatusServiceUnavailable)
			return
		}

		data, marshalErr := marshaler.Marshal(ea)
		if marshalErr != nil {
			http.Error(w, marshalErr.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(data) //nolint:errcheck // HTTP response write errors are non-recoverable
	})

	mux.HandleFunc("GET /player_bones", func(w http.ResponseWriter, r *http.Request) {
		frame := state.getFrame()
		if frame == nil {
			http.Error(w, "no frame available", http.StatusServiceUnavailable)
			return
		}

		ea := frame.GetEchoArena()
		if ea == nil || len(ea.GetPlayerBones()) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{}")) //nolint:errcheck // HTTP response write errors are non-recoverable
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[")) //nolint:errcheck // HTTP response write errors are non-recoverable
		for i, bones := range ea.GetPlayerBones() {
			if i > 0 {
				w.Write([]byte(",")) //nolint:errcheck // HTTP response write errors are non-recoverable
			}
			data, marshalErr := marshaler.Marshal(bones)
			if marshalErr != nil {
				continue
			}
			w.Write(data) //nolint:errcheck // HTTP response write errors are non-recoverable
		}
		w.Write([]byte("]")) //nolint:errcheck // HTTP response write errors are non-recoverable
	})

	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		frame := state.getFrame()
		done := state.isDone()
		var frameIdx uint32
		if frame != nil {
			frameIdx = frame.GetFrameIndex()
		}
		printf(w, `{"frame_index":%d,"done":%t}`, frameIdx, done)
	})

	printf(cmd.OutOrStdout(), "replaying %s on %s\n", filePath, addr)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return server.ListenAndServe()
}
