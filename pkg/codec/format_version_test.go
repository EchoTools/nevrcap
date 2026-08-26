package codec

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	capturepb "buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/telemetry/v2"
	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/proto"
)

// writeRawTape writes an envelope stream directly (bypassing the Writer) so we
// can craft headers with arbitrary format versions.
func writeRawTape(t *testing.T, path string, envelopes ...*capturepb.Envelope) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}

	for _, env := range envelopes {
		data, err := proto.Marshal(env)
		if err != nil {
			t.Fatal(err)
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
			t.Fatal(err)
		}
		if _, err := enc.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestReader_ReadHeader_RejectsUnknownFormatVersion proves GH #30: the Reader
// must validate format_version in the CaptureHeader and reject versions it does
// not understand. Currently the Reader accepts any version silently.
func TestReader_ReadHeader_RejectsUnknownFormatVersion(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "v99.tape")
	writeRawTape(t, path, &capturepb.Envelope{
		Message: &capturepb.Envelope_Header{
			Header: &capturepb.CaptureHeader{
				FormatVersion: 99,
			},
		},
	})

	r, err := NewReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close() //nolint:errcheck // test cleanup

	_, err = r.ReadHeader()
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("ReadHeader with format_version=99: want ErrUnsupportedVersion, got %v", err)
	}
}

// TestReader_ReadHeader_AcceptsVersion2 proves the companion case: format_version=2
// (and format_version=0 for pre-2.1 captures) is accepted.
func TestReader_ReadHeader_AcceptsVersion2(t *testing.T) {
	t.Parallel()

	for _, ver := range []uint32{0, 2} {
		path := filepath.Join(t.TempDir(), "v2.tape")
		writeRawTape(t, path, &capturepb.Envelope{
			Message: &capturepb.Envelope_Header{
				Header: &capturepb.CaptureHeader{
					FormatVersion: ver,
				},
			},
		})

		r, err := NewReader(path)
		if err != nil {
			t.Fatal(err)
		}

		hdr, err := r.ReadHeader()
		r.Close() //nolint:errcheck // test cleanup
		if err != nil {
			t.Fatalf("ReadHeader with format_version=%d: %v", ver, err)
		}
		if hdr.GetFormatVersion() != ver {
			t.Errorf("format_version = %d, want %d", hdr.GetFormatVersion(), ver)
		}
	}
}
