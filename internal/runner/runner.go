package runner

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
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
}

// Run executes the load test described by cfg.
func Run(cfg Config) error {
	// Build HTTP client with connection limit matching workers.
	transport := &http.Transport{
		MaxConnsPerHost:     cfg.Connections,
		MaxIdleConnsPerHost: cfg.Connections,
		IdleConnTimeout:     90 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	// Build rate limiter.
	var lim *rate.Limiter
	if cfg.RPS <= 0 {
		lim = rate.NewLimiter(rate.Inf, 0)
	} else {
		lim = rate.NewLimiter(rate.Limit(cfg.RPS), cfg.RPS)
	}

	// Context: cancelled by duration OR Ctrl+C.
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
	defer cancel()

	// Handle Ctrl+C gracefully.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			fmt.Println() // newline after ^C
			cancel()
		case <-ctx.Done():
		}
	}()

	col := metrics.New()
	start := time.Now()

	// Start worker goroutines.
	var wg sync.WaitGroup
	for i := 0; i < cfg.Connections; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker(ctx, client, lim, col, cfg)
		}()
	}

	// Live reporter goroutine.
	rep := reporter.NewLive(col, cfg.Duration, start)
	tickDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rep.Tick()
			case <-ctx.Done():
				rep.Tick() // final live update
				close(tickDone)
				return
			}
		}
	}()

	wg.Wait()
	<-tickDone

	// Print final summary.
	summary := col.Summary()
	reporter.PrintSummary(reporter.SummaryConfig{
		URL:         cfg.URL,
		Method:      cfg.Method,
		Duration:    cfg.Duration,
		Connections: cfg.Connections,
		RPS:         cfg.RPS,
	}, summary)

	return nil
}

func worker(ctx context.Context, client *http.Client, lim *rate.Limiter, col *metrics.Collector, cfg Config) {
	bodyBytes := []byte(cfg.Body)

	for {
		if err := lim.Wait(ctx); err != nil {
			// Context cancelled — stop.
			return
		}

		var reqBody io.Reader
		if len(bodyBytes) > 0 {
			reqBody = strings.NewReader(cfg.Body)
		}

		req, err := http.NewRequestWithContext(ctx, cfg.Method, cfg.URL, reqBody)
		if err != nil {
			col.RecordError()
			continue
		}
		for k, v := range cfg.Headers {
			req.Header.Set(k, v)
		}

		t0 := time.Now()
		resp, err := client.Do(req)
		latency := time.Since(t0)

		if err != nil {
			// Don't record if ctx was cancelled (test ended).
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
