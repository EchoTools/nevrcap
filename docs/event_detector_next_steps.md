# Event Detector Next Steps

1. Implement the sensor management API and last-frame caching from `docs/event_detector_performance_tasks.md`, then rerun `go test ./...` to lock in the behavioral change.
2. Add the `ProcessFrameBatch` fast path plus the shared-lock ingestion logic, and extend the detector benchmarks to cover the batch API.
3. Introduce `DetectorConfig` + `Configure` plumbing so channel/frame capacities are tunable, followed by regression tests that pin the new defaults.
4. Wire up the synchronous `Detect` path through both the detector and `FrameProcessor`, profiling latency after the change.
5. Expose `DetectorStats` and ensure every benchmark asserts `DroppedFrames == 0` to catch regressions early.
