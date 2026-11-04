# Goroutine Leak Lab: Requirements

## 1. Goal

A Go TCP server that leaks goroutines in 3 ways, selected by flag, with a fixed version of each one. Every claim made in the video must come from a run of this lab.

The lab must produce:

- a CSV of metrics over time (for the charts);
- a pprof dump showing each leak;
- a goleak test that **fails** with the bug and **passes** with the fix;
- a real OOM kill inside a memory-limited container.

Out of scope: Kubernetes, TLS, real protocols (HTTP, gRPC), authentication, persistence.

## 2. Components

| Component | Purpose |
|---|---|
| `cmd/server` | TCP server with the leak mode selected by flag |
| `cmd/loadgen` | Load client that creates the connection pattern that triggers each leak |
| `internal/metrics` | Collects metrics and writes the CSV |
| `server_test.go` | goleak tests, one per leak |
| `scripts/plot.py` | CSV to PNG |
| `Dockerfile` + `docker-compose.yml` | Memory-limited environment |
| `Makefile` | One command per scenario |

## 3. Protocol

A line protocol, as simple as possible:

- the client sends `GET <key>\n`;
- the server replies `VALUE <key> <n>\n`;
- the server simulates a slow "lookup" with a configurable duration (this is what feeds the channel leak).

## 4. Server

### 4.1 Flags

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `:9000` | TCP address |
| `-pprof-addr` | `:6060` | pprof address, separate from the traffic |
| `-leak` | `none` | `none`, `read`, `chan` or `ticker` |
| `-fix` | `false` | Enables the fixed version of the same mode |
| `-conn-bytes` | `65536` | Memory allocated and held per connection (simulates `bufio` plus session state) |
| `-lookup` | `200ms` | Duration of the simulated lookup |
| `-handler-timeout` | `100ms` | How long the handler waits for the lookup before giving up |
| `-idle-timeout` | `30s` | Read deadline used by the fixed versions |
| `-metrics-path` | `/metrics/run.csv` | CSV output |
| `-metrics-interval` | `1s` | Collection interval |

Design rule: with `-fix=true`, the code changes **only** the line or block that fixes the leak. Everything else is identical. This is what lets the video show "this is the whole difference".

### 4.2 Behavior per mode

**`none`** (baseline): one goroutine per connection, reads with a deadline, replies, closes. Nothing grows.

**`read`**, Read without a deadline:

- bug: the handler allocates `conn-bytes`, calls `conn.Read` without a deadline, and waits forever on a silent client;
- fix: `SetReadDeadline` before every read, plus a total deadline per connection, so a client that sends 1 byte every now and then also gets dropped;
- expected pprof state: `IO wait`.

**`chan`**, send on an unbuffered channel after a timeout:

- bug: the handler starts a lookup goroutine that delivers its result on an unbuffered channel; the handler gives up after `handler-timeout` and returns; the lookup goroutine finishes later and blocks forever on the send;
- leak condition: `lookup` longer than `handler-timeout`;
- fix: a channel with buffer 1, or a `select` with `ctx.Done()` on the send;
- expected pprof state: `chan send`.

**`ticker`**, a heartbeat goroutine nobody stops:

- bug: for each connection, the handler starts a goroutine that runs `for range ticker.C` (heartbeat, stats); the connection closes, the handler returns, the goroutine keeps running;
- fix: a context cancelled when the connection closes, a `select` with `ctx.Done()` in the loop, and `defer ticker.Stop()`;
- expected pprof state: `select` or `chan receive`.
- Requirement: `go.mod` must declare `go 1.23` or newer. Since that version, an unstopped and unreferenced ticker no longer leaks by itself; the leak the video shows is the **goroutine**, not the ticker.

### 4.3 pprof

- `net/http/pprof` on `-pprof-addr`.
- Must serve `/debug/pprof/goroutine?debug=1` and `?debug=2`.

### 4.4 Shutdown

- On SIGTERM or SIGINT: stop accepting, close the listener, wait for the handlers that **can** finish, and exit.
- Also exposed as a function (`Server.Shutdown`), because the tests need it.
- Question to answer while implementing: in `read` mode with the bug, can shutdown wait for the handlers? What happens if it uses a `WaitGroup`?

