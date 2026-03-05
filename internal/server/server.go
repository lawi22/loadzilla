package server

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/lawi22/loadzilla/internal/metrics"
	"github.com/lawi22/loadzilla/internal/runner"
)

//go:embed web/index.html
var indexHTML []byte

var running atomic.Bool

// Start registers HTTP routes and starts the server on the given port.
func Start(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", serveIndex)
	mux.HandleFunc("/api/run", handleRun)

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("Loadzilla web UI → http://localhost%s\n", addr)
	return http.ListenAndServe(addr, mux)
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

type runRequest struct {
	URL         string              `json:"url"`
	Method      string              `json:"method"`
	Headers     map[string]string   `json:"headers"`
	Body        string              `json:"body"`
	Connections int                 `json:"connections"`
	RPS         int                 `json:"rps"`
	Duration    float64             `json:"duration"` // seconds
	CSVRows     []map[string]string `json:"csv_rows"`
}

type progressEvent struct {
	Type    string  `json:"type"`
	Total   int64   `json:"total"`
	Success int64   `json:"success"`
	Errors  int64   `json:"errors"`
	RPS     float64 `json:"rps"`
	Pct     float64 `json:"pct"`
}

type summaryJSON struct {
	Total     int64             `json:"total"`
	Success   int64             `json:"success"`
	Errors    int64             `json:"errors"`
	MinMs     float64           `json:"min_ms"`
	MaxMs     float64           `json:"max_ms"`
	AvgMs     float64           `json:"avg_ms"`
	P50Ms     float64           `json:"p50_ms"`
	P75Ms     float64           `json:"p75_ms"`
	P95Ms     float64           `json:"p95_ms"`
	P99Ms     float64           `json:"p99_ms"`
	P999Ms    float64           `json:"p999_ms"`
	StatusMap map[string]int64  `json:"status_map"`
	DurationS float64           `json:"duration_s"`
}

type doneEvent struct {
	Type    string      `json:"type"`
	Summary summaryJSON `json:"summary"`
}

func handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !running.CompareAndSwap(false, true) {
		http.Error(w, "test already running", http.StatusConflict)
		return
	}

	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		running.Store(false)
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Method == "" {
		req.Method = "GET"
	}
	if req.Connections <= 0 {
		req.Connections = 10
	}
	if req.Duration <= 0 {
		req.Duration = 10
	}
	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		running.Store(false)
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")

	progressCh := make(chan progressEvent, 10)
	doneCh := make(chan metrics.Summary, 1)

	var lastTotal int64
	lastTick := time.Now()

	cfg := runner.Config{
		URL:         req.URL,
		Method:      req.Method,
		Headers:     req.Headers,
		Body:        req.Body,
		Connections: req.Connections,
		RPS:         req.RPS,
		Duration:    time.Duration(req.Duration * float64(time.Second)),
		CSVRows:     req.CSVRows,
		OnProgress: func(snap metrics.Snapshot, pct float64) {
			now := time.Now()
			interval := now.Sub(lastTick).Seconds()
			var rps float64
			if interval > 0 {
				rps = float64(snap.Total-lastTotal) / interval
			}
			lastTotal = snap.Total
			lastTick = now
			ev := progressEvent{
				Type:    "progress",
				Total:   snap.Total,
				Success: snap.Success,
				Errors:  snap.Errors,
				RPS:     rps,
				Pct:     pct,
			}
			select {
			case progressCh <- ev:
			default:
			}
		},
		OnComplete: func(summary metrics.Summary) {
			doneCh <- summary
		},
	}

	runnerDone := make(chan struct{})
	go func() {
		defer close(runnerDone)
		_ = runner.RunWithContext(r.Context(), cfg)
	}()

	defer func() {
		<-runnerDone
		running.Store(false)
	}()

	sendSSE := func(v any) {
		data, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

loop:
	for {
		select {
		case ev := <-progressCh:
			sendSSE(ev)
		case summary := <-doneCh:
			statusMap := make(map[string]int64, len(summary.StatusMap))
			for k, v := range summary.StatusMap {
				if k == 0 {
					statusMap["err"] = v
				} else {
					statusMap[fmt.Sprintf("%d", k)] = v
				}
			}
			sendSSE(doneEvent{
				Type: "done",
				Summary: summaryJSON{
					Total:     summary.Total,
					Success:   summary.Success,
					Errors:    summary.Errors,
					MinMs:     summary.MinMs,
					MaxMs:     summary.MaxMs,
					AvgMs:     summary.AvgMs,
					P50Ms:     summary.P50Ms,
					P75Ms:     summary.P75Ms,
					P95Ms:     summary.P95Ms,
					P99Ms:     summary.P99Ms,
					P999Ms:    summary.P999Ms,
					StatusMap: statusMap,
					DurationS: req.Duration,
				},
			})
			break loop
		case <-r.Context().Done():
			break loop
		}
	}
}
