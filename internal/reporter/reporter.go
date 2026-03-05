package reporter

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/lawi22/loadzilla/internal/metrics"
)

var (
	bold   = color.New(color.Bold)
	cyan   = color.New(color.FgCyan, color.Bold)
	green  = color.New(color.FgGreen)
	red    = color.New(color.FgRed)
	yellow = color.New(color.FgYellow)
	dim    = color.New(color.Faint)
)

// LiveReporter prints a live progress line every tick.
type LiveReporter struct {
	col       *metrics.Collector
	duration  time.Duration
	start     time.Time
	lastTotal int64
	lastTick  time.Time
}

func NewLive(col *metrics.Collector, duration time.Duration, start time.Time) *LiveReporter {
	return &LiveReporter{
		col:      col,
		duration: duration,
		start:    start,
		lastTick: start,
	}
}

// Tick prints the current progress line, overwriting the previous one.
func (r *LiveReporter) Tick() {
	now := time.Now()
	elapsed := now.Sub(r.start)
	snap := r.col.Snapshot()

	pct := math.Min(elapsed.Seconds()/r.duration.Seconds()*100, 100)
	bar := progressBar(pct, 20)

	interval := now.Sub(r.lastTick).Seconds()
	var rps float64
	if interval > 0 {
		rps = float64(snap.Total-r.lastTotal) / interval
	}
	r.lastTotal = snap.Total
	r.lastTick = now

	line := fmt.Sprintf("\r[%s] %3.0f%% | %s req | %s rps | Errors: %s",
		bar,
		pct,
		formatInt(snap.Total),
		yellow.Sprintf("%.1f", rps),
		errStr(snap.Errors),
	)
	fmt.Print(line)
}

// ClearLine erases the live line before printing the final summary.
func ClearLine() {
	fmt.Print("\r" + strings.Repeat(" ", 80) + "\r")
}

func progressBar(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return cyan.Sprint(bar)
}

func errStr(n int64) string {
	if n == 0 {
		return green.Sprint("0")
	}
	return red.Sprintf("%s", formatInt(n))
}

// PrintSummary prints the final results table.
func PrintSummary(cfg SummaryConfig, s metrics.Summary) {
	ClearLine()

	sep := strings.Repeat("═", 45)
	thin := strings.Repeat("─", 45)

	bold.Println("Loadzilla Results")
	fmt.Println(sep)

	printKV("URL", cfg.URL)
	printKV("Method", cfg.Method)
	printKV("Duration", cfg.Duration.String())
	rpsLabel := "unlimited"
	if cfg.RPS > 0 {
		rpsLabel = fmt.Sprintf("%d", cfg.RPS)
	}
	printKV("Connections", fmt.Sprintf("%d | RPS Target: %s", cfg.Connections, rpsLabel))

	fmt.Println()
	bold.Println(" Requests")
	fmt.Println(" " + thin)

	successPct := 0.0
	errPct := 0.0
	if s.Total > 0 {
		successPct = float64(s.Success) / float64(s.Total) * 100
		errPct = float64(s.Errors) / float64(s.Total) * 100
	}
	throughput := float64(s.Total) / cfg.Duration.Seconds()

	printKV("Total", formatInt(s.Total))
	printKV("Success", fmt.Sprintf("%s  (%.1f%%)", formatInt(s.Success), successPct))
	printKV("Errors", errFmt(s.Errors, errPct))
	printKV("Throughput", fmt.Sprintf("%.1f req/s", throughput))

	fmt.Println()
	bold.Println(" Latency")
	fmt.Println(" " + thin)

	fmt.Printf("  %-8s %8s    %-8s %8s\n",
		dim.Sprint("Min:"), fmtMs(s.MinMs),
		dim.Sprint("P50:"), fmtMs(s.P50Ms))
	fmt.Printf("  %-8s %8s    %-8s %8s\n",
		dim.Sprint("Avg:"), fmtMs(s.AvgMs),
		dim.Sprint("P95:"), fmtMs(s.P95Ms))
	fmt.Printf("  %-8s %8s    %-8s %8s\n",
		dim.Sprint("Max:"), fmtMs(s.MaxMs),
		dim.Sprint("P99:"), fmtMs(s.P99Ms))
	fmt.Printf("  %-8s %8s    %-8s %8s\n",
		"", "",
		dim.Sprint("P999:"), fmtMs(s.P999Ms))

	if len(s.StatusMap) > 0 {
		fmt.Println()
		bold.Println(" Status Codes")
		fmt.Println(" " + thin)

		codes := make([]int, 0, len(s.StatusMap))
		for code := range s.StatusMap {
			codes = append(codes, code)
		}
		sort.Ints(codes)

		for _, code := range codes {
			label := fmt.Sprintf("%d", code)
			if code == 0 {
				label = "err"
			}
			count := s.StatusMap[code]
			if code == 0 || code >= 400 {
				fmt.Printf("  %s: %s\n", red.Sprint(label), formatInt(count))
			} else {
				fmt.Printf("  %s: %s\n", green.Sprint(label), formatInt(count))
			}
		}
	}

	fmt.Println(sep)
}

// SummaryConfig holds the test parameters echoed in the summary.
type SummaryConfig struct {
	URL         string
	Method      string
	Duration    time.Duration
	Connections int
	RPS         int
}

func printKV(key, val string) {
	fmt.Printf(" %-14s%s\n", dim.Sprintf("%s:", key), val)
}

func fmtMs(ms float64) string {
	if ms < 1 {
		return yellow.Sprintf("%.2fms", ms)
	}
	if ms >= 1000 {
		return red.Sprintf("%.1fs", ms/1000)
	}
	return fmt.Sprintf("%.1fms", ms)
}

func errFmt(n int64, pct float64) string {
	s := fmt.Sprintf("%s  (%.1f%%)", formatInt(n), pct)
	if n > 0 {
		return red.Sprint(s)
	}
	return s
}

func formatInt(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	s := fmt.Sprintf("%d", n)
	result := make([]byte, 0, len(s)+len(s)/3)
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
