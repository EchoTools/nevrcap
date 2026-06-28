# Go Agent Addendum — Idiomatic Go 1.26

*Authored by [github.com/metis-sprock](https://github.com/metis-sprock).
Generic edition — drop into any Go project. Tighten per project in a
sibling `AGENTS.md`.*

**Required reading** for ANY agent writing or reviewing Go code in a
project that adopts this addendum. Read this BEFORE writing any Go code,
BEFORE reviewing any Go PR, and BEFORE touching any Go file in the repo.

---

## The Missile Knows Where It Is

> *The missile knows where it is, because it knows where it isn't.*

This document is structured by negation as much as assertion. The "Never
Use", "Never", and "What Not to Do" clauses are load-bearing — they are
how an agent triangulates the correct path. An agent that reads only what
something IS will guess at its boundaries. An agent that also reads what
it IS NOT will not.

### This IS

- A binding ruleset for writing and reviewing Go 1.26 code in any project
  that adopts it (typically by linking from the repo's `AGENTS.md`).
- A code-review gate: the "Code Review Hard Stops" table is enforced.

### This is NOT

- A Go tutorial. Reading this does not teach you Go — it disciplines an
  agent that already knows Go.
- A style preference. These are not opinions; they are pinned defaults.
- A superset of project rules. Project-level `AGENTS.md` overrides this
  on conflict (most-specific wins).
- Optional for "scripts" or "small tools". Every `.go` file in an
  adopting repo is in scope, including `cmd/` entrypoints.
- A list of nice-to-haves. The "Never Use" entries are forbidden, not
  discouraged.

### You MUST

- Verify `go.mod` declares Go 1.26 (or the project's pinned version) before
  writing code; if the toolchain is older, stop and ask the user.
- Run the full local gate before declaring work done: `gofmt -l`, `go vet`,
  `golangci-lint run`, `go test -race`, `go fix`, `go mod tidy`,
  `govulncheck` — all clean.
- Define interfaces at the CONSUMER, return concrete types from constructors.
- Wrap errors with `%w` and a function-scoped prefix on every boundary.
- Pass `context.Context` as the first parameter on any I/O or blocking call.
- Treat warnings from linters as errors. `//nolint` requires an inline reason.

### You must NEVER

- Use `interface{}` (use `any`), `Union`-style empty interfaces for "shapes",
  or any API in the "Never Use" table.
- Return a non-nil error AND a non-zero result on the same call.
- Call `log.Fatal` / `os.Exit` outside `main` / `cmd/`.
- Spawn a goroutine without a teardown path or an `errgroup` parent.
- Use `context.Background()` outside `main`, tests, or top-level servers.
- Bypass safety: no `--no-verify`, no `//nolint` without justification, no
  silencing vet/lint by deleting the check.
- Skip steps in the local gate because "CI will catch it" — CI is a backstop,
  not the primary verifier.

### Common Mistakes (what goes wrong when agents guess)

- Treating Go like a generic OO language: bloated interfaces, base-class
  thinking, package-as-namespace. Result: weak abstractions, hidden coupling.
- Catch-all `interface{}` ("just in case it varies") instead of a small,
  consumer-defined interface. Result: type assertions metastasize.
- Wrapping errors without `%w`, or with prefixes that lose call-site
  context. Result: unrecoverable diagnostics.
- Defining interfaces in the producer package "for symmetry". Result:
  import cycles and test seams in the wrong place.
- Goroutines without an explicit cancellation/`Wait` path. Result: leaks
  that only surface in production under load.
- Silencing the linter instead of the code smell. Result: drift from the
  ecosystem's idioms; the next agent inherits a maze.

---

## Go Version

**Baseline: Go 1.26.** Check `go.mod` directive before writing code.

### Use These (1.24–1.26 features)

| Feature | Notes |
|---|---|
| `any` keyword | Never `interface{}` |
| `new(expr)` initializer | Replaces `&T{field: val}` for pointer-optional fields (1.26) |
| Recursive generic constraints | `type Adder[A Adder[A]] interface{}` now legal (1.26) |
| `go fix ./...` modernizers | Run as part of CI; applies idiomatic rewrites (1.26 revamp) |
| `errors.AsType[T]` | Generic type-safe replacement for `errors.As` (1.26) |
| `log/slog.NewMultiHandler` | Fan-out structured logging (1.26) |
| `testing.T.ArtifactDir()` | Test output files instead of `t.TempDir()` (1.26) |
| `B.Loop()` for benchmarks | Replaces `for i := 0; i < b.N; i++` (1.24+, fixed in 1.26) |
| `runtime/trace.FlightRecorder` | Ring-buffer tracing for rare events (1.25) |
| Container-aware `GOMAXPROCS` | Automatic; never call `runtime.GOMAXPROCS` in containers (1.25) |
| `go vet` `waitgroup` analyzer | Catches misplaced `WaitGroup.Add` (1.25) |
| `go vet` `hostport` analyzer | Use `net.JoinHostPort` always (1.25) |
| `go.mod` `ignore` directive | Declare vendor/generated dirs (1.25) |
| Range-over-func iterators | `iter.Seq2` for streaming results (1.22+) |

### Never Use (deprecated/removed)

- `interface{}` — use `any`
- `cmd/doc` / `go tool doc` — use `go doc`
- `net/http/httputil.ReverseProxy.Director` — use `Rewrite`
- `crypto/rsa.EncryptPKCS1v15` / `DecryptPKCS1v15` — use OAEP
- `for i := 0; i < b.N; i++` in benchmarks — use `b.Loop()`
- `fmt.Sprintf("%s:%d", host, port)` — use `net.JoinHostPort`
- `runtime.GOMAXPROCS` in containers
- `GODEBUG` settings `gotypesalias`, `asynctimerchan` (removed 1.27)

---

## Naming

| Entity | Rule | Example |
|---|---|---|
| Packages | Short, lowercase, singular noun | `store`, `metric`, `relay` |
| Interfaces | `-er` suffix for single-method; noun for multi | `Reader`, `Validator`, `UserStore` |
| Constructors | `New<Type>` | `NewClient`, `NewServer` |
| Error vars | `Err<Name>` (sentinel) | `ErrNotFound` |
| Error types | `<Name>Error` (struct) | `ValidationError` |
| Constants | MixedCaps, NOT ALL_CAPS | `MaxRetries`, `DefaultTimeout` |
| Acronyms | All-caps or all-lower | `userID`, `parseURL`, `HTTPClient` |
| Context | Always first param: `ctx context.Context` | — |
| Options | Functional options: `type Option func(*Config)` | `WithTimeout` |
| Test helpers | `must<Name>` for fatal helpers | `mustParse`, `mustDial` |

**Never:** name a variable `data`, `info`, `result`. Never shadow package names.
Never discard errors with `_` except in `defer` cleanup with logged fallback.

Short names for limited scope: `r` for reader, `w` for writer, `ctx` for context.

---

## Interface Design (Go-specific)

- **"The bigger the interface, the weaker the abstraction."** — Go Proverbs
- Interfaces are 1–3 methods. If you need 4+, split into composable pieces.
- Define interfaces at the CONSUMER, not the producer.
- Single-method interfaces get `-er` suffix: `Signer`, `Relayer`, `Dispatcher`.
- Never put all interfaces in one file ("interfaces.jar" anti-pattern).
- Return concrete types from constructors; accept interfaces as parameters.
- Maximum cyclomatic complexity: 10.

---

## Project Layout

```
cmd/               # Thin entrypoints; wiring only, no business logic
internal/          # Private application code (default home)
pkg/               # Public importable packages (use sparingly)
api/               # Proto definitions
testdata/          # Test fixtures
scripts/           # Helper scripts
justfile           # All developer workflows
.golangci.yml      # Linter config
go.mod
```

- Test files live NEXT TO the code: `foo_test.go` beside `foo.go`.
- Prefer black-box tests: `package foo_test`, not `package foo`.
- Integration tests use build tag `//go:build integration`.
- Never put test helpers in `main_test.go`; use shared `testutil` package.

---

## Testing

### TDD: Red-Green-Refactor. Always.

- Table-driven tests mandatory for logic-heavy functions.
- `t.Parallel()` in all compatible subtests.
- `testify/require` or raw `t.Fatal` — never `t.Error` then continue.
- Fuzz new parsers/public entry points: `func FuzzXxx(f *testing.F)`.
- `t.ArtifactDir()` for test output (1.26).
- No `time.Sleep` in tests — use channels, WaitGroup, or `testutil.Eventually`.
- Mock at interface boundary (`moq` or `mockery`). No monkey-patching.
- Goroutine leak detection: `GOEXPERIMENT=goroutineleakprofile go test ./...`

### Benchmarks

```go
func BenchmarkParse(b *testing.B) {
    input := []byte(`{"key":"value"}`)
    b.ReportAllocs()
    for b.Loop() {
        _, _ = parse(input)
    }
}
```

---

## Error Handling

```go
if err != nil {
    return fmt.Errorf("userService.Create: %w", err)
}

// type-safe unwrapping (1.26)
if ve, ok := errors.AsType[*ValidationError](err); ok {
    // use ve directly
}
```

- Never return non-nil error AND valid result simultaneously.
- Never `log.Fatal` or `os.Exit` outside `main` or `cmd/`.
- Error strings: lowercase, no trailing punctuation.
- `panic` only for programmer errors (violated invariants), never runtime failures.

---

## Concurrency

- `context.Context` as first param for any I/O or blocking function.
- `sync.WaitGroup.Add` BEFORE spawning goroutine (enforced by vet).
- `net.JoinHostPort` for all host:port (enforced by vet).
- Buffer channels explicitly; document buffer size rationale.
- Prefer `errgroup.Group` over raw goroutines + channels for fan-out.
- `sync/atomic` for single scalars, not `sync.Mutex`.

---

## Formatting & Static Analysis

```
gofmt -s -w .         # mandatory; CI fails on dirty
goimports -w .        # manages import grouping
go vet ./...          # every test invocation
golangci-lint run     # version-pinned; warnings = errors
```

### Import grouping

```go
import (
    "context"                              // stdlib
    "github.com/some/dep"                  // external
    "github.com/your-org/your-repo/internal" // internal
)
```

### golangci-lint minimum

```yaml
linters:
  enable:
    - errcheck
    - govet
    - staticcheck
    - gosimple
    - ineffassign
    - unused
    - revive
    - exhaustive
    - bodyclose
    - noctx
    - wastedassign
    - godot
```

---

## Logging

`log/slog` only. No zerolog, no zap, no logrus.

```go
slog.Info("user created",
    slog.String("user_id", u.ID),
    slog.Duration("latency", elapsed),
)
```

---

## Justfile (single source of truth)

```just
default: fmt vet lint test
fmt:     gofmt -w . && goimports -w .
vet:     go vet ./...
lint:    golangci-lint run ./...
test:    go test -race -count=1 ./...
test-leak: GOEXPERIMENT=goroutineleakprofile go test -race -count=1 ./...
bench:   go test -bench=. -benchmem -benchtime=3s ./...
modernize: go fix ./... && go mod tidy
audit:   govulncheck ./...
ci:      fmt vet lint test-leak audit
```

---

## Code Review Hard Stops

| # | Rule |
|---|---|
| 1 | `go vet ./...` zero output |
| 2 | `gofmt -l .` zero output |
| 3 | `golangci-lint run` zero output |
| 4 | Test coverage on changed packages does not decrease |
| 5 | No `fmt.Errorf` without `%w` when re-wrapping |
| 6 | No exported function without doc comment |
| 7 | No goroutine without teardown path |
| 8 | No `context.Background()` outside main/tests/top-level servers |
| 9 | No `interface{}` — use `any` |
| 10 | `go fix ./...` zero changes |
| 11 | `go mod tidy` zero changes |
| 12 | `govulncheck ./...` no known vulns |
| 13 | No deprecated API usage |
| 14 | All benchmarks use `b.Loop()` |
| 15 | No `//nolint` without inline explanation |

---

## What Not to Do

- No `init()` except driver/codec registration with no alternative.
- No `reflect` when generics solve the problem.
- No `runtime.GC()` in application code.
- No `crypto/rsa.EncryptPKCS1v15` — deprecated; use OAEP.
- No `fmt.Sprintf` for host:port — use `net.JoinHostPort`.
- No `runtime.GOMAXPROCS` in containers.
- No `http.Handler` on structs that also hold business logic.
