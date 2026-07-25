package conversion

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	enginev1 "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/engine/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// keyset_guard.go — the key-set completeness guard for the echoreplay fidelity
// audit.
//
// WHY IT EXISTS: the round-trip audit reads a recording, writes it back, reads
// it again, and compares the two parsed frame sets. That comparison cannot see
// anything the READER dropped, because a dropped key is missing from both
// sides. The echoreplay reader is constructed with
// protojson.UnmarshalOptions{DiscardUnknown: true} (pkg/codec/echoreplay.go),
// so every JSON key the proto has no field for is discarded at read time and
// the audit reports LOSSLESS. Verified: a real recording rebuilt with
// "unknown_future_field" injected into all 197 session objects audited
// LOSSLESS, frames=197, session-mismatch=0, whole-frame-mismatch=0.
//
// The audit is the floor for deciding whether irreplaceable recordings may be
// deleted, so it has to compare against the ORIGINAL FILE'S BYTES at least
// once. This guard does exactly that and nothing more: it extracts the JSON
// keys actually present in a recording's session objects (at every nesting
// level) and checks each one against the generated proto descriptors. A key
// with no proto field means the file carries content the codec silently
// discards, which fails the audit for that file.
//
// The accepted-name set is derived from the descriptors at runtime, in both
// the `name=` and `json=` forms protojson accepts — never a hand-maintained
// list, because a hand list rots and invisible drift is the exact bug this
// guards.
//
// SCOPE: this is audit-side detection only. It deliberately does not change
// the codec. Making the reader strict (erroring on unknown keys) is a product
// decision with blast radius across every reader of the format.

// keyFinding is one JSON key present in a recording that the proto cannot
// represent, with the number of objects it appeared in.
type keyFinding struct {
	// Path is the dotted descriptor path to the offending key, e.g.
	// "SessionResponse.teams[].players[].unknown_future_field".
	Path  string
	Count int
}

// keyScanResult is the guard's verdict for one recording.
type keyScanResult struct {
	FramesTotal   int // frame lines in the file
	FramesScanned int // frame lines whose session JSON was actually inspected
	Stride        int // 1 == every frame; n>1 == every nth frame was inspected
	ParseErrors   int // session payloads encoding/json could not parse at all
	Findings      []keyFinding
}

// Sampled reports whether the scan looked at fewer than all frames.
func (r keyScanResult) Sampled() bool { return r.FramesScanned < r.FramesTotal }

// Summary renders the verdict in the audit's log style. It always states the
// coverage, because a guard that quietly inspects a fraction of a file and
// prints OK is the same blindness the guard exists to remove.
func (r keyScanResult) Summary() string {
	var b strings.Builder
	if r.Sampled() {
		fmt.Fprintf(&b, "keyscan=%d/%d frames (SAMPLED, stride %d, first+last included)",
			r.FramesScanned, r.FramesTotal, r.Stride)
	} else {
		fmt.Fprintf(&b, "keyscan=%d/%d frames (all)", r.FramesScanned, r.FramesTotal)
	}
	if r.ParseErrors > 0 {
		fmt.Fprintf(&b, " json-unparseable=%d", r.ParseErrors)
	}
	if len(r.Findings) == 0 {
		b.WriteString(" unknown-keys=0")
		return b.String()
	}
	fmt.Fprintf(&b, " unknown-keys=%d [", len(r.Findings))
	for i, f := range r.Findings {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s x%d", f.Path, f.Count)
	}
	b.WriteString("]")
	return b.String()
}

// defaultKeyScanBudget bounds how many frames the guard JSON-parses per file.
//
// Real recordings reach 100k+ frames and hundreds of megabytes, and the guard
// runs alongside a full read-write-read round trip. Parsing every session
// object of every file is disproportionate, so past this budget the guard
// samples at a fixed stride across the WHOLE file (first and last frame always
// included) and says so in its output. The tradeoff is stated rather than
// hidden: on a sampled file, a key that appears in fewer than 1 in `Stride`
// frames can be missed. Pass budget<=0 (TAPE_AUDIT_KEYSCAN=0) to parse every
// frame.
const defaultKeyScanBudget = 2000

