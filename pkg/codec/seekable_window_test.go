package codec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
)

// bombWindow is the window a hostile file declares. It is 256 MiB: larger than
// anything this library's writer can produce (see maxWriterWindow) and well
// inside what zstd permits, so the frame is legal and only the reader's own
// ceiling can refuse it.
const bombWindow = 256 << 20

// TestReadBlockRefusesAWindowThisLibraryCannotHaveWritten is the residual half
// of F4, and it exists because the recorded witness cannot witness it.
//
// TestReadBlockRefusesAFrameWhoseSizeContradictsTheSeekTable (renamed on
// 2026-09-07 from TestReadBlockBoundsDecoderMemory, for this reason) builds its
// bomb with enc.EncodeAll, which knows the payload length and therefore writes a
// Frame_Content_Size. The frame-header cross-check fires on that FCS and returns
// first, so the decoder cap is never the thing under test and the decoded-size
// equality never runs. A STREAMING encoder writes no FCS — the size is unknown
// when the header is emitted — so this test reaches the code the other one
// shadows.
//
// What it asserts: the memory cap must not be computed from a number the
// hostile file chose. hdr.WindowSize is read out of the attacker's own frame
// header, so a cap of max(declared, hdr.WindowSize, 1) lets the attacker name
// their own ceiling, and klauspost allocates roughly twice the window before
// anything else can object (framedec.go:255-266).
func TestReadBlockRefusesAWindowThisLibraryCannotHaveWritten(t *testing.T) {
	path, _ := corruptionCorpus(t, "corpus.tape")
	clean, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	index, err := OpenBlockIndex(path)
	if err != nil {
		t.Fatalf("OpenBlockIndex: %v", err)
	}
	blocks := index.Blocks()
	blockOff, blockLen, err := index.BlockRange(1)
	if err != nil {
		t.Fatalf("BlockRange: %v", err)
	}

	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf,
		zstd.WithEncoderLevel(zstd.SpeedFastest),
		zstd.WithWindowSize(bombWindow),
	)
	if err != nil {
		t.Fatalf("zstd.NewWriter: %v", err)
	}
	if _, err := enc.Write(make([]byte, 8<<20)); err != nil {
		t.Fatalf("encoder write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("encoder close: %v", err)
	}
	bomb := buf.Bytes()

	// INSTRUMENT CHECK, and it is the whole reason this test is a separate file.
	// If the bomb carries an FCS then check 3a answers it and this test silently
	// becomes a duplicate of the size-contradiction test above — passing for a
	// reason that has nothing to do with what it is named for.
	var probe zstd.Header
	if err := probe.Decode(bomb); err != nil {
		t.Fatalf("decode bomb header: %v", err)
	}
	if probe.HasFCS {
		t.Fatalf("bomb carries a Frame_Content_Size (%d); the frame-header "+
			"cross-check would answer it and this test would not reach the "+
			"decoder cap it exists to test", probe.FrameContentSize)
	}
	if probe.WindowSize != bombWindow {
		t.Fatalf("bomb declares window %d, want %d; the attacker-chosen window "+
			"is the input to this test", probe.WindowSize, bombWindow)
	}
	t.Logf("bomb: %d compressed bytes, HasFCS=%v, WindowSize=%d",
		len(bomb), probe.HasFCS, probe.WindowSize)

	// Swap block 1 for the bomb, tell the seek table only how many bytes it
	// occupies, and leave Decompressed_Size at the honest, small original.
	damaged := make([]byte, 0, len(clean)+len(bomb))
	damaged = append(damaged, clean[:blockOff]...)
	damaged = append(damaged, bomb...)
	damaged = append(damaged, clean[blockOff+blockLen:]...)
	cOff, dOff := seekTableEntryOffsets(t, damaged, 1, blocks)
	binary.LittleEndian.PutUint32(damaged[cOff:], uint32(len(bomb))) //nolint:gosec // bounded by the corpus
	declared := binary.LittleEndian.Uint32(damaged[dOff:])

	target := filepath.Join(t.TempDir(), "window-bomb.tape")
	if err := os.WriteFile(target, damaged, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	const budget = 1 << 20
	bad, err := OpenBlockIndex(target, WithMaxDecodedBytes(budget))
	if err != nil {
		t.Fatalf("OpenBlockIndex on the crafted file: %v", err)
	}

	var out []byte
	var readErr error
	allocated := allocatedDuring(func() {
		out, readErr = bad.ReadBlock(1, nil)
	})

	if readErr == nil {
		t.Errorf("ReadBlock returned %d bytes and NO error for a block the seek "+
			"table says yields %d", len(out), declared)
	}
	// The SENTINEL is asserted, not just the failure. Without this the explicit
	// window check could be deleted and the test would still pass on the
	// decoder option alone — an untested check, which is the defect this whole
	// piece of work is about.
	if !errors.Is(readErr, ErrWindowTooLarge) {
		t.Errorf("ReadBlock returned %v; want ErrWindowTooLarge. A frame this "+
			"library could not have written must be refused as policy, not "+
			"reported as damage.", readErr)
	}
	// THE THRESHOLD IS DERIVED, NOT PICKED. Two found numbers: the budget the
	// caller declared on this very call, and maxWriterWindow, which is the
	// largest window this library's own encoder can emit. A refusal may cost
	// the caller what they asked for plus, at worst, one legitimate window. It
	// may not cost what the FILE asked for.
	const ceiling = budget + maxWriterWindow
	if allocated > ceiling {
		t.Errorf("ReadBlock allocated %d bytes (ceiling %d = caller budget %d + "+
			"maxWriterWindow %d) refusing a %d-byte block whose frame declares "+
			"a %d-byte window. The cap must come from this library's own "+
			"writer, not from the file's header.",
			allocated, ceiling, budget, maxWriterWindow, len(bomb), bombWindow)
	}
	t.Logf("window bomb: err=%v, %d bytes allocated, file is %d bytes on disk",
		readErr, allocated, len(damaged))
}

// TestReadBlockBoundsAWindowAgainstOurCeilingNotTheTableS is what makes
// WithDecoderMaxWindow load-bearing rather than decorative.
//
// Two facts have to line up for this to matter. First, zstd.Header.Decode parses
// ONE frame header — the first — while DecodeAll decodes every frame concatenated
// in its input, so check 3c cannot see a window hidden behind an innocent leading
// frame. Second, klauspost clamps the window ceiling down to the decoded ceiling
// (zstd/framedec.go:51-53), which means that WITHOUT an explicit window option the
// window a block may use rises with whatever the seek table declares.
//
// So a table declaring a large-but-legal decompressed size buys the attacker a
// large window for a frame the first-header check never inspects. The explicit
// WithDecoderMaxWindow(maxWriterWindow) is what keeps the window ceiling pinned to
// our writer's number no matter how big the declared size is. Delete that one
// option and this test goes red; the other window test stays green, which is the
// point of having both.
func TestReadBlockBoundsAWindowAgainstOurCeilingNotTheTableS(t *testing.T) {
	// 32 MiB: above maxWriterWindow, and below the 64 MiB the table will declare
	// so that the clamp alone would permit it.
	const hiddenWindow = 32 << 20
	const declaredSize = 64 << 20

	path, _ := corruptionCorpus(t, "corpus.tape")
	clean, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	index, err := OpenBlockIndex(path)
	if err != nil {
		t.Fatalf("OpenBlockIndex: %v", err)
	}
	blocks := index.Blocks()
	blockOff, blockLen, err := index.BlockRange(1)
	if err != nil {
		t.Fatalf("BlockRange: %v", err)
	}

	// Frame one: honest, tiny, a window this library could have written.
	small, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		t.Fatalf("small encoder: %v", err)
	}
	head := small.EncodeAll([]byte("tape"), nil)
	if err := small.Close(); err != nil {
		t.Fatalf("small close: %v", err)
	}

	// Frame two: a window above our ceiling, hidden behind it.
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf,
		zstd.WithEncoderLevel(zstd.SpeedFastest),
		zstd.WithWindowSize(hiddenWindow),
	)
	if err != nil {
		t.Fatalf("bomb encoder: %v", err)
	}
	if _, err := enc.Write(make([]byte, hiddenWindow)); err != nil {
		t.Fatalf("bomb write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("bomb close: %v", err)
	}
	payload := append(append([]byte(nil), head...), buf.Bytes()...)

	// INSTRUMENT CHECKS. If the leading frame were above the ceiling, check 3c
	// would answer this and the test would witness the wrong thing.
	var probe zstd.Header
	if err := probe.Decode(payload); err != nil {
		t.Fatalf("decode leading header: %v", err)
	}
	if probe.WindowSize > maxWriterWindow {
		t.Fatalf("the LEADING frame declares window %d, above the %d ceiling; "+
			"check 3c would catch it and this test would not reach the decoder "+
			"ceiling it exists to witness", probe.WindowSize, uint64(maxWriterWindow))
	}
	if declaredSize <= maxWriterWindow {
		t.Fatalf("declaredSize %d must exceed maxWriterWindow %d, or the clamp "+
			"and the explicit option agree and this test cannot distinguish them",
			uint64(declaredSize), uint64(maxWriterWindow))
	}
	t.Logf("payload: %d bytes; leading frame window %d (innocent); hidden frame "+
		"window %d; table will declare %d decompressed",
		len(payload), probe.WindowSize, uint64(hiddenWindow), uint64(declaredSize))

	damaged := make([]byte, 0, len(clean)+len(payload))
	damaged = append(damaged, clean[:blockOff]...)
	damaged = append(damaged, payload...)
	damaged = append(damaged, clean[blockOff+blockLen:]...)
	cOff, dOff := seekTableEntryOffsets(t, damaged, 1, blocks)
	binary.LittleEndian.PutUint32(damaged[cOff:], uint32(len(payload))) //nolint:gosec // bounded by the corpus
	binary.LittleEndian.PutUint32(damaged[dOff:], declaredSize)

	target := filepath.Join(t.TempDir(), "ceiling-bomb.tape")
	if err := os.WriteFile(target, damaged, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// The caller's budget is generous, so check 2 passes on the declared size and
	// the window is the only thing left standing between the file and the memory.
	bad, err := OpenBlockIndex(target, WithMaxDecodedBytes(128<<20))
	if err != nil {
		t.Fatalf("OpenBlockIndex on the crafted file: %v", err)
	}

	var out []byte
	var readErr error
	allocated := allocatedDuring(func() {
		out, readErr = bad.ReadBlock(1, nil)
	})

	if readErr == nil {
		t.Errorf("ReadBlock returned %d bytes and NO error for a block hiding a "+
			"%d-byte window behind an innocent first frame", len(out), uint64(hiddenWindow))
	}
	// Refused on the window, so nothing is decoded: the cost is our ceiling at
	// worst, never the table's declared size.
	if allocated > maxWriterWindow {
		t.Errorf("ReadBlock allocated %d bytes (ceiling %d) on a block whose "+
			"SECOND frame declares a %d-byte window and whose table declares %d "+
			"decompressed. The window ceiling must come from this library's "+
			"writer, not rise with the size the table claims.",
			allocated, uint64(maxWriterWindow), uint64(hiddenWindow), uint64(declaredSize))
	}
	t.Logf("ceiling bomb: err=%v, %d bytes allocated, file is %d bytes on disk",
		readErr, allocated, len(damaged))
}

// spliceWindowBomb builds a per-block capture whose block 1 is a single
// streaming-encoded frame with the given window, and whose seek table declares
// declaredSize for it. Streaming means no Frame_Content_Size, so the frame-header
// cross-check cannot answer it and the window check is what fires.
func spliceWindowBomb(t *testing.T, window int, payload int, declaredSize uint32) string {
	t.Helper()
	path, _ := corruptionCorpus(t, "corpus.tape")
	clean, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	index, err := OpenBlockIndex(path)
	if err != nil {
		t.Fatalf("OpenBlockIndex: %v", err)
	}
	blocks := index.Blocks()
	blockOff, blockLen, err := index.BlockRange(1)
	if err != nil {
		t.Fatalf("BlockRange: %v", err)
	}

	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf,
		zstd.WithEncoderLevel(zstd.SpeedFastest),
		zstd.WithWindowSize(window),
	)
	if err != nil {
		t.Fatalf("zstd.NewWriter: %v", err)
	}
	if _, err := enc.Write(make([]byte, payload)); err != nil {
		t.Fatalf("encoder write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("encoder close: %v", err)
	}
	bomb := buf.Bytes()

	var probe zstd.Header
	if err := probe.Decode(bomb); err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if probe.HasFCS {
		t.Fatalf("bomb carries an FCS; the frame-header check would answer it")
	}
	if probe.WindowSize != uint64(window) {
		t.Fatalf("bomb declares window %d, want %d", probe.WindowSize, window)
	}

	damaged := make([]byte, 0, len(clean)+len(bomb))
	damaged = append(damaged, clean[:blockOff]...)
	damaged = append(damaged, bomb...)
	damaged = append(damaged, clean[blockOff+blockLen:]...)
	cOff, dOff := seekTableEntryOffsets(t, damaged, 1, blocks)
	binary.LittleEndian.PutUint32(damaged[cOff:], uint32(len(bomb))) //nolint:gosec // bounded by the corpus
	binary.LittleEndian.PutUint32(damaged[dOff:], declaredSize)

	target := filepath.Join(t.TempDir(), "shaped.tape")
	if err := os.WriteFile(target, damaged, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return target
}

// TestOversizeWindowRefusalSaysWhichShapeItIs is the self-diagnosing half of the
// window bound.
//
// maxWriterWindow comes from a producer survey, and a survey has an expiry as
// well as a perimeter — so the error this reader cannot avoid emitting is
// sometimes aimed at a LEGITIMATE capture from a producer nobody surveyed. The
// refusal stands either way; what it must not do is look identical in both cases,
// because then the operator's only recourse is a support ticket.
//
// Both messages are asserted, and asserted to DIFFER. A discriminator whose two
// branches read the same is not a discriminator.
func TestOversizeWindowRefusalSaysWhichShapeItIs(t *testing.T) {
	// Bomb shape: a 256 MiB window over a block the table says yields 182 bytes.
	// The window references data that is not there.
	bombPath := spliceWindowBomb(t, bombWindow, 8<<20, 182)
	bombIdx, err := OpenBlockIndex(bombPath, WithMaxDecodedBytes(256<<20))
	if err != nil {
		t.Fatalf("OpenBlockIndex: %v", err)
	}
	_, bombErr := bombIdx.ReadBlock(1, nil)
	if !errors.Is(bombErr, ErrWindowTooLarge) {
		t.Fatalf("bomb: want ErrWindowTooLarge, got %v", bombErr)
	}

	// Legitimate shape: a 32 MiB window over a block the table says yields
	// 64 MiB. Above our ceiling, but proportionate to its own content — exactly
	// what a producer using a larger window would produce.
	legitPath := spliceWindowBomb(t, 32<<20, 32<<20, 64<<20)
	legitIdx, err := OpenBlockIndex(legitPath, WithMaxDecodedBytes(256<<20))
	if err != nil {
		t.Fatalf("OpenBlockIndex: %v", err)
	}
	_, legitErr := legitIdx.ReadBlock(1, nil)
	if !errors.Is(legitErr, ErrWindowTooLarge) {
		t.Fatalf("legit-shaped: want ErrWindowTooLarge (it is still refused), got %v", legitErr)
	}

	bombMsg, legitMsg := bombErr.Error(), legitErr.Error()

	if !strings.Contains(bombMsg, "shape of a decompression bomb") {
		t.Errorf("bomb refusal does not name the bomb shape:\n%s", bombMsg)
	}
	if !strings.Contains(legitMsg, "shape of a legitimate capture") {
		t.Errorf("proportionate-window refusal does not point at the producer survey:\n%s", legitMsg)
	}
	if bombMsg == legitMsg {
		t.Errorf("both refusals produced the same text; the discriminator does not discriminate:\n%s", bombMsg)
	}
	t.Logf("BOMB  : %s", bombMsg)
	t.Logf("LEGIT : %s", legitMsg)
}

// TestReadBlockErrorNamesTheDictionaryTheFrameWants covers the diagnostic added
// alongside the window bound: a decompression failure on a dictionary-bearing
// block says WHICH dictionary the frame asked for.
//
// It is a diagnostic, not a check. It helps a MISSING dictionary (the id says
// what to find) and a wrong dictionary with a DIFFERENT id (the ids visibly
// disagree). It cannot help F3 — a wrong dictionary carrying the SAME id — and
// the assertion here is deliberately about the id being NAMED, not about the
// error distinguishing anything it cannot.
func TestReadBlockErrorNamesTheDictionaryTheFrameWants(t *testing.T) {
	const dictID = MinPrivateDictionaryID

	path, _ := corruptionCorpus(t, "corpus.tape")
	clean, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	index, err := OpenBlockIndex(path)
	if err != nil {
		t.Fatalf("OpenBlockIndex: %v", err)
	}
	blocks := index.Blocks()
	blockOff, blockLen, err := index.BlockRange(1)
	if err != nil {
		t.Fatalf("BlockRange: %v", err)
	}

	// A block compressed WITH a dictionary, which we then decline to supply.
	frames := blockTestFrames(600)
	samples := make([][]byte, len(frames))
	for i, f := range frames {
		b, marshalErr := proto.Marshal(f)
		if marshalErr != nil {
			t.Fatalf("Marshal sample %d: %v", i, marshalErr)
		}
		samples[i] = b
	}
	// t.Fatalf, not t.Skipf: a test that skips is not evidence (AGENTS.md §6),
	// and this one is the only check that the dictionary id reaches the error.
	dict, err := TrainDictionary(dictID, samples)
	if err != nil {
		t.Fatalf("TrainDictionary on %d samples: %v", len(samples), err)
	}
	enc, err := zstd.NewWriter(nil,
		zstd.WithEncoderLevel(zstd.SpeedFastest),
		zstd.WithEncoderDict(dict),
	)
	if err != nil {
		t.Fatalf("zstd.NewWriter: %v", err)
	}
	payload := []byte("a block that needs a dictionary to read")
	block := enc.EncodeAll(payload, nil)
	if err := enc.Close(); err != nil {
		t.Fatalf("encoder close: %v", err)
	}

	// INSTRUMENT CHECK: the frame must actually carry the id, or this test is
	// asserting the absence of a thing rather than its presence.
	var probe zstd.Header
	if err := probe.Decode(block); err != nil {
		t.Fatalf("decode block header: %v", err)
	}
	if probe.DictionaryID != dictID {
		t.Fatalf("frame declares dictionary id %d, want %d; the encoder did not "+
			"record the id and this test cannot check what it is named for",
			probe.DictionaryID, dictID)
	}

	damaged := make([]byte, 0, len(clean)+len(block))
	damaged = append(damaged, clean[:blockOff]...)
	damaged = append(damaged, block...)
	damaged = append(damaged, clean[blockOff+blockLen:]...)
	cOff, dOff := seekTableEntryOffsets(t, damaged, 1, blocks)
	binary.LittleEndian.PutUint32(damaged[cOff:], uint32(len(block)))   //nolint:gosec // test-sized
	binary.LittleEndian.PutUint32(damaged[dOff:], uint32(len(payload))) //nolint:gosec // test-sized

	target := filepath.Join(t.TempDir(), "needs-dict.tape")
	if err := os.WriteFile(target, damaged, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	bad, err := OpenBlockIndex(target)
	if err != nil {
		t.Fatalf("OpenBlockIndex on the crafted file: %v", err)
	}
	// Read it with NO dictionary — the missing-dictionary case.
	_, readErr := bad.ReadBlock(1, nil)
	if readErr == nil {
		t.Fatal("ReadBlock succeeded without the dictionary the block was written with")
	}
	if !strings.Contains(readErr.Error(), fmt.Sprintf("dictionary id %d", dictID)) {
		t.Errorf("the error does not name the dictionary the frame asked for, so an "+
			"operator holding several cannot act on it.\ngot: %v", readErr)
	}
	t.Logf("missing dictionary: %v", readErr)
}
