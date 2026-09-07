// Package codec reads and writes the telemetry capture formats.
//
// # Containers
//
// Two containers, neither of which is gzip:
//
//   - .tape — a Zstd stream (github.com/klauspost/compress/zstd) of
//     length-delimited telemetry.v2.Envelope messages: one CaptureHeader, then
//     Frames, then one CaptureFooter. An uncompressed envelope stream is also
//     accepted, so a capture is still readable if the Zstd magic is absent.
//     .nevrcap is the legacy extension for the v1 payload and is read-only.
//   - .echoreplay — the game engine's own recording: an archive/zip container
//     holding line-oriented "timestamp\tsessionJSON[\tbonesJSON]" records.
//
// # Formats
//
//   - TapeV1 — Zstd-compressed telemetry.v1 messages: a TelemetryHeader
//     followed by LobbySessionStateFrame messages. Read-only; the v1 writer was
//     deliberately removed.
//   - TapeV2 — the current format. Game-agnostic Envelope with spatial.v1
//     float32 primitives, and a footer carrying frame count, duration, and
//     keyframe/event indexes.
//   - EchoReplay — the legacy engine format. The writer reproduces the engine's
//     output byte for byte, because third-party parsers depend on those exact
//     bytes; see FixProtojsonUint64Encoding and FixEngineFloatFormatting.
//
// # Integrity
//
// Readers are bounded by default (see Limits) so a small hostile file cannot
// force unbounded allocation — SEC-001 for decompression bombs, SEC-002 for
// allocate-before-verify. Reading a .tape to the end validates THREE trailers,
// and it took until v4.1.0 to reach all three:
//
//   - the footer's frame_count against the frames actually read —
//     ErrFooterMismatch rather than a clean io.EOF, so a capture that lost
//     data is refused instead of read as a short but successful one;
//   - zstd's content checksum, by consuming the end of the compressed frame —
//     ErrCorruptCapture. Before v4.1.0 the reader stopped at the footer
//     envelope and never reached it, so 58% of single-bit flips read back as
//     clean captures with wrong telemetry (F1);
//   - that nothing follows the footer at all — ErrTrailingData.
//
// # Seeking
//
// The Zstd seekable frames this once said would be needed are here, and they are
// the default. NewWriterWithOptions with no options writes the PER-BLOCK layout:
// each block is an independent Zstd frame opening at a keyframe, and a seek table
// in a trailing skippable frame records every block's compressed and decompressed
// size (see seekable.go, which implements facebook/zstd's seekable format
// verbatim). OpenBlockIndex reads that table from EOF without decompressing
// anything, BlockRange turns a block number into a byte range, and ReadBlock and
// Footer decode one block on its own. That is random access.
//
// KeyframeEntry.ByteOffset is what the layout changes. Under the per-block
// default it is an offset into the COMPRESSED file and is therefore servable;
// under WithWholeStreamCompression the capture is one Zstd frame, the offset is a
// position in the DECOMPRESSED stream, and reaching it means decompressing from
// the start. Only the opt-out layout is the scanning case this paragraph used to
// describe for both.
package codec
