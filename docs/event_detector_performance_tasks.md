# Event Detector Performance Backlog

- [ ] Add sensor management APIs in `events.go`.
  - Extend the `Detector` interface with `RegisterSensors(s ...EventSensor)` and `ClearSensors()` so callers no longer reach into `EventDetector` internals to configure sensors.
  - Implement both methods on `EventDetector`, storing sensors behind the mutex.
  - While touching the sensor plumbing, cache the latest frame once per detection cycle and feed that pointer to every sensor to eliminate the current extra lock acquisition plus the `nil` frame bug triggered by `getFrame(0)`.

- [ ] Add `ProcessFrameBatch` to the detector interface for bulk ingestion.
  - Update the `Detector` interface with `ProcessFrameBatch(frames ...*telemetry.LobbySessionStateFrame)` and have `EventDetector` drain the slice via a single enqueue loop, bypassing repeated select/default logic.
  - Ensure the batch path reuses a single lock acquisition when copying frames into the ring buffer to minimize contention.
  - Wire the new method through `FrameProcessor` so high-throughput producers can feed multiple frames per tick without per-frame channel overhead.

- [ ] Make detector buffers configurable through a lightweight config struct.
  - Introduce `type DetectorConfig struct { InputChanSize int; EventsChanSize int; FrameBufferCapacity int }` in `events.go` plus an option-aware constructor `NewEventDetectorWithConfig(cfg DetectorConfig) Detector`.
  - Allow runtime reconfiguration via a new `Configure(cfg DetectorConfig)` method on the `Detector` interface so benchmarks can tune capacities without rebuilding.
  - Update the default constructor to delegate to the configurable version and add unit tests covering non-default sizes.

- [ ] Provide a synchronous detection path for latency-sensitive callers.
  - Add `Detect(frame *telemetry.LobbySessionStateFrame) []*telemetry.LobbySessionEvent` to the `Detector` interface; the method should add the frame directly to the ring buffer and immediately return detected events without touching `inputChan`.
  - Implement the method on `EventDetector` by reusing `addFrameToBuffer` plus `detectEvents`, and guard concurrent usage with the existing mutex.
  - Expose the synchronous path through `FrameProcessor` (e.g., `ProcessFrameSync`) so callers that already manage their own goroutines can skip channel scheduling overhead.

- [ ] Surface drop/backpressure metrics via the interface.
  - Define `type DetectorStats struct { DroppedFrames uint64; BufferedFrames int; EventQueueDepth int }` and add `Stats() DetectorStats` to the `Detector` interface.
  - Increment the drop counter in `ProcessFrame` when the input channel is full; expose buffer depth measurements without additional locking by using atomics.
  - Use the stats in benchmarks to assert that optimizations do not silently increase drop counts.
