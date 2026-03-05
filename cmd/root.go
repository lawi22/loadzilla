package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lawi22/loadzilla/internal/runner"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "loadzilla",
	Short: "A fast, friendly HTTP load testing tool",
	Long:  `Loadzilla — run timed HTTP load tests with live progress and latency percentiles.`,
	RunE:  runLoad,
}

var (
	flagURL         string
	flagMethod      string
	flagHeaders     []string
	flagBody        string
	flagBodyFile    string
	flagConnections int
	flagRPS         int
	flagDuration    time.Duration
)

func init() {
	rootCmd.Flags().StringVarP(&flagURL, "url", "u", "", "Target URL (required)")
	rootCmd.Flags().StringVarP(&flagMethod, "method", "m", "GET", "HTTP method")
	rootCmd.Flags().StringArrayVarP(&flagHeaders, "header", "H", nil, "Request header (repeatable, e.g. -H 'Key: Value')")
	rootCmd.Flags().StringVarP(&flagBody, "body", "b", "", "Request body (raw string)")
	rootCmd.Flags().StringVar(&flagBodyFile, "body-file", "", "Read request body from file")
	rootCmd.Flags().IntVarP(&flagConnections, "connections", "c", 10, "Concurrent connections (goroutines)")
	rootCmd.Flags().IntVarP(&flagRPS, "rps", "r", 0, "Target requests per second (0 = unlimited)")
	rootCmd.Flags().DurationVarP(&flagDuration, "duration", "d", 10*time.Second, "Test duration")

	_ = rootCmd.MarkFlagRequired("url")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runLoad(cmd *cobra.Command, args []string) error {
	headers := make(map[string]string)
	for _, h := range flagHeaders {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid header format %q — expected 'Key: Value'", h)
		}
		headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}

	body := flagBody
	if flagBodyFile != "" {
		data, err := os.ReadFile(flagBodyFile)
		if err != nil {
			return fmt.Errorf("reading body file: %w", err)
		}
		body = string(data)
	}

	cfg := runner.Config{
		URL:         flagURL,
		Method:      strings.ToUpper(flagMethod),
		Headers:     headers,
		Body:        body,
		Connections: flagConnections,
		RPS:         flagRPS,
		Duration:    flagDuration,
	}

	return runner.Run(cfg)
}