// auditSessionKeys scans a .echoreplay file's raw session JSON and reports
// every key that the SessionResponse proto has no field for.
//
// budget bounds the number of frames parsed; budget<=0 parses all of them.
func auditSessionKeys(path string, budget int) (keyScanResult, error) {
	var res keyScanResult

	total, err := countFrameLines(path)
	if err != nil {
		return res, err
	}
	res.FramesTotal = total
	if total == 0 {
		res.Stride = 1
		return res, nil
	}

	res.Stride = 1
	if budget > 0 && total > budget {
		res.Stride = (total + budget - 1) / budget
	}

	counts := map[string]int{}
	md := (&enginev1.SessionResponse{}).ProtoReflect().Descriptor()

	err = eachFrameLine(path, func(i int, line []byte) error {
		if i%res.Stride != 0 && i != total-1 {
			return nil
		}
		parts := bytes.Split(line, []byte("\t"))
		if len(parts) < 2 {
			return nil
		}
		res.FramesScanned++
		var obj map[string]any
		if err := json.Unmarshal(parts[1], &obj); err != nil {
			res.ParseErrors++
			return nil
		}
		walkUnknownKeys(md, obj, string(md.Name()), counts)
		return nil
	})
	if err != nil {
		return res, err
	}

	paths := make([]string, 0, len(counts))
	for p := range counts {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		res.Findings = append(res.Findings, keyFinding{Path: p, Count: counts[p]})
	}
	return res, nil
}

// walkUnknownKeys descends a decoded session object alongside its message
// descriptor, counting every key with no corresponding proto field. Recursion
// covers all nesting that carries data (session -> teams -> players -> ...),
// not just the top level, because a dropped per-player key is exactly as
// invisible as a dropped top-level one.
func walkUnknownKeys(md protoreflect.MessageDescriptor, obj map[string]any, path string, counts map[string]int) {
	// google.protobuf.Struct/Value/Any and friends legitimately carry
	// arbitrary keys; their JSON shape is not driven by field descriptors.
	if strings.HasPrefix(string(md.FullName()), "google.protobuf.") {
		return
	}
	fields := md.Fields()
	for k, v := range obj {
		fd := fields.ByJSONName(k)
		if fd == nil {
			fd = fields.ByName(protoreflect.Name(k))
		}
		if fd == nil {
			counts[path+"."+k]++
			continue
		}
		walkUnknownValue(fd, v, path+"."+k, counts)
	}
}

func walkUnknownValue(fd protoreflect.FieldDescriptor, v any, path string, counts map[string]int) {
	switch {
	case fd.IsMap():
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		vd := fd.MapValue()
		for _, mv := range m {
			walkUnknownSingular(vd, mv, path+"{}", counts)
		}
	case fd.IsList():
		arr, ok := v.([]any)
		if !ok {
			return
		}
		for _, e := range arr {
			walkUnknownSingular(fd, e, path+"[]", counts)
		}
	default:
		walkUnknownSingular(fd, v, path, counts)
	}
}

func walkUnknownSingular(fd protoreflect.FieldDescriptor, v any, path string, counts map[string]int) {
	if fd.Kind() != protoreflect.MessageKind && fd.Kind() != protoreflect.GroupKind {
		return
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return
	}
	walkUnknownKeys(fd.Message(), obj, path, counts)
}

// --- raw file access ---------------------------------------------------------
//
// The guard reads the file's own bytes on purpose: routing through the codec
// would reintroduce the blindness it is checking for.

// replayMember resolves the frame-bearing zip member using the same preference
// order as the codec's reader (pkg/codec/echoreplay.go initScanner): the member
// named like the archive without its extension, else the first .echoreplay
// member.
func replayMember(zr *zip.Reader, name string) (*zip.File, error) {
	base := filepath.Base(name)
	if ext := filepath.Ext(base); ext != "" {
		base = base[:len(base)-len(ext)]
	}
	for _, f := range zr.File {
		if f.Name == base {
			return f, nil
		}
	}
	for _, f := range zr.File {
		if filepath.Ext(f.Name) == ".echoreplay" {
			return f, nil
		}
	}
	return nil, fmt.Errorf("no `.echoreplay` file found in zip %s", name)
}

// eachFrameLine calls fn for every non-empty line of the recording, with the
// line's frame index.
func eachFrameLine(path string, fn func(i int, line []byte) error) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer func() { _ = zr.Close() }()

	member, err := replayMember(&zr.Reader, path)
	if err != nil {
		return err
	}
	rc, err := member.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	sc := bufio.NewScanner(rc)
	// Match the codec's 10 MB line budget; frames can be very large.
	sc.Buffer(make([]byte, 64*1024), 10*1024*1024)
	i := 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := fn(i, line); err != nil {
			return err
		}
		i++
	}
	return sc.Err()
}

// countFrameLines counts frame lines without parsing any JSON, so the sampling
// stride can be spread evenly across the whole file.
func countFrameLines(path string) (int, error) {
	n := 0
	err := eachFrameLine(path, func(int, []byte) error {
		n++
		return nil
	})
	return n, err
}
