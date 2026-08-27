package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/echotools/tape/v4/pkg/codec"
	"github.com/echotools/tape/v4/pkg/conversion"
)

// GH #45: `replay` advertises that it emulates the Echo VR API, but it used to
// marshal the v2 EchoArenaFrame directly. That JSON shares no field names with
// engine.v1.SessionResponse, so no real client could parse it. These tests pin
// the endpoint to the engine's shape and to the engine's bytes.

func reconstructGoldenFrame(t *testing.T, ordinal int) (session, bones []byte) {
	t.Helper()

	r, err := codec.NewReader(goldenTape)
	if err != nil {
		t.Fatalf("open golden: %v", err)
	}
	defer r.Close() //nolint:errcheck // test cleanup

	rc, err := conversion.NewSessionReconstructor(r)
	if err != nil {
		t.Fatalf("reconstructor: %v", err)
	}
	if rc.FrameCount() <= ordinal {
		t.Skipf("golden has %d frames, need > %d", rc.FrameCount(), ordinal)
	}

	frame := rc.ReconstructFrame(ordinal)
	if frame == nil || frame.GetSession() == nil {
		t.Fatalf("frame %d did not reconstruct a session", ordinal)
	}

	session, err = marshalEngineJSON(frame.GetSession())
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	if pb := frame.GetPlayerBones(); pb != nil {
		bones, err = marshalEngineJSON(pb)
		if err != nil {
			t.Fatalf("marshal bones: %v", err)
		}
	}
	return session, bones
}

func TestReplay_SessionEndpointUsesEngineFieldNames(t *testing.T) {
	session, _ := reconstructGoldenFrame(t, 200)

	// Field names a real client parses.
	for _, want := range []string{
		// Note "sessionid", not "session_id" — the engine's own json_name is
		// one word (engine_http.proto:160). Using the engine's spelling is
		// part of what makes this output parseable by real clients.
		"game_status", "teams", "disc", "sessionid", "map_name", "client_name",
	} {
		if !bytes.Contains(session, []byte(`"`+want+`"`)) {
			t.Errorf("engine field %q missing from /session JSON", want)
		}
	}

	// v2-only shapes that must NOT leak through.
	for _, bad := range []string{
		`"player_bones"`, `"timestamp_offset_ms"`, `"frame_index"`, `"echo_arena"`,
	} {
		if bytes.Contains(session, []byte(bad)) {
			t.Errorf("v2-only field %q leaked into /session JSON", bad)
		}
	}

	// It must be valid JSON.
	var probe map[string]any
	if err := json.Unmarshal(session, &probe); err != nil {
		t.Fatalf("/session JSON does not parse: %v", err)
	}
	if _, ok := probe["teams"]; !ok {
		t.Error(`parsed /session JSON has no "teams" key`)
	}
}

// The stronger contract: the bytes this endpoint serves must be the same bytes
// the .echoreplay writer would produce for the same frame, because both claim
// to reproduce engine output and third-party parsers depend on it.
func TestReplay_SessionBytesMatchEchoReplayWriter(t *testing.T) {
	const ordinal = 200
	session, _ := reconstructGoldenFrame(t, ordinal)

	// Render the same frame through the echoreplay writer and pull out its
	// session field (record layout: timestamp \t sessionJSON [\t bonesJSON]).
	r, err := codec.NewReader(goldenTape)
	if err != nil {
		t.Fatalf("open golden: %v", err)
	}
	defer r.Close() //nolint:errcheck // test cleanup
	rc, err := conversion.NewSessionReconstructor(r)
	if err != nil {
		t.Fatalf("reconstructor: %v", err)
	}
	frame := rc.ReconstructFrame(ordinal)

	w, err := codec.NewEchoReplayWriter(filepath.Join(t.TempDir(), "probe.echoreplay"))
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	var buf bytes.Buffer
	if n := w.WriteReplayFrame(&buf, frame); n == 0 {
		t.Fatal("WriteReplayFrame wrote nothing")
	}

	record := strings.TrimRight(buf.String(), "\r\n")
	parts := strings.Split(record, "\t")
	if len(parts) < 2 {
		t.Fatalf("unexpected echoreplay record shape: %d fields", len(parts))
	}
	writerSession := parts[1]

	if string(session) != writerSession {
		t.Errorf("replay /session bytes differ from the echoreplay writer's.\n"+
			"  replay len=%d\n  writer len=%d", len(session), len(writerSession))
		// Show the first divergence to make the failure actionable.
		a, b := string(session), writerSession
		for i := 0; i < len(a) && i < len(b); i++ {
			if a[i] != b[i] {
				lo := max(0, i-40)
				t.Errorf("  first difference at byte %d:\n   replay: ...%s\n   writer: ...%s",
					i, a[lo:min(len(a), i+40)], b[lo:min(len(b), i+40)])
				break
			}
		}
	}
}
