# Task 9: Generate Initial Benchmark Baseline

## Context
You are implementing Task 9 of the CI/CD setup plan. This creates the initial performance baseline that will be used to detect benchmark regressions in CI.

## Files to Reference
- **pkg/events/events_bench_test.go** — BenchmarkAsyncDetector_ProcessFrame
- **pkg/codecs/codec_echoreplay_bench_test.go** — BenchmarkReadFrameTo
- **pkg/codecs/simple_benchmark_test.go** — BenchmarkOptimizedWriteFrame
- **pkg/events/event_detection_bench_test.go** — Event detection micro-benchmarks
- **Plan file** — `.sisyphus/plans/ci-cd-setup.md` (Task 9 section, lines 1007-1111)

## What to Create
**Directory**: `.benchmarks/`  
**File**: `.benchmarks/baseline.txt` (Go benchmark text format)

## Requirements

### Benchmark Execution
- **Command**: `go test -bench=. -benchmem -count=5 ./... | tee .benchmarks/baseline.txt`
- **Count**: Run 5 times for statistical significance
- **Flags**:
  - `-bench=.` — Run all benchmarks
  - `-benchmem` — Include memory allocation stats
  - `-count=5` — Run each benchmark 5 times
- **Format**: Standard Go benchmark text format (NOT JSON)

### Expected Benchmarks
The baseline must include:
1. `BenchmarkAsyncDetector_ProcessFrame` — Event detector performance
2. `BenchmarkReadFrameTo` — EchoReplay codec read performance
3. `BenchmarkOptimizedWriteFrame` — Frame write optimization
4. Event detection micro-benchmarks — Various event type detections

### File Format
Standard Go benchmark output:
```
BenchmarkAsyncDetector_ProcessFrame-8    1234567    100.0 ns/op    64 B/op    2 allocs/op
BenchmarkReadFrameTo-8                   2345678    200.0 ns/op   128 B/op    4 allocs/op
...
```

### Git Tracking
- **Track in git**: YES (baseline.txt should be committed)
- **NOT gitignored**: Baseline is version-controlled
- **Why**: Baseline evolves with codebase, must be tracked

## Implementation Steps

1. **Create `.benchmarks/` directory**:
   ```bash
   mkdir -p .benchmarks
   ```

2. **Run all benchmarks**:
   ```bash
   go test -bench=. -benchmem -count=5 ./... | tee .benchmarks/baseline.txt
   ```

3. **Verify format**:
   - Check first line starts with "Benchmark"
   - Check contains ns/op, B/op, allocs/op fields
   - Check file has >10 lines (multiple benchmarks)

4. **Verify all critical benchmarks present**:
   ```bash
   grep "BenchmarkAsyncDetector_ProcessFrame" .benchmarks/baseline.txt
   grep "BenchmarkReadFrameTo" .benchmarks/baseline.txt
   grep "BenchmarkOptimizedWriteFrame" .benchmarks/baseline.txt
   ```

5. **Ensure tracked in git**:
   ```bash
   git add .benchmarks/baseline.txt
   git check-ignore .benchmarks/baseline.txt  # Should exit 1 (not ignored)
   ```

## Verification (Agent-Executed QA)

### Scenario 1: Baseline file is created and valid
```bash
# Step 1: Check file exists
ls .benchmarks/baseline.txt
# Expected: File exists

# Step 2: Check file has content
wc -l .benchmarks/baseline.txt
# Expected: >10 lines (multiple benchmarks)

# Step 3: Check format
head -5 .benchmarks/baseline.txt
# Expected: Lines start with "Benchmark"

# Step 4: Verify standard fields
grep -E "BenchmarkAsyncDetector_ProcessFrame.*ns/op" .benchmarks/baseline.txt
# Expected: Contains ns/op, B/op, allocs/op fields
```

### Scenario 2: All critical benchmarks are in baseline
```bash
# Step 1: Check ProcessFrame benchmark
grep "BenchmarkAsyncDetector_ProcessFrame" .benchmarks/baseline.txt
# Expected: Exit code 0 (found)

# Step 2: Check ReadFrameTo benchmark
grep "BenchmarkReadFrameTo" .benchmarks/baseline.txt
# Expected: Exit code 0 (found)

# Step 3: Check OptimizedWriteFrame benchmark
grep "BenchmarkOptimizedWriteFrame" .benchmarks/baseline.txt
# Expected: Exit code 0 (found)
```

### Scenario 3: Baseline is tracked in git (not ignored)
```bash
# Step 1: Check not in .gitignore
git check-ignore .benchmarks/baseline.txt 2>&1
# Expected: Exit code 1 (NOT ignored - should be tracked)

# Step 2: Verify file is readable
ls -la .benchmarks/baseline.txt
# Expected: File exists and is readable

# Step 3: Add to git (if not already)
git add .benchmarks/baseline.txt
git ls-files .benchmarks/baseline.txt
# Expected: Exit code 0 (file tracked in git)
```

## Important Notes

### Text Format (NOT JSON)
The baseline MUST be in standard Go benchmark text format for benchstat compatibility. Do NOT convert to JSON.

**Correct** (text format):
```
BenchmarkProcessFrame-8    1000000    1000 ns/op    64 B/op    2 allocs/op
```

**Wrong** (JSON format):
```json
{"benchmark": "ProcessFrame", "ns_per_op": 1000, ...}
```

### Why Track in Git?
- Baseline evolves with code changes
- Version control shows performance trends over time
- CI can detect regressions by comparing against committed baseline
- Manual updates via PR when intentional performance changes occur

## References
- **benchstat tool**: https://pkg.go.dev/golang.org/x/perf/cmd/benchstat
- **Go benchmarks**: https://pkg.go.dev/testing#hdr-Benchmarks
- **go test flags**: https://pkg.go.dev/cmd/go#hdr-Testing_flags

## Success Criteria
✅ Directory `.benchmarks/` created  
✅ File `.benchmarks/baseline.txt` exists  
✅ File has >10 lines (multiple benchmarks)  
✅ Format is standard Go benchmark text (not JSON)  
✅ Contains "Benchmark" prefixed lines  
✅ Contains ns/op, B/op, allocs/op fields  
✅ All critical benchmarks present:
  - BenchmarkAsyncDetector_ProcessFrame
  - BenchmarkReadFrameTo
  - BenchmarkOptimizedWriteFrame
✅ File is tracked in git (not ignored)  
✅ Benchmarks run with -count=5  

## Commit
**Message**: `chore(ci): add initial benchmark baseline for regression tracking`  
**Files**: `.benchmarks/baseline.txt`, potentially `.benchmarks/.gitkeep`  
**Pre-commit check**: `ls .benchmarks/baseline.txt && head -5 .benchmarks/baseline.txt`

## Anti-Patterns (DO NOT DO)
❌ Convert to JSON format (benchstat needs text format)  
❌ Run with -benchtime too long (default is fine)  
❌ Add to .gitignore (baseline should be tracked)  
❌ Skip -count=5 (needed for statistical significance)  
❌ Leave placeholders or TODOs  

## Parallelization
- **Can run in parallel**: NO (must be after workflows)
- **Parallel group**: Wave 4 (alone)
- **Blocks**: Task 10 (integration testing)
- **Blocked by**: Tasks 3, 4 (workflows reference baseline)
