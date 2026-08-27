package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	telemetryv1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v1"
	"github.com/echotools/tape/v4/pkg/codec"
	"github.com/echotools/tape/v4/pkg/conversion"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
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
at the original capture rate.

Frames are reconstructed into the engine's own response shape and rendered
with the same JSON fixers the .echoreplay writer uses, so the bytes match what
the game engine produced and existing clients can parse them unchanged.`,
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
	// currentFrame is the reconstructed engine-shaped frame, not the v2 wire
	// frame. Clients of this server parse engine.v1 JSON (GH #45).
	currentFrame *telemetryv1.LobbySessionStateFrame
	mu           sync.RWMutex
	done         bool
}

func (s *replayState) getFrame() *telemetryv1.LobbySessionStateFrame {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentFrame
}

func (s *replayState) setFrame(f *telemetryv1.LobbySessionStateFrame) {
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

// engineMarshaler mirrors the echoreplay writer's marshal options so the JSON
// this server emits matches what the game engine produced. Third-party clients
// depend on those exact bytes, which is the whole point of the endpoint.
var engineMarshaler = protojson.MarshalOptions{
	UseProtoNames:   false,
	UseEnumNumbers:  true,
	EmitUnpopulated: true,
}

// marshalEngineJSON renders msg the way the engine would, applying the same
// two fixers the echoreplay writer applies in the same order
// (pkg/codec/echoreplay.go): protojson encodes uint64 as strings and formats
// floats differently from the engine.
func marshalEngineJSON(msg proto.Message) ([]byte, error) {
	data, err := engineMarshaler.Marshal(msg)
	if err != nil {
		return nil, err
	}
	data = codec.FixProtojsonUint64Encoding(data)
	data = codec.FixEngineFloatFormatting(data)
	return data, nil
}

func runReplay(cmd *cobra.Command, filePath, addr string, rate float64) error {
	reader, err := codec.NewReader(filePath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer reader.Close() //nolint:errcheck // best-effort cleanup

	// The reconstructor materializes each frame back into the engine's
	// SessionResponse shape. Serving the v2 frame directly produced JSON with
	// none of the engine's field names, so no real client could parse it
	// (GH #45).
	rc, err := conversion.NewSessionReconstructor(reader)
	if err != nil {
		return fmt.Errorf("reconstruct: %w", err)
	}

	state := &replayState{}

	// Start frame playback goroutine.
	go func() {
		defer state.setDone()

		var prev time.Time
		for i := range rc.FrameCount() {
			frame := rc.ReconstructFrame(i)
			if frame == nil {
				continue
			}

			// Wait for the inter-frame delay, derived from the reconstructed
			// absolute timestamps.
			ts := frame.GetTimestamp().AsTime()
			if !prev.IsZero() && ts.After(prev) {
				time.Sleep(time.Duration(float64(ts.Sub(prev)) / rate))
			}
			prev = ts

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

		session := frame.GetSession()
		if session == nil {
			http.Error(w, "no session data", http.StatusServiceUnavailable)
			return
		}

		data, marshalErr := marshalEngineJSON(session)
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

		bones := frame.GetPlayerBones()
		if bones == nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{}")) //nolint:errcheck // HTTP response write errors are non-recoverable
			return
		}

		data, marshalErr := marshalEngineJSON(bones)
		if marshalErr != nil {
			http.Error(w, marshalErr.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(data) //nolint:errcheck // HTTP response write errors are non-recoverable
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
