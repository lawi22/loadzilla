# Loadzilla

Super lightweight HTTP load testing CLI — clean output, accurate latency percentiles, zero config overhead.

```
[████████████████████] 100% | 5,842 req | 194.7 rps | Errors: 0
```

---

## Install

```bash
go install github.com/lawi22/loadzilla@latest
```

Or download a pre-built binary from [Releases](https://github.com/lawi22/loadzilla/releases).

---

## Quick Start

```bash
# Simple GET
loadzilla -u https://httpbin.org/get

# 20 connections, 200 rps, 30 seconds
loadzilla -u https://api.example.com/users -c 20 -r 200 -d 30s

# POST with headers and body
loadzilla -u https://api.example.com/users \
  -m POST \
  -H "Authorization: Bearer token" \
  -H "Content-Type: application/json" \
  -b '{"name": "test"}' \
  -c 10 -r 50 -d 30s
```

---

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--url` | `-u` | **required** | Target URL |
| `--method` | `-m` | `GET` | HTTP method (`GET`, `POST`, `PUT`, `PATCH`, `DELETE`, …) |
| `--header` | `-H` | — | Request header in `Key: Value` format. Repeatable. |
| `--body` | `-b` | — | Request body as a raw string |
| `--body-file` | — | — | Read request body from a file |
| `--connections` | `-c` | `10` | Number of concurrent goroutines (workers) |
| `--rps` | `-r` | `0` | Target requests per second — `0` means unlimited |
| `--duration` | `-d` | `10s` | How long to run the test (e.g. `30s`, `2m`) |

### Flag details

**`--url / -u`** *(required)*
The full URL to target, including scheme and any path or query parameters.

```bash
loadzilla -u "https://api.example.com/search?q=test"
```

**`--method / -m`**
HTTP verb. Defaults to `GET`. Case-insensitive.

```bash
loadzilla -u https://api.example.com/items -m DELETE
```

**`--header / -H`**
Set one or more request headers. Use the flag multiple times for multiple headers.

```bash
loadzilla -u https://api.example.com \
  -H "Authorization: Bearer eyJ..." \
  -H "X-Request-ID: test-run-1"
```

**`--body / -b`**
Inline request body. Useful for short payloads.

```bash
loadzilla -u https://api.example.com/users -m POST \
  -H "Content-Type: application/json" \
  -b '{"name":"alice","role":"admin"}'
```

**`--body-file`**
Read the request body from a file. Takes precedence over `--body` if both are set.

```bash
loadzilla -u https://api.example.com/import -m POST \
  -H "Content-Type: application/json" \
  --body-file ./payload.json
```

**`--connections / -c`**
Number of concurrent workers. Each worker maintains its own persistent HTTP connection. Increasing this pushes more parallel load; pair with `--rps` to keep throughput controlled.

```bash
loadzilla -u https://api.example.com -c 50
```

**`--rps / -r`**
Global requests-per-second cap enforced via a token bucket. All workers share this budget. Set to `0` (default) to run flat-out.

```bash
# Steady 100 rps across 10 workers
loadzilla -u https://api.example.com -c 10 -r 100

# Unlimited — hit as hard as the connections allow
loadzilla -u https://api.example.com -c 50 -r 0
```

**`--duration / -d`**
How long the test runs. Accepts Go duration strings. Press `Ctrl+C` at any time to stop early — a full summary is still printed.

```bash
loadzilla -u https://api.example.com -d 2m
loadzilla -u https://api.example.com -d 90s
```

---

## Output

### Live progress (updates every second)

```
[████████████░░░░░░░░]  62% | 1,240 req | 97.8 rps | Errors: 3
```

### Final summary

```
Loadzilla Results
═════════════════════════════════════════════
 URL:          https://api.example.com/users
 Method:       POST
 Duration:     30s
 Connections:  20 | RPS Target: 200

 Requests
 ─────────────────────────────────────────────
 Total:        5,842
 Success:      5,839  (99.9%)
 Errors:       3  (0.1%)
 Throughput:   194.7 req/s

 Latency
 ─────────────────────────────────────────────
  Min:     12.4ms    P50:     38.2ms
  Avg:     41.1ms    P95:     76.4ms
  Max:    892.1ms    P99:    124.8ms
                     P999:   341.2ms

 Status Codes
 ─────────────────────────────────────────────
  200: 5,839
  500: 3
═════════════════════════════════════════════
```

Latency percentiles are computed using an [HDR Histogram](https://github.com/HdrHistogram/hdrhistogram-go) for accuracy even at high request volumes.

---

## Examples

```bash
# Baseline GET, default settings
loadzilla -u https://httpbin.org/get

# Ramp up connections, cap at 500 rps, run for 1 minute
loadzilla -u https://api.example.com/feed -c 50 -r 500 -d 1m

# Authenticated POST from a file
loadzilla -u https://api.example.com/events \
  -m POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --body-file event.json \
  -c 20 -r 100 -d 30s

# Stress test — unlimited rps, many connections
loadzilla -u https://api.example.com/health -c 100 -d 60s
```

---

## How it works

- **N goroutines** run concurrently, each sending requests in a tight loop
- A shared **token bucket** (`golang.org/x/time/rate`) enforces the RPS cap globally
- `net/http.Transport` is configured with `MaxConnsPerHost` matching `--connections` to prevent silent connection pooling
- Results are recorded into a **mutex-protected HDR histogram** (microsecond precision) alongside atomic success/error counters
- `Ctrl+C` cancels the context cleanly — in-flight requests finish, then the summary prints

---

## License

MIT
