package conversion

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

const (
	sampleEchoReplay = "../../testdata/sample.echoreplay"
	goldenTape       = "../../testdata/sample.tape.golden"
)

// TestGoldenConvert converts testdata/sample.echoreplay to tape format and
// compares the output against a committed golden file. On first run (no
// golden file), it creates it.
func TestGoldenConvert(t *testing.T) {
	if _, err := os.Stat(sampleEchoReplay); os.IsNotExist(err) {
		t.Skip("testdata/sample.echoreplay not available")
	}

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "sample.tape")

	result, err := ConvertFile(sampleEchoReplay, outputPath)
	if err != nil {
		t.Fatalf("ConvertFile: %v", err)
	}

	if result.FrameCount == 0 {
		t.Fatal("conversion produced 0 frames")
	}

	outputData, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	outputHash := sha256sum(outputData)

	goldenData, err := os.ReadFile(goldenTape)
	if os.IsNotExist(err) {
		// First run: create the golden file.
		if err := os.MkdirAll(filepath.Dir(goldenTape), 0o755); err != nil {
			t.Fatalf("create testdata dir: %v", err)
		}
		if err := os.WriteFile(goldenTape, outputData, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		t.Logf("created golden file: %s (%d bytes, %d frames, sha256:%s)",
			goldenTape, len(outputData), result.FrameCount, outputHash)
		return
	}
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}

	goldenHash := sha256sum(goldenData)

	if outputHash != goldenHash {
		t.Errorf("output does not match golden file\n  output: sha256:%s (%d bytes)\n  golden: sha256:%s (%d bytes)",
			outputHash, len(outputData), goldenHash, len(goldenData))
	}
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
