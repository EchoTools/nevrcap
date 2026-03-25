# Consuming `.tape` Files in the Browser for WebGL Rendering

This guide covers how to decode nevr-tape v2 `.tape` files in a browser environment and feed the resulting frame data into a WebGL renderer.

## Format Overview

A `.tape` file is a **Zstd-compressed stream of length-delimited Protocol Buffer messages**. Each message is a `telemetry.v2.Envelope` containing one of:

1. **`CaptureHeader`** — session metadata (exactly one, first message)
2. **`Frame`** — per-frame game state (repeated, bulk of the file)
3. **`CaptureFooter`** — frame count, duration, keyframe/event indexes (exactly one, last message)

Wire layout (after Zstd decompression):

```
[varint length][Envelope{CaptureHeader}]
[varint length][Envelope{Frame}]
[varint length][Envelope{Frame}]
...
[varint length][Envelope{CaptureFooter}]
```

Each record is a **protobuf varint** encoding the byte length of the marshaled `Envelope`, followed by that many bytes of serialized protobuf.

Reference implementation (Go): [`pkg/codecs/codec_tape_v2.go`](https://github.com/EchoTools/nevr-capture/blob/main/pkg/codecs/codec_tape_v2.go)

---

## Step 1: Obtain the Protobuf Definitions

The `.tape` format is defined in the [`buf.build/echotools/nevr-api`](https://buf.build/echotools/nevr-api) module on the Buf Schema Registry. The relevant proto files are:

| Proto file | Contains |
|---|---|
| `telemetry/v2/capture.proto` | `Envelope`, `CaptureHeader`, `Frame`, `CaptureFooter`, `KeyframeEntry`, `EventIndexEntry` |
| `telemetry/v2/echo_arena.proto` | `EchoArenaHeader`, `EchoArenaFrame`, `EchoEvent`, game enums |
| `spatial/v1/types.proto` | `Vec3` (float32 xyz), `Quat` (float32 xyzw), `Pose` (position + orientation) |

### Generating TypeScript/JavaScript types

Use the Buf CLI to generate JS/TS bindings. With [`protobuf-es`](https://github.com/bufbuild/protobuf-es) (recommended for browser use):

```bash
# Install the Buf CLI and protobuf-es plugin
npm install @bufbuild/protobuf @bufbuild/protoc-gen-es

# Generate from the BSR module directly
npx buf generate buf.build/echotools/nevr-api
```

Example `buf.gen.yaml`:

```yaml
version: v2
plugins:
  - local: protoc-gen-es
    out: src/gen
    opt:
      - target=ts
```

This produces TypeScript classes for `Envelope`, `Frame`, `CaptureHeader`, `CaptureFooter`, `Vec3`, `Quat`, `Pose`, and all game-specific types.

Alternatively, pull generated types directly from the BSR's NPM registry:

```bash
npm install @buf/echotools_nevr-api.bufbuild_es
```

---

## Step 2: Decompress Zstd in the Browser

The entire `.tape` file is a single Zstd frame. You need a browser-compatible Zstd decoder.

### Option A: `fzstd` (lightweight, pure JS)

```bash
npm install fzstd
```

```typescript
import { decompress } from 'fzstd';

async function loadTape(url: string): Promise<Uint8Array> {
  const response = await fetch(url);
  const compressed = new Uint8Array(await response.arrayBuffer());
  return decompress(compressed);
}
```

### Option B: `@aspect-build/aspect-zstd` (WASM-based, faster for large files)

```bash
npm install @aspect-build/aspect-zstd
```

### Option C: Streaming decompression

For large captures (10+ minutes at 120 Hz = 72,000+ frames), consider streaming decompression to avoid loading the entire decompressed buffer into memory. The [`DecompressionStream`](https://developer.mozilla.org/en-US/docs/Web/API/DecompressionStream) Web API does not yet support Zstd natively, but you can wrap a WASM decoder in a `TransformStream`.

---

## Step 3: Parse Length-Delimited Protobuf Messages

After decompression you have a byte stream of varint-length-delimited `Envelope` messages. Parse them sequentially:

```typescript
import { Envelope } from './gen/telemetry/v2/capture_pb';
import type { CaptureHeader, Frame, CaptureFooter } from './gen/telemetry/v2/capture_pb';

function readVarint(data: Uint8Array, offset: number): [bigint, number] {
  let result = 0n;
  let shift = 0n;
  let pos = offset;
  while (pos < data.length) {
    const byte = data[pos];
    result |= BigInt(byte & 0x7f) << shift;
    pos++;
    if ((byte & 0x80) === 0) break;
    shift += 7n;
  }
  return [result, pos];
}

interface TapeContents {
  header: CaptureHeader;
  frames: Frame[];
  footer: CaptureFooter;
}

function parseTape(decompressed: Uint8Array): TapeContents {
  let offset = 0;
  const envelopes: Envelope[] = [];

  while (offset < decompressed.length) {
    const [length, newOffset] = readVarint(decompressed, offset);
    const msgBytes = decompressed.slice(newOffset, newOffset + Number(length));
    envelopes.push(Envelope.fromBinary(msgBytes));
    offset = newOffset + Number(length);
  }

  // First envelope is always the header
  const header = envelopes[0].message.value as CaptureHeader;

  // Last envelope is always the footer
  const footer = envelopes[envelopes.length - 1].message.value as CaptureFooter;

  // Everything in between is frames
  const frames = envelopes.slice(1, -1).map(e => e.message.value as Frame);

  return { header, frames, footer };
}
```

> **Performance note:** For large files, avoid allocating an array of all frames upfront. Instead, parse on demand or into a ring buffer matching your playback window.

---

## Step 4: Extract Renderable Data from Frames

Each `Frame` contains a `timestamp_offset_ms` (milliseconds since `CaptureHeader.created_at`) and a game-specific payload. For Echo Arena, the payload is an `EchoArenaFrame`:

```typescript
import type { EchoArenaFrame } from './gen/telemetry/v2/echo_arena_pb';

function extractRenderState(frame: Frame) {
  const ts = frame.timestampOffsetMs; // milliseconds into the capture
  const arena = frame.payload.value as EchoArenaFrame;

  // Disc state — position and velocity as spatial.v1.Vec3
  const disc = arena.disc;
  const discPos = disc?.position; // { x: number, y: number, z: number }
  const discVel = disc?.velocity; // { x: number, y: number, z: number }

  // Player states
  for (const player of arena.players) {
    const pos = player.pose?.position;   // Vec3
    const rot = player.pose?.orientation; // Quat { x, y, z, w }
    const vel = player.velocity;          // Vec3
    // player.team, player.name, player.slot, etc.
  }

  // Skeletal data (zero-copy packed bytes)
  for (const bones of arena.playerBones) {
    // bones.transforms: Uint8Array — packed float32 xyz per bone
    // bones.orientations: Uint8Array — packed float32 xyzw quaternion per bone
    // Layout defined by CaptureHeader → EchoArenaHeader → skeleton
    //   Default: 22 bones, 12 bytes/transform (3×f32), 16 bytes/orientation (4×f32)
  }

  return { ts, discPos, discVel, players: arena.players, bones: arena.playerBones };
}
```

### Spatial types → WebGL

The `spatial.v1` types use **float32**, which maps directly to WebGL's `Float32Array` without conversion:

| Proto type | Layout | WebGL usage |
|---|---|---|
| `Vec3` | `{x, y, z}` float32 | `gl.uniform3f` or buffer attribute (3 floats) |
| `Quat` | `{x, y, z, w}` float32 | Quaternion → mat4 for `gl.uniformMatrix4fv` |
| `Pose` | Vec3 + Quat | Model matrix via TRS decomposition |
| `PlayerBones.transforms` | Packed `[x,y,z] × bone_count` bytes | Upload directly to `Float32Array` buffer |
| `PlayerBones.orientations` | Packed `[x,y,z,w] × bone_count` bytes | Upload directly to `Float32Array` buffer |

Bones data is already packed as raw bytes — wrap it in a `Float32Array` view for zero-copy GPU upload:

```typescript
function bonesAsFloat32(raw: Uint8Array): Float32Array {
  return new Float32Array(raw.buffer, raw.byteOffset, raw.byteLength / 4);
}

// Upload to GPU
const boneTransforms = bonesAsFloat32(bones.transforms);
gl.bufferData(gl.ARRAY_BUFFER, boneTransforms, gl.DYNAMIC_DRAW);
```

---

## Step 5: Playback Loop

Drive frame delivery from the footer metadata and `timestamp_offset_ms` fields:

```typescript
class TapePlayer {
  private frames: Frame[];
  private startTime: number = 0;
  private currentIndex: number = 0;
  private durationMs: number;

  constructor(tape: TapeContents) {
    this.frames = tape.frames;
    this.durationMs = tape.footer.durationMs;
  }

  start() {
    this.startTime = performance.now();
    this.currentIndex = 0;
    this.tick();
  }

  private tick = () => {
    const elapsed = performance.now() - this.startTime;

    // Advance to the correct frame based on elapsed time
    while (
      this.currentIndex < this.frames.length - 1 &&
      this.frames[this.currentIndex + 1].timestampOffsetMs <= elapsed
    ) {
      this.currentIndex++;
    }

    const frame = this.frames[this.currentIndex];
    this.render(frame);

    if (elapsed < this.durationMs) {
      requestAnimationFrame(this.tick);
    }
  };

  private render(frame: Frame) {
    // Feed frame data to your WebGL renderer
  }
}
```

---

## Step 6: Using the Footer for Seeking

The `CaptureFooter` contains a **keyframe index** (every 100 frames by default) and an **event index**. These enable efficient seeking without scanning the full stream:

```typescript
function seekToTime(tape: TapeContents, targetMs: number): number {
  // Binary search the keyframe index for the nearest entry before targetMs
  const keyframes = tape.footer.keyframeIndex;
  let lo = 0, hi = keyframes.length - 1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    const frameIdx = keyframes[mid].frameIndex;
    if (tape.frames[frameIdx].timestampOffsetMs <= targetMs) {
      lo = mid;
    } else {
      hi = mid - 1;
    }
  }

  // Linear scan from keyframe to exact target
  let idx = keyframes[lo].frameIndex;
  while (idx < tape.frames.length - 1 && tape.frames[idx + 1].timestampOffsetMs <= targetMs) {
    idx++;
  }
  return idx;
}

// Jump to all goals
function findGoalFrames(tape: TapeContents): number[] {
  const entry = tape.footer.eventIndex.find(
    e => e.eventType === EventType.GOAL_SCORED
  );
  return entry?.frameIndices ?? [];
}
```

> **Note:** The `byte_offset` fields in `KeyframeEntry` refer to offsets within the **decompressed** stream. These are useful if you implement streaming/partial decompression rather than decompressing the entire file upfront.

---

## Coordinate System

Echo Arena uses a **right-handed coordinate system**:
- **+X** = right
- **+Y** = up
- **+Z** = toward blue goal (forward from spectator perspective)

Map this to your WebGL coordinate conventions. If your renderer uses a left-handed system, negate the Z axis.

---

## Reference Links

| Resource | URL |
|---|---|
| nevr-tape Go library | [github.com/EchoTools/nevr-capture](https://github.com/EchoTools/nevr-capture) |
| Protobuf definitions (BSR) | [buf.build/echotools/nevr-api](https://buf.build/echotools/nevr-api) |
| BSR generated Go types | [buf.build/gen/go/echotools/nevr-api](https://buf.build/gen/go/echotools/nevr-api/protocolbuffers/go/) |
| TapeV2 codec source | [codec_tape_v2.go](https://github.com/EchoTools/nevr-capture/blob/main/pkg/codecs/codec_tape_v2.go) |
| spatial.v1 types | [buf.build/echotools/nevr-api → spatial/v1](https://buf.build/echotools/nevr-api/docs/main:spatial.v1) |
| protobuf-es (JS/TS runtime) | [github.com/bufbuild/protobuf-es](https://github.com/bufbuild/protobuf-es) |
| fzstd (browser Zstd) | [npmjs.com/package/fzstd](https://www.npmjs.com/package/fzstd) |

---

## Minimal End-to-End Example

```typescript
import { decompress } from 'fzstd';
import { Envelope } from '@buf/echotools_nevr-api.bufbuild_es/telemetry/v2/capture_pb';

async function main(tapeUrl: string, gl: WebGL2RenderingContext) {
  // 1. Fetch and decompress
  const resp = await fetch(tapeUrl);
  const raw = decompress(new Uint8Array(await resp.arrayBuffer()));

  // 2. Parse all envelopes
  const { header, frames, footer } = parseTape(raw);

  console.log(`Loaded ${footer.frameCount} frames, ${footer.durationMs}ms`);

  // 3. Start playback
  const player = new TapePlayer({ header, frames, footer });
  player.start();
}
```
