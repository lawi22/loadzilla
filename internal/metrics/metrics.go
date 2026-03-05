package metrics

import (
	"sync"
	"sync/atomic"
	"time"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"
)

// Collector gathers latency and request outcome data thread-safely.
type Collector struct {
	mu      sync.Mutex
	hist    *hdrhistogram.Histogram
	statMap map[int]int64

	total   atomic.Int64
	success atomic.Int64
	errors  atomic.Int64
}

// Snapshot is a point-in-time view of counters (no percentiles — cheap to read).
type Snapshot struct {
	Total   int64
	Success int64
	Errors  int64
}

// Summary is the final report including latency percentiles.
type Summary struct {
	Total      int64
	Success    int64
	Errors     int64
	StatusMap  map[int]int64
	MinMs      float64
	MaxMs      float64
	AvgMs      float64
	P50Ms      float64
	P75Ms      float64
	P95Ms      float64
	P99Ms      float64
	P999Ms     float64
}

func New() *Collector {
	// 1µs to 60s range, 3 significant figures
	return &Collector{
		hist:    hdrhistogram.New(1, 60_000_000, 3),
		statMap: make(map[int]int64),
	}
}

// RecordResult records one HTTP result. Safe for concurrent use.
func (c *Collector) RecordResult(statusCode int, latency time.Duration) {
	micros := latency.Microseconds()
	if micros < 1 {
		micros = 1
	}

	c.mu.Lock()
	_ = c.hist.RecordValue(micros)
	c.statMap[statusCode]++
	c.mu.Unlock()

	c.total.Add(1)
	if statusCode >= 200 && statusCode < 400 {
		c.success.Add(1)
	} else {
		c.errors.Add(1)
	}
}

// RecordError records a request that failed before getting an HTTP response.
func (c *Collector) RecordError() {
	c.total.Add(1)
	c.errors.Add(1)

	c.mu.Lock()
	c.statMap[0]++
	c.mu.Unlock()
}

// Snapshot returns lightweight counters without touching the histogram.
func (c *Collector) Snapshot() Snapshot {
	return Snapshot{
		Total:   c.total.Load(),
		Success: c.success.Load(),
		Errors:  c.errors.Load(),
	}
}

// Summary returns final stats including latency percentiles. Call once after test.
func (c *Collector) Summary() Summary {
	c.mu.Lock()
	defer c.mu.Unlock()

	h := c.hist
	totalCount := h.TotalCount()

	statCopy := make(map[int]int64, len(c.statMap))
	for k, v := range c.statMap {
		statCopy[k] = v
	}

	var avg float64
	if totalCount > 0 {
		avg = float64(h.Mean()) / 1000.0
	}

	return Summary{
		Total:     c.total.Load(),
		Success:   c.success.Load(),
		Errors:    c.errors.Load(),
		StatusMap: statCopy,
		MinMs:     float64(h.Min()) / 1000.0,
		MaxMs:     float64(h.Max()) / 1000.0,
		AvgMs:     avg,
		P50Ms:     float64(h.ValueAtQuantile(50)) / 1000.0,
		P75Ms:     float64(h.ValueAtQuantile(75)) / 1000.0,
		P95Ms:     float64(h.ValueAtQuantile(95)) / 1000.0,
		P99Ms:     float64(h.ValueAtQuantile(99)) / 1000.0,
		P999Ms:    float64(h.ValueAtQuantile(99.9)) / 1000.0,
	}
}