## 5. Metrics

CSV with a header, one row per interval, flushed after every row (if the process dies from OOM, the CSV must be complete up to the last second).

| Column | Source |
|---|---|
| `ts_unix_ms` | clock |
| `goroutines` | `runtime.NumGoroutine()` |
| `heap_alloc_bytes` | `runtime.ReadMemStats` |
| `heap_sys_bytes` | `runtime.ReadMemStats` |
| `stack_inuse_bytes` | `runtime.ReadMemStats` |
| `rss_bytes` | `VmRSS` from `/proc/self/status` |
| `cgroup_mem_bytes` | `/sys/fs/cgroup/memory.current` (empty if missing) |
| `open_conns` | the server's own counter |

Requirement: collection must not call `ReadMemStats` at a high rate, because it stops the world. Once per second is enough.

## 6. Loadgen

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `server:9000` | Target |
| `-mode` | `silent` | `silent`, `request` or `request-leave` |
| `-rate` | `50` | New connections per second |
| `-max-conns` | `0` | Cap on open connections (0 means no cap) |
| `-duration` | `0` | Total run time (0 means until stopped) |

Modes:

- `silent`: connects and sends nothing, keeping the connection open. Triggers the `read` leak.
- `request`: connects, sends `GET`, reads the reply, and closes. Triggers `chan` and `ticker`.
- `request-leave`: sends `GET` and closes without reading. Useful to show the server does not notice.

Output: one line per second on stdout with open connections, connection errors and write errors. When the server dies, the errors must show up here.

## 7. goleak tests

One test per mode, each run twice (bug and fix):

1. start the server on `127.0.0.1:0`;
2. create the leak scenario;
3. call the server shutdown;
4. `goleak.VerifyNone(t)`.

Acceptance criteria: **with the bug, the test fails** and the goleak message shows the leaking stack; **with the fix, it passes**. If the test passes with the bug, the test is wrong, not the code.

Trap for `read` mode: if the test closes the client connection, the server's `Read` gets EOF and returns, and the leak disappears. So how does the test reproduce the leak? Solve this before writing the test.

The bug must not break `go test ./...` for the repo. Use a build tag, a `t.Skip` controlled by an environment variable, or a test that expects the failure. Pick one and justify it in the README.

## 8. Environment

- Multi-stage `Dockerfile` building `server` and `loadgen`.
- `docker-compose.yml`:
  - `server`: `mem_limit: 256m`, `memswap_limit: 256m`, `ulimits.nofile: 1048576`, `restart: on-failure`, volume `./metrics:/metrics`;
  - `loadgen`: `ulimits.nofile: 1048576`, same network.
- `Makefile` with one target per scenario: `make run-read`, `make run-read-fix`, `make run-chan`, ..., `make test`, `make plot`.

## 9. What the runs must produce (for the video)

For each mode, bug and fix:

- time until the OOM kill (or "did not die in X min" for the fix);
- cost per leaked connection = (final `cgroup_mem_bytes` minus initial) / leaked goroutines;
- difference between `rss_bytes` and `cgroup_mem_bytes`;
- the `debug=1` and `debug=2` excerpts shown on screen;
- `docker inspect` showing `OOMKilled: true` and the restart count;
- whether the server hit the file descriptor limit or the memory limit first.

Record the `-conn-bytes` and `-rate` used for every number. A number without its configuration does not go into the video.

## 10. Non-functional

- Only external dependency: `go.uber.org/goleak`.
- `go vet` and `go test -race` clean on the fixed version.
- Runnable from scratch with `docker compose up` and one `make` target.

## 11. Implementation order

1. Server in `none` mode + CSV metrics + pprof.
2. `read` mode (bug and fix) + `silent` loadgen.
3. Docker with the memory limit and the first OOM. **At this point there is already material for the hook.**
4. goleak test for `read`.
5. `chan` mode + `request` loadgen + test.
6. `ticker` mode + test.
7. `plot.py`.