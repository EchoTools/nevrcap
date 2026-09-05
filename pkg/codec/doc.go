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
// Because the stream is Zstd-compressed, the footer's byte offsets support
// scanning rather than true random access; seeking would require Zstd seekable
// frames.
package codec
