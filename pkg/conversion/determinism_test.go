package conversion

import (
	"os"
	"path/filepath"
	"testing"
)

// TestConvertDeterministic converts the same .echoreplay twice and asserts the
// two tape outputs are byte-identical. Conversion must be deterministic: an
// anticheat tape whose bytes change run-to-run cannot be a trustworthy record.
//
// Before the per-frame event-ordering fix this failed, because the player
// sensors emitted PlayerJoined/PlayerLeft/PlayerSwitchedTeam events in Go map
// iteration order (randomized), so frames with multiple simultaneous player
// events serialized in a different order each run.
func TestConvertDeterministic(t *testing.T) {
	if _, err := os.Stat(sampleEchoReplay); os.IsNotExist(err) {
		t.Skip("testdata/sample.echoreplay not available")
	}

	convert := func() ([]byte, uint32) {
		t.Helper()
		out := filepath.Join(t.TempDir(), "sample.tape")
		result, err := ConvertFile(sampleEchoReplay, out)
		if err != nil {
			t.Fatalf("ConvertFile: %v", err)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read output: %v", err)
		}
		return data, result.FrameCount
	}

	first, firstFrames := convert()
	second, secondFrames := convert()

	if firstFrames != secondFrames {
		t.Fatalf("frame count differs between runs: %d vs %d", firstFrames, secondFrames)
	}

	firstHash := sha256sum(first)
	secondHash := sha256sum(second)
	if firstHash != secondHash {
		t.Fatalf("conversion is non-deterministic:\n  run 1: sha256:%s (%d bytes)\n  run 2: sha256:%s (%d bytes)",
			firstHash, len(first), secondHash, len(second))
	}
}
