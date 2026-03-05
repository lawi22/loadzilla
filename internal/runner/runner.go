package runner

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/lawi22/loadzilla/internal/metrics"
	"github.com/lawi22/loadzilla/internal/reporter"
	"golang.org/x/time/rate"
)

// Config holds all parameters for a load test run.
type Config struct {
	URL         string
	Method      string
	Headers     map[string]string
	Body        string
	Connections int
	RPS         int
	Duration    time.Duration

	// CSV variable substitution: each row maps column name → value.
	// Workers pick rows round-robin and substitute {$COLUMN} in URL, headers, body.
	CSVRows []map[string]string

	// OnProgress is called every second with a snapshot and completion %.
	// Nil in CLI mode; set by the web server.
	OnProgress func(snap metrics.Snapshot, pct float64)

	// OnComplete is called once after all workers finish with the final summary.
	// Nil in CLI mode; set by the web server.
	OnComplete func(summary metrics.Summary)
}

// Run executes the load test described by cfg.
func Run(cfg Config) error {
	return RunWithContext(context.Background(), cfg)
}

// RunWithContext executes the load test, deriving a timeout context from parentCtx.
// Signal handling (SIGINT) only runs when OnProgress == nil (CLI mode).
func RunWithContext(parentCtx context.Context, cfg Config) error {
	transport := &http.Transport{
		MaxConnsPerHost:     cfg.Connections,
		MaxIdleConnsPerHost: cfg.Connections,
		IdleConnTimeout:     90 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	var lim *rate.Limiter
	if cfg.RPS <= 0 {
		lim = rate.NewLimiter(rate.Inf, 0)
	} else {
		lim = rate.NewLimiter(rate.Limit(cfg.RPS), cfg.RPS)
	}

	ctx, cancel := context.WithTimeout(parentCtx, cfg.Duration)
	defer cancel()

	// Signal handling only in CLI mode.
	if cfg.OnProgress == nil {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		go func() {
			select {
			case <-sigCh:
				fmt.Println()
				cancel()
			case <-ctx.Done():
			}
		}()
	}

	col := metrics.New()
	start := time.Now()

	var csvIdx atomic.Uint64

	var wg sync.WaitGroup
	for i := 0; i < cfg.Connections; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker(ctx, client, lim, col, cfg, &csvIdx)
		}()
	}

	tickDone := make(chan struct{})
	if cfg.OnProgress == nil {
		rep := reporter.NewLive(col, cfg.Duration, start)
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					rep.Tick()
				case <-ctx.Done():
					rep.Tick()
					close(tickDone)
					return
				}
			}
		}()
	} else {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					snap := col.Snapshot()
					elapsed := time.Since(start)
					pct := math.Min(elapsed.Seconds()/cfg.Duration.Seconds()*100, 100)
					cfg.OnProgress(snap, pct)
				case <-ctx.Done():
					snap := col.Snapshot()
					cfg.OnProgress(snap, 100)
					close(tickDone)
					return
				}
			}
		}()
	}

	wg.Wait()
	<-tickDone

	summary := col.Summary()
	if cfg.OnComplete == nil {
		reporter.PrintSummary(reporter.SummaryConfig{
			URL:         cfg.URL,
			Method:      cfg.Method,
			Duration:    cfg.Duration,
			Connections: cfg.Connections,
			RPS:         cfg.RPS,
		}, summary)
	} else {
		cfg.OnComplete(summary)
	}

	return nil
}

// substituteVars replaces {$KEY} placeholders in s using the given row map.
func substituteVars(s string, row map[string]string) string {
	for k, v := range row {
		s = strings.ReplaceAll(s, "{$"+k+"}", v)
	}
	return s
}

func worker(ctx context.Context, client *http.Client, lim *rate.Limiter, col *metrics.Collector, cfg Config, csvIdx *atomic.Uint64) {
	for {
		if err := lim.Wait(ctx); err != nil {
			return
		}

		url := cfg.URL
		body := cfg.Body
		headers := cfg.Headers

		if len(cfg.CSVRows) > 0 {
			idx := int(csvIdx.Add(1)-1) % len(cfg.CSVRows)
			row := cfg.CSVRows[idx]
			url = substituteVars(url, row)
			body = substituteVars(body, row)
			newHeaders := make(map[string]string, len(headers))
			for k, v := range headers {
				newHeaders[k] = substituteVars(v, row)
			}
			headers = newHeaders
		}

		var reqBody io.Reader
		if len(body) > 0 {
			reqBody = strings.NewReader(body)
		}

		req, err := http.NewRequestWithContext(ctx, cfg.Method, url, reqBody)
		if err != nil {
			col.RecordError()
			continue
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		t0 := time.Now()
		resp, err := client.Do(req)
		latency := time.Since(t0)

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			col.RecordError()
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		col.RecordResult(resp.StatusCode, latency)
	}
}
